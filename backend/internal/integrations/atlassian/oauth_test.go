package atlassian

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeAuth simula auth.atlassian.com e api.atlassian.com.
func fakeAuth(t *testing.T, handler func(path string, form url.Values) (int, any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		status, body := handler(r.URL.Path, r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
}

// withFakeHosts substitui as URLs hardcoded por um httptest server.
// Volta a função de cleanup.
func withFakeHosts(t *testing.T, srv *httptest.Server) {
	t.Helper()
	httpClientOverride = srv.Client()
	t.Cleanup(func() { httpClientOverride = nil })
}

func TestBuildAuthorizeURL(t *testing.T) {
	c := &OAuthConfig{
		ClientID:    "cid",
		RedirectURI: "https://app.example.com/cb",
		Scopes:      []string{"read:jira-work", "offline_access"},
	}
	u := c.BuildAuthorizeURL("state-xyz")
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "auth.atlassian.com" {
		t.Errorf("host = %q", parsed.Host)
	}
	q := parsed.Query()
	if q.Get("client_id") != "cid" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("state") != "state-xyz" {
		t.Errorf("state = %q", q.Get("state"))
	}
	if !strings.Contains(q.Get("scope"), "offline_access") {
		t.Errorf("scope = %q, faltou offline_access", q.Get("scope"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("audience") != "api.atlassian.com" {
		t.Errorf("audience = %q", q.Get("audience"))
	}
}

func TestNewOAuthConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv("ATLASSIAN_OAUTH_CLIENT_ID", "cid")
	t.Setenv("ATLASSIAN_OAUTH_CLIENT_SECRET", "sec")
	t.Setenv("ATLASSIAN_OAUTH_REDIRECT_URI", "")
	c, err := NewOAuthConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.RedirectURI == "" {
		t.Error("redirect default vazio")
	}
	wantScopes := []string{"read:jira-work", "read:jira-user", "offline_access"}
	if strings.Join(c.Scopes, ",") != strings.Join(wantScopes, ",") {
		t.Errorf("scopes = %+v", c.Scopes)
	}
}

func TestNewOAuthConfigFromEnv_RequiresIDAndSecret(t *testing.T) {
	t.Setenv("ATLASSIAN_OAUTH_CLIENT_ID", "")
	if _, err := NewOAuthConfigFromEnv(); err == nil {
		t.Error("aceitou env vazio")
	}
}

// Para testar ExchangeCode/RefreshToken precisamos substituir o URL
// hardcoded — vou refatorar minimamente em uma variável para o test.
// Solução: criar um httptest server que serve POST /oauth/token e
// hijack via httpClientOverride + um helper que substitui o domain.
// Como a função usa URL absoluta hardcoded, monkey-patch via
// http.Client.Transport.

type rewriteTransport struct {
	authHost string
	apiHost  string
	rt       http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.URL.Host {
	case "auth.atlassian.com":
		req.URL.Host = t.authHost
		req.URL.Scheme = "http"
	case "api.atlassian.com":
		req.URL.Host = t.apiHost
		req.URL.Scheme = "http"
	}
	return t.rt.RoundTrip(req)
}

func TestExchangeCode_HappyPath(t *testing.T) {
	srv := fakeAuth(t, func(path string, form url.Values) (int, any) {
		if path != "/oauth/token" {
			return 404, map[string]string{"error": "nope"}
		}
		if form.Get("grant_type") != "authorization_code" {
			return 400, map[string]string{"error": "grant"}
		}
		if form.Get("code") != "abc" {
			return 400, map[string]string{"error": "code"}
		}
		return 200, map[string]any{
			"access_token":  "at-1",
			"refresh_token": "rt-1",
			"expires_in":    3600,
			"scope":         "read:jira-work offline_access",
			"token_type":    "Bearer",
		}
	})
	defer srv.Close()

	tr := &rewriteTransport{authHost: strings.TrimPrefix(srv.URL, "http://"), apiHost: "", rt: http.DefaultTransport}
	httpClientOverride = &http.Client{Transport: tr, Timeout: 5 * time.Second}
	defer func() { httpClientOverride = nil }()

	c := &OAuthConfig{ClientID: "cid", ClientSecret: "sec", RedirectURI: "http://app/cb"}
	tok, err := c.ExchangeCode(context.Background(), "abc")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "at-1" || tok.RefreshToken != "rt-1" || tok.ExpiresIn != 3600 {
		t.Errorf("got %+v", tok)
	}
	expires := tok.ExpiresAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	want := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	if !expires.Equal(want) {
		t.Errorf("expiresAt = %v, want %v", expires, want)
	}
}

func TestExchangeCode_HTTPError(t *testing.T) {
	srv := fakeAuth(t, func(_ string, _ url.Values) (int, any) {
		return 400, map[string]string{"error": "invalid_grant"}
	})
	defer srv.Close()
	tr := &rewriteTransport{authHost: strings.TrimPrefix(srv.URL, "http://"), rt: http.DefaultTransport}
	httpClientOverride = &http.Client{Transport: tr, Timeout: 5 * time.Second}
	defer func() { httpClientOverride = nil }()

	c := &OAuthConfig{ClientID: "cid", ClientSecret: "sec"}
	_, err := c.ExchangeCode(context.Background(), "code")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("err = %v", err)
	}
}

func TestRefreshToken_RotatesIfReturned(t *testing.T) {
	srv := fakeAuth(t, func(_ string, form url.Values) (int, any) {
		if form.Get("grant_type") != "refresh_token" {
			return 400, map[string]string{"error": "grant"}
		}
		return 200, map[string]any{
			"access_token":  "new-at",
			"refresh_token": "new-rt", // rotação
			"expires_in":    1800,
			"token_type":    "Bearer",
		}
	})
	defer srv.Close()
	tr := &rewriteTransport{authHost: strings.TrimPrefix(srv.URL, "http://"), rt: http.DefaultTransport}
	httpClientOverride = &http.Client{Transport: tr, Timeout: 5 * time.Second}
	defer func() { httpClientOverride = nil }()

	c := &OAuthConfig{ClientID: "cid", ClientSecret: "sec"}
	tok, err := c.RefreshToken(context.Background(), "old-rt")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "new-at" || tok.RefreshToken != "new-rt" {
		t.Errorf("got %+v", tok)
	}
}

func TestListAccessibleResources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-tok" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "cloud-1", "name": "ACME", "url": "https://acme.atlassian.net", "scopes": []string{"read:jira-work"}},
		})
	}))
	defer srv.Close()
	tr := &rewriteTransport{apiHost: strings.TrimPrefix(srv.URL, "http://"), rt: http.DefaultTransport}
	httpClientOverride = &http.Client{Transport: tr, Timeout: 5 * time.Second}
	defer func() { httpClientOverride = nil }()

	res, err := ListAccessibleResources(context.Background(), "test-tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ID != "cloud-1" {
		t.Errorf("got %+v", res)
	}
}
