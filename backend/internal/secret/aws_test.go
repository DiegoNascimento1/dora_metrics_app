package secret

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fixedCreds struct{}

func (fixedCreds) get() (string, string, string, error) {
	return "AKIAFAKE", "secret", "", nil
}

// awsStub responde no formato secretsmanager:GetSecretValue.
// data[secretId] = SecretString (raw JSON or plain). Se vazio, devolve
// ResourceNotFoundException (400).
func awsStub(t *testing.T, data map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Amz-Target") != "secretsmanager.GetSecretValue" {
			http.Error(w, "wrong target", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "no auth", http.StatusForbidden)
			return
		}
		var req struct{ SecretId string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		val, ok := data[req.SecretId]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"__type":"ResourceNotFoundException","message":"Secrets Manager can't find the specified secret."}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Name":         req.SecretId,
			"ARN":          "arn:aws:secretsmanager:us-east-1:000:secret:" + req.SecretId,
			"SecretString": val,
			"VersionId":    "v1",
			"VersionStages": []string{"AWSCURRENT"},
		})
	}))
}

func newTestAWSProvider(t *testing.T, srv *httptest.Server, prefix string) *AWSSecretsManagerProvider {
	t.Helper()
	return &AWSSecretsManagerProvider{
		region:       "us-east-1",
		endpoint:     srv.URL,
		pathPrefix:   prefix,
		credProvider: fixedCreds{},
		http:         srv.Client(),
	}
}

func TestAWSProvider_GetDirectSecret(t *testing.T) {
	srv := awsStub(t, map[string]string{
		"dora/GITLAB_TOKEN": "glpat-xyz",
	})
	defer srv.Close()
	p := newTestAWSProvider(t, srv, "dora")
	got, err := p.Get(context.Background(), "GITLAB_TOKEN")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "glpat-xyz" {
		t.Errorf("got %q", got)
	}
}

func TestAWSProvider_FallbackToCredentialsGroup(t *testing.T) {
	srv := awsStub(t, map[string]string{
		"dora/credentials": `{"GITLAB_TOKEN":"glpat-grouped","JIRA_TOKEN":"jira-xyz"}`,
	})
	defer srv.Close()
	p := newTestAWSProvider(t, srv, "dora")
	got, err := p.Get(context.Background(), "JIRA_TOKEN")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "jira-xyz" {
		t.Errorf("got %q", got)
	}
}

func TestAWSProvider_NotFound(t *testing.T) {
	srv := awsStub(t, map[string]string{})
	defer srv.Close()
	p := newTestAWSProvider(t, srv, "dora")
	_, err := p.Get(context.Background(), "MISSING")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAWSProvider_5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	p := newTestAWSProvider(t, srv, "")
	_, err := p.Get(context.Background(), "X")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v", err)
	}
}

func TestAWSProvider_RequiresRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	_, err := NewAWSSecretsManagerProvider()
	if err == nil {
		t.Error("expected error when AWS_REGION missing")
	}
}

// SigV4: testa formato do header Authorization. Não validamos a
// assinatura crypto-corretamente porque AWS é quem faz; aqui só
// garantimos que o header está bem-formado.
func TestSigV4_AuthorizationHeader(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://secretsmanager.us-east-1.amazonaws.com/", strings.NewReader(`{"SecretId":"x"}`))
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager.GetSecretValue")

	fixed := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	if err := signSigV4(req, []byte(`{"SecretId":"x"}`), "AKIA", "key", "", "us-east-1", "secretsmanager", fixed); err != nil {
		t.Fatalf("signSigV4: %v", err)
	}
	auth := req.Header.Get("Authorization")
	for _, want := range []string{"AWS4-HMAC-SHA256", "Credential=AKIA/20260522/us-east-1/secretsmanager/aws4_request", "SignedHeaders=", "Signature="} {
		if !strings.Contains(auth, want) {
			t.Errorf("Authorization=%q missing %q", auth, want)
		}
	}
	if req.Header.Get("X-Amz-Date") != "20260522T120000Z" {
		t.Errorf("X-Amz-Date = %q", req.Header.Get("X-Amz-Date"))
	}
}

func TestSigV4_SessionTokenIncluded(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://x/", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager.GetSecretValue")
	if err := signSigV4(req, []byte("{}"), "AK", "key", "session-tok", "us-east-1", "secretsmanager", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("X-Amz-Security-Token") != "session-tok" {
		t.Errorf("missing X-Amz-Security-Token")
	}
	if !strings.Contains(req.Header.Get("Authorization"), "x-amz-security-token") {
		t.Errorf("session token não está em SignedHeaders")
	}
}
