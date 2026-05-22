// AWSSecretsManagerProvider lê segredos do AWS Secrets Manager via HTTP
// direto (sem AWS SDK).
//
// Motivação para não usar o SDK oficial: `github.com/aws/aws-sdk-go-v2`
// traz ~80 deps transitivas e ~30MB de binário. Para este caso (1 chamada
// GetSecretValue por segredo, com refresh ocasional), implementar
// SigV4 manualmente custa ~200 LOC e mantém o binário enxuto.
//
// Como funciona o lookup:
//   1. tenta nome literal {prefix}/{key} (1 segredo por chave, mais comum
//      em deployments novos)
//   2. fallback: lê o segredo {prefix}/credentials (formato JSON
//      {"GITLAB_TOKEN":"...","JIRA_TOKEN":"..."}) e extrai a chave
//
// Auth: credenciais AWS standard (env, profile, IRSA, EC2 role) — usa
// o que estiver disponível via os.Getenv("AWS_ACCESS_KEY_ID")/etc. Para
// produção com IAM Role, recomendamos preencher via Pod Identity / IRSA
// (Kubernetes) ou Instance Metadata Service (EC2/ECS).
package secret

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// AWSSecretsManagerProvider implementa secret.Provider via AWS Secrets
// Manager.
type AWSSecretsManagerProvider struct {
	region       string
	endpoint     string // override para testes (httptest); default é AWS oficial
	pathPrefix   string
	credProvider awsCredentialsProvider
	http         *http.Client
}

// awsCredentialsProvider é a interface mínima necessária. Em produção,
// devolve credenciais do env/IRSA/IMDS. Em testes, retorna fixas.
type awsCredentialsProvider interface {
	get() (accessKey, secretKey, sessionToken string, err error)
}

// envCredentials lê AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY e
// (opcional) AWS_SESSION_TOKEN.
type envCredentials struct{}

func (envCredentials) get() (string, string, string, error) {
	ak := os.Getenv("AWS_ACCESS_KEY_ID")
	sk := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if ak == "" || sk == "" {
		return "", "", "", errors.New("AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY ausentes")
	}
	return ak, sk, os.Getenv("AWS_SESSION_TOKEN"), nil
}

// NewAWSSecretsManagerProvider lê config do ambiente.
//
//   - AWS_REGION                  obrigatório
//   - AWS_SECRETS_PATH_PREFIX     opcional (ex: "dora/prod/")
//   - AWS_SECRETS_MANAGER_ENDPOINT opcional, default oficial
func NewAWSSecretsManagerProvider() (*AWSSecretsManagerProvider, error) {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		return nil, errors.New("AWS_REGION obrigatório")
	}
	endpoint := os.Getenv("AWS_SECRETS_MANAGER_ENDPOINT")
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://secretsmanager.%s.amazonaws.com", region)
	}
	return &AWSSecretsManagerProvider{
		region:       region,
		endpoint:     endpoint,
		pathPrefix:   os.Getenv("AWS_SECRETS_PATH_PREFIX"),
		credProvider: envCredentials{},
		http:         &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// Get implementa secret.Provider.
func (p *AWSSecretsManagerProvider) Get(ctx context.Context, key string) (string, error) {
	if val, err := p.getSecret(ctx, path.Join(p.pathPrefix, key)); err == nil {
		return val, nil
	} else if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	// fallback: lê o secret "credentials" e extrai o subkey
	raw, err := p.getSecret(ctx, path.Join(p.pathPrefix, "credentials"))
	if err != nil {
		return "", err
	}
	var dict map[string]string
	if err := json.Unmarshal([]byte(raw), &dict); err != nil {
		return "", fmt.Errorf("credentials secret não é JSON: %w", err)
	}
	val, ok := dict[key]
	if !ok || val == "" {
		return "", ErrNotFound
	}
	return val, nil
}

// getSecret chama a operação secretsmanager:GetSecretValue.
func (p *AWSSecretsManagerProvider) getSecret(ctx context.Context, secretID string) (string, error) {
	body := fmt.Sprintf(`{"SecretId":"%s"}`, strings.TrimPrefix(secretID, "/"))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build aws request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager.GetSecretValue")

	ak, sk, sessionToken, err := p.credProvider.get()
	if err != nil {
		return "", err
	}
	if err := signSigV4(req, []byte(body), ak, sk, sessionToken, p.region, "secretsmanager", time.Now().UTC()); err != nil {
		return "", fmt.Errorf("sigv4 sign: %w", err)
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("aws request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))

	if resp.StatusCode == http.StatusBadRequest {
		// AWS devolve 400 para "ResourceNotFoundException" — checa pelo body.
		if strings.Contains(string(respBody), "ResourceNotFoundException") {
			return "", ErrNotFound
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("aws http %d: %s", resp.StatusCode, respBody)
	}

	var out struct {
		SecretString string `json:"SecretString"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("decode aws response: %w", err)
	}
	if out.SecretString == "" {
		return "", ErrNotFound
	}
	return out.SecretString, nil
}

// ---- SigV4 minimalista ----
//
// Assina uma requisição AWS conforme
// https://docs.aws.amazon.com/general/latest/gr/sigv4_signing.html
// O subset implementado cobre POST + body de tamanho conhecido + headers
// fixos (Host + Content-Type + X-Amz-Target + X-Amz-Date + Authorization).
// Não cobre query-string signing nem chunked transfer.
func signSigV4(req *http.Request, body []byte, accessKey, secretKey, sessionToken, region, service string, now time.Time) error {
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("X-Amz-Date", amzDate)
	if sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", sessionToken)
	}
	req.Header.Set("Host", req.URL.Host)

	// Canonical request
	canonicalURI := req.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := req.URL.RawQuery

	// Headers ordenados.
	signedHeadersList := []string{"content-type", "host", "x-amz-date", "x-amz-target"}
	if sessionToken != "" {
		signedHeadersList = append(signedHeadersList, "x-amz-security-token")
	}
	sort.Strings(signedHeadersList)

	var headerLines strings.Builder
	for _, h := range signedHeadersList {
		headerLines.WriteString(h)
		headerLines.WriteString(":")
		headerLines.WriteString(strings.TrimSpace(req.Header.Get(http.CanonicalHeaderKey(h))))
		headerLines.WriteString("\n")
	}
	signedHeaders := strings.Join(signedHeadersList, ";")
	payloadHash := sha256Hex(body)

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		headerLines.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	// String to sign
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	// Signing key
	kDate := hmacSHA256([]byte("AWS4"+secretKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	signature := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))

	authHeader := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, credentialScope, signedHeaders, signature,
	)
	req.Header.Set("Authorization", authHeader)
	return nil
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
