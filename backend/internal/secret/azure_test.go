package secret

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// azureStub simula AAD + Key Vault no mesmo servidor. Diferencia pelos
// paths: /<tenant>/oauth2/v2.0/token (AAD) e /secrets/<name> (KV).
func azureStub(t *testing.T, secrets map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// AAD token endpoint
	mux.HandleFunc("/tnt-123/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("client_id") != "client-id" {
			http.Error(w, "bad client", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"aad-token","token_type":"Bearer","expires_in":3600}`)
	})

	// Key Vault GET /secrets/{name}
	mux.HandleFunc("/secrets/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer aad-token" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/secrets/")
		val, ok := secrets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"value":%q,"id":"https://x.vault.azure.net/secrets/%s/v1"}`, val, name)
	})

	return httptest.NewServer(mux)
}

func newTestAzureProvider(t *testing.T, srv *httptest.Server, prefix string) *AzureKeyVaultProvider {
	t.Helper()
	return &AzureKeyVaultProvider{
		vaultURL:  srv.URL,
		tenantID:  "tnt-123",
		clientID:  "client-id",
		clientSec: "client-sec",
		prefix:    prefix,
		aadHost:   srv.URL,
		http:      srv.Client(),
	}
}

func TestAzureProvider_GetDirectSecret(t *testing.T) {
	srv := azureStub(t, map[string]string{
		"dora-GITLAB-TOKEN": "glpat-azure",
	})
	defer srv.Close()
	p := newTestAzureProvider(t, srv, "dora")
	got, err := p.Get(context.Background(), "GITLAB_TOKEN")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "glpat-azure" {
		t.Errorf("got %q", got)
	}
}

func TestAzureProvider_FallbackToCredentialsGroup(t *testing.T) {
	srv := azureStub(t, map[string]string{
		"dora-credentials": `{"JIRA_TOKEN":"jira-from-group"}`,
	})
	defer srv.Close()
	p := newTestAzureProvider(t, srv, "dora")
	got, err := p.Get(context.Background(), "JIRA_TOKEN")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "jira-from-group" {
		t.Errorf("got %q", got)
	}
}

func TestAzureProvider_NotFound(t *testing.T) {
	srv := azureStub(t, map[string]string{})
	defer srv.Close()
	p := newTestAzureProvider(t, srv, "")
	_, err := p.Get(context.Background(), "MISSING")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestAzureProvider_TokenCacheReusedAcrossCalls(t *testing.T) {
	var tokenRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/oauth2/v2.0/token") {
			tokenRequests++
			fmt.Fprintf(w, `{"access_token":"t","expires_in":3600}`)
			return
		}
		fmt.Fprintf(w, `{"value":"v"}`)
	}))
	defer srv.Close()
	p := newTestAzureProvider(t, srv, "")
	_, _ = p.Get(context.Background(), "X")
	_, _ = p.Get(context.Background(), "Y")
	_, _ = p.Get(context.Background(), "Z")
	if tokenRequests > 1 {
		t.Errorf("token requested %d times, expected 1 (cache reuse)", tokenRequests)
	}
}

func TestAzureProvider_RequiresEnvVars(t *testing.T) {
	t.Setenv("AZURE_VAULT_URL", "")
	_, err := NewAzureKeyVaultProvider()
	if err == nil {
		t.Error("expected error when env vars missing")
	}
}

func TestSecretName_SanitizesUnderscoresAndPrefix(t *testing.T) {
	cases := []struct {
		prefix, key, want string
	}{
		{"", "GITLAB_TOKEN", "GITLAB-TOKEN"},
		{"dora", "GITLAB_TOKEN", "dora-GITLAB-TOKEN"},
		{"dora-prod", "credentials", "dora-prod-credentials"},
		{"", "simple", "simple"},
	}
	for _, c := range cases {
		if got := secretName(c.prefix, c.key); got != c.want {
			t.Errorf("secretName(%q,%q)=%q, want %q", c.prefix, c.key, got, c.want)
		}
	}
}
