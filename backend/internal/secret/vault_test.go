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

// vaultStub responde no formato KVv2 do Vault. `data[path] = subkeys`.
func vaultStub(t *testing.T, data map[string]map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "tok" {
			http.Error(w, "no token", http.StatusForbidden)
			return
		}
		// path esperado: /v1/secret/data/<rel>
		prefix := "/v1/secret/data/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		rel := r.URL.Path[len(prefix):]
		entry, ok := data[rel]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"data":{"data":%s}}`, mapToJSON(entry))
	}))
}

func mapToJSON(m map[string]string) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%q:%q", k, v))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func TestVaultProvider_GetByValueSubkey(t *testing.T) {
	srv := vaultStub(t, map[string]map[string]string{
		"dora/GITLAB_TOKEN": {"value": "glpat-xyz"},
	})
	defer srv.Close()

	v := &VaultProvider{
		addr:       srv.URL,
		token:      "tok",
		mount:      "secret",
		pathPrefix: "dora",
		http:       srv.Client(),
	}
	got, err := v.Get(context.Background(), "GITLAB_TOKEN")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "glpat-xyz" {
		t.Errorf("got %q, want glpat-xyz", got)
	}
}

func TestVaultProvider_GetFromCredentialsGroup(t *testing.T) {
	// O secret dedicado não existe, mas há um secret "credentials" com a chave.
	srv := vaultStub(t, map[string]map[string]string{
		"dora/credentials": {
			"GITLAB_TOKEN": "glpat-grouped",
			"JIRA_TOKEN":   "jira-xyz",
		},
	})
	defer srv.Close()

	v := &VaultProvider{
		addr:       srv.URL,
		token:      "tok",
		mount:      "secret",
		pathPrefix: "dora",
		http:       srv.Client(),
	}
	got, err := v.Get(context.Background(), "JIRA_TOKEN")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "jira-xyz" {
		t.Errorf("got %q", got)
	}
}

func TestVaultProvider_NotFound(t *testing.T) {
	srv := vaultStub(t, map[string]map[string]string{})
	defer srv.Close()

	v := &VaultProvider{
		addr:  srv.URL,
		token: "tok",
		mount: "secret",
		http:  srv.Client(),
	}
	_, err := v.Get(context.Background(), "MISSING")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestVaultProvider_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
	}))
	defer srv.Close()

	v := &VaultProvider{
		addr:  srv.URL,
		token: "wrong",
		mount: "secret",
		http:  srv.Client(),
	}
	_, err := v.Get(context.Background(), "X")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("err = %v, expected 403", err)
	}
}

func TestNew_VaultRequiresEnv(t *testing.T) {
	t.Setenv("VAULT_ADDR", "")
	_, err := New("vault")
	if err == nil {
		t.Error("expected error when VAULT_ADDR empty")
	}
}

func TestEnvProvider_LookupAndMiss(t *testing.T) {
	p := NewEnvProvider()
	t.Setenv("DORA_TEST_KEY", "hello")
	got, err := p.Get(context.Background(), "DORA_TEST_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Errorf("got %q", got)
	}
	_, err = p.Get(context.Background(), "DORA_TEST_MISSING")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
