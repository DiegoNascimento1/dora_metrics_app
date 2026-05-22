package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newOAuthForTest configura clientes fixos e operator token.
func newOAuthForTest(t *testing.T) *OAuthServer {
	t.Helper()
	t.Setenv("MCP_OAUTH_CLIENTS", "claude-desktop:http://localhost:5173/cb|other:http://x/cb")
	t.Setenv("MCP_OAUTH_OPERATOR_TOKEN", "operator-sek")
	return NewOAuthServer()
}

func pkcePair() (verifier, challenge string) {
	verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestOAuth_Metadata(t *testing.T) {
	o := newOAuthForTest(t)
	mux := http.NewServeMux()
	o.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/oauth/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var doc map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &doc)
	if doc["authorization_endpoint"] == "" || doc["token_endpoint"] == "" {
		t.Errorf("metadata incompleto: %+v", doc)
	}
	methods, _ := doc["code_challenge_methods_supported"].([]any)
	if len(methods) == 0 || methods[0] != "S256" {
		t.Errorf("code_challenge_methods = %+v, want [S256]", methods)
	}
}

func TestOAuth_AuthorizeRedirectsWithCode(t *testing.T) {
	o := newOAuthForTest(t)
	_, challenge := pkcePair()

	u := url.URL{Path: "/oauth/authorize"}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", "claude-desktop")
	q.Set("redirect_uri", "http://localhost:5173/cb")
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", "xyz")
	u.RawQuery = q.Encode()

	req := httptest.NewRequest(http.MethodGet, u.String(), nil)
	req.Header.Set("X-MCP-Operator-Token", "operator-sek")
	w := httptest.NewRecorder()
	o.handleAuthorize(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("location parse: %v", err)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Error("missing code in redirect")
	}
	if loc.Query().Get("state") != "xyz" {
		t.Errorf("state = %q", loc.Query().Get("state"))
	}
}

func TestOAuth_AuthorizeRequiresOperatorToken(t *testing.T) {
	o := newOAuthForTest(t)
	_, challenge := pkcePair()

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=claude-desktop&redirect_uri=http://localhost:5173/cb&code_challenge="+challenge+"&code_challenge_method=S256", nil)
	// SEM X-MCP-Operator-Token
	w := httptest.NewRecorder()
	o.handleAuthorize(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestOAuth_AuthorizeRejectsUnknownClient(t *testing.T) {
	o := newOAuthForTest(t)
	_, challenge := pkcePair()
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=hax&redirect_uri=http://localhost:5173/cb&code_challenge="+challenge+"&code_challenge_method=S256", nil)
	req.Header.Set("X-MCP-Operator-Token", "operator-sek")
	w := httptest.NewRecorder()
	o.handleAuthorize(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d", w.Code)
	}
}

func TestOAuth_AuthorizeRejectsBadRedirectURI(t *testing.T) {
	o := newOAuthForTest(t)
	_, challenge := pkcePair()
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=claude-desktop&redirect_uri=https://evil.com/cb&code_challenge="+challenge+"&code_challenge_method=S256", nil)
	req.Header.Set("X-MCP-Operator-Token", "operator-sek")
	w := httptest.NewRecorder()
	o.handleAuthorize(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d", w.Code)
	}
}

func TestOAuth_AuthorizeRequiresPKCE_S256(t *testing.T) {
	o := newOAuthForTest(t)
	// sem code_challenge
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=claude-desktop&redirect_uri=http://localhost:5173/cb", nil)
	req.Header.Set("X-MCP-Operator-Token", "operator-sek")
	w := httptest.NewRecorder()
	o.handleAuthorize(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d", w.Code)
	}
}

func TestOAuth_TokenSwap_HappyPath(t *testing.T) {
	o := newOAuthForTest(t)
	verifier, challenge := pkcePair()

	// 1. authorize → ganha code
	authReq := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=claude-desktop&redirect_uri=http://localhost:5173/cb&code_challenge="+challenge+"&code_challenge_method=S256", nil)
	authReq.Header.Set("X-MCP-Operator-Token", "operator-sek")
	authW := httptest.NewRecorder()
	o.handleAuthorize(authW, authReq)
	loc, _ := url.Parse(authW.Header().Get("Location"))
	code := loc.Query().Get("code")

	// 2. token swap
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", "claude-desktop")
	form.Set("redirect_uri", "http://localhost:5173/cb")
	form.Set("code_verifier", verifier)
	tokReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	tokReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokW := httptest.NewRecorder()
	o.handleToken(tokW, tokReq)

	if tokW.Code != http.StatusOK {
		t.Fatalf("token status = %d, body = %s", tokW.Code, tokW.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(tokW.Body.Bytes(), &out)
	access, _ := out["access_token"].(string)
	if access == "" {
		t.Fatal("access_token vazio")
	}
	if tt, _ := out["token_type"].(string); tt != "Bearer" {
		t.Errorf("token_type = %q", tt)
	}

	// 3. valida o token
	cid, _, ok := o.ValidateToken(access)
	if !ok {
		t.Error("ValidateToken devolveu false")
	}
	if cid != "claude-desktop" {
		t.Errorf("clientID = %q", cid)
	}
}

func TestOAuth_TokenSwap_WrongVerifier(t *testing.T) {
	o := newOAuthForTest(t)
	_, challenge := pkcePair()

	authReq := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=claude-desktop&redirect_uri=http://localhost:5173/cb&code_challenge="+challenge+"&code_challenge_method=S256", nil)
	authReq.Header.Set("X-MCP-Operator-Token", "operator-sek")
	authW := httptest.NewRecorder()
	o.handleAuthorize(authW, authReq)
	loc, _ := url.Parse(authW.Header().Get("Location"))
	code := loc.Query().Get("code")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", "claude-desktop")
	form.Set("redirect_uri", "http://localhost:5173/cb")
	form.Set("code_verifier", "wrong-verifier")
	tokReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	tokReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokW := httptest.NewRecorder()
	o.handleToken(tokW, tokReq)

	if tokW.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (PKCE mismatch)", tokW.Code)
	}
}

func TestOAuth_TokenSwap_SingleUse(t *testing.T) {
	o := newOAuthForTest(t)
	verifier, challenge := pkcePair()

	authReq := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=claude-desktop&redirect_uri=http://localhost:5173/cb&code_challenge="+challenge+"&code_challenge_method=S256", nil)
	authReq.Header.Set("X-MCP-Operator-Token", "operator-sek")
	authW := httptest.NewRecorder()
	o.handleAuthorize(authW, authReq)
	loc, _ := url.Parse(authW.Header().Get("Location"))
	code := loc.Query().Get("code")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", "claude-desktop")
	form.Set("redirect_uri", "http://localhost:5173/cb")
	form.Set("code_verifier", verifier)
	body := form.Encode()

	// primeira troca: ok
	r1 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(body))
	r1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w1 := httptest.NewRecorder()
	o.handleToken(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first swap status = %d", w1.Code)
	}

	// segunda troca com mesmo code: deve falhar
	r2 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(body))
	r2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w2 := httptest.NewRecorder()
	o.handleToken(w2, r2)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("replay attack: status = %d, want 400", w2.Code)
	}
}

func TestOAuth_Revoke(t *testing.T) {
	o := newOAuthForTest(t)
	// emite um token
	o.activeTokens["abc"] = issuedToken{clientID: "x", scope: "all", expiresAt: time.Now().Add(time.Hour)}

	form := url.Values{}
	form.Set("token", "abc")
	r := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	o.handleRevoke(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
	if _, _, ok := o.ValidateToken("abc"); ok {
		t.Error("token deveria ter sido revogado")
	}
}

func TestOAuth_NotEnabledWithoutClients(t *testing.T) {
	t.Setenv("MCP_OAUTH_CLIENTS", "")
	o := NewOAuthServer()
	if o.Enabled() {
		t.Error("OAuth não deveria estar habilitado sem clientes")
	}
}
