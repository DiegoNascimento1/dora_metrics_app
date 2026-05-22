// AzureKeyVaultProvider lê segredos do Azure Key Vault via HTTP REST
// direto, sem SDK oficial. Mesma motivação do AWSSecretsManagerProvider:
// evitar pulling de ~50 deps transitivas para 1 chamada GetSecret por
// chave.
//
// Auth: OAuth 2.0 Client Credentials Flow contra Azure AD. Lê o token
// do AAD uma vez e renova quando expira. Endpoint do AAD e do Vault
// configuráveis (com defaults oficiais).
//
// Como funciona o lookup (espelha Vault + AWS):
//
//	{prefix}-{key}     → 1 segredo por chave (default)
//	credentials        → fallback (JSON com map de chaves)
//
// Importante: Azure Key Vault NÃO aceita "/" em nome de segredo —
// usamos "-" como separador entre prefix e key.
package secret

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// AzureKeyVaultProvider implementa secret.Provider via Azure Key Vault.
type AzureKeyVaultProvider struct {
	vaultURL   string // ex: https://my-vault.vault.azure.net
	tenantID   string
	clientID   string
	clientSec  string
	prefix     string
	aadHost    string // ex: https://login.microsoftonline.com
	http       *http.Client
	tokenMu    sync.Mutex
	cachedTok  string
	tokExpires time.Time
}

// NewAzureKeyVaultProvider lê config do ambiente.
//
//   - AZURE_VAULT_URL              obrigatório (ex: https://x.vault.azure.net)
//   - AZURE_TENANT_ID              obrigatório
//   - AZURE_CLIENT_ID              obrigatório (Service Principal)
//   - AZURE_CLIENT_SECRET          obrigatório
//   - AZURE_KEY_VAULT_PREFIX       opcional ("dora-prod")
//   - AZURE_AAD_HOST               opcional override (testes)
func NewAzureKeyVaultProvider() (*AzureKeyVaultProvider, error) {
	vault := os.Getenv("AZURE_VAULT_URL")
	tenant := os.Getenv("AZURE_TENANT_ID")
	clientID := os.Getenv("AZURE_CLIENT_ID")
	secret := os.Getenv("AZURE_CLIENT_SECRET")
	if vault == "" || tenant == "" || clientID == "" || secret == "" {
		return nil, errors.New("AZURE_VAULT_URL / AZURE_TENANT_ID / AZURE_CLIENT_ID / AZURE_CLIENT_SECRET obrigatórios")
	}
	aadHost := os.Getenv("AZURE_AAD_HOST")
	if aadHost == "" {
		aadHost = "https://login.microsoftonline.com"
	}
	return &AzureKeyVaultProvider{
		vaultURL:  strings.TrimRight(vault, "/"),
		tenantID:  tenant,
		clientID:  clientID,
		clientSec: secret,
		prefix:    os.Getenv("AZURE_KEY_VAULT_PREFIX"),
		aadHost:   strings.TrimRight(aadHost, "/"),
		http:      &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// Get implementa secret.Provider.
func (a *AzureKeyVaultProvider) Get(ctx context.Context, key string) (string, error) {
	if val, err := a.getSecret(ctx, secretName(a.prefix, key)); err == nil {
		return val, nil
	} else if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	raw, err := a.getSecret(ctx, secretName(a.prefix, "credentials"))
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

// secretName aplica a convenção `{prefix}-{key}`. Sanitização: Azure
// permite só [a-zA-Z0-9-] em nomes de segredo, então convertemos
// underscores para hífens.
func secretName(prefix, key string) string {
	sanitized := strings.ReplaceAll(key, "_", "-")
	if prefix == "" {
		return sanitized
	}
	return prefix + "-" + sanitized
}

// getSecret chama GET {vaultURL}/secrets/{name}?api-version=7.4
func (a *AzureKeyVaultProvider) getSecret(ctx context.Context, name string) (string, error) {
	token, err := a.token(ctx)
	if err != nil {
		return "", fmt.Errorf("azure token: %w", err)
	}

	uri := fmt.Sprintf("%s/secrets/%s?api-version=7.4", a.vaultURL, url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return "", fmt.Errorf("build azure request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("azure request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))

	if resp.StatusCode == http.StatusNotFound {
		return "", ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("azure http %d: %s", resp.StatusCode, body)
	}

	var out struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decode azure response: %w", err)
	}
	if out.Value == "" {
		return "", ErrNotFound
	}
	return out.Value, nil
}

// token devolve um access token válido. Faz cache até 60s antes de
// expirar — Client Credentials Flow dá ~1h por default.
func (a *AzureKeyVaultProvider) token(ctx context.Context) (string, error) {
	a.tokenMu.Lock()
	defer a.tokenMu.Unlock()

	if a.cachedTok != "" && time.Now().Before(a.tokExpires.Add(-60*time.Second)) {
		return a.cachedTok, nil
	}

	form := url.Values{}
	form.Set("client_id", a.clientID)
	form.Set("client_secret", a.clientSec)
	form.Set("grant_type", "client_credentials")
	form.Set("scope", "https://vault.azure.net/.default")

	uri := fmt.Sprintf("%s/%s/oauth2/v2.0/token", a.aadHost, a.tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uri, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("aad http %d: %s", resp.StatusCode, body)
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	a.cachedTok = out.AccessToken
	a.tokExpires = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return out.AccessToken, nil
}
