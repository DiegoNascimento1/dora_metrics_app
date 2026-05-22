// OAuth 2.1 Authorization Server minimalista para o MCP server.
//
// Implementa o subset da OAuth 2.1 que a spec MCP exige para clientes
// autorizarem agentes a falar com o servidor:
//
//   - Authorization Code Flow com PKCE (RFC 7636), code_challenge_method=S256
//   - Token endpoint trocando code por access_token (Bearer)
//   - Clientes pré-cadastrados via env MCP_OAUTH_CLIENTS=cliente1:redirect1,cliente2:redirect2
//     (Dynamic Client Registration / RFC 7591 NÃO suportado — produto interno)
//
// Storage: in-memory (lifetime do processo). Para multi-réplica, mover
// `codes` e `tokens` para Postgres ou Redis.
//
// Endpoints registrados:
//
//	GET  /oauth/.well-known/oauth-authorization-server  (metadata RFC 8414)
//	GET  /oauth/authorize?...                            (autoriza o code)
//	POST /oauth/token                                    (troca code → access_token)
//	POST /oauth/revoke                                   (revoga token)
//
// Como o MCP server roda interno (sem UI de login real), o /authorize
// **auto-aprova** se um header `X-MCP-Operator-Token` for fornecido
// (equivalente ao token estático do Bearer atual). Em produção, isso
// vira um redirecionamento para o IdP central.
package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	codeTTL  = 60 * time.Second
	tokenTTL = 1 * time.Hour
)

// OAuthServer mantém clientes registrados + códigos pendentes + tokens
// emitidos. Stateful intencional — produção real precisa migrar storage.
type OAuthServer struct {
	mu            sync.Mutex
	clients       map[string]oauthClient   // clientID → client
	pendingCodes  map[string]pendingCode   // code → state
	activeTokens  map[string]issuedToken   // access_token → state
	operatorToken string                   // pre-shared para auto-aprovação
}

type oauthClient struct {
	ID           string
	RedirectURIs []string
}

type pendingCode struct {
	clientID            string
	redirectURI         string
	codeChallenge       string
	codeChallengeMethod string
	scope               string
	expiresAt           time.Time
}

type issuedToken struct {
	clientID  string
	scope     string
	expiresAt time.Time
}

// NewOAuthServer lê clientes do env MCP_OAUTH_CLIENTS (formato
// "id1:redirect1|id2:redirect2"). O operator token vem de
// MCP_OAUTH_OPERATOR_TOKEN. Se ambos vazios, OAuth fica desligado
// (server.Server cai no Bearer estático tradicional).
func NewOAuthServer() *OAuthServer {
	clients := map[string]oauthClient{}
	for _, entry := range strings.Split(os.Getenv("MCP_OAUTH_CLIENTS"), "|") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		id, redirect, ok := strings.Cut(entry, ":")
		if !ok {
			continue
		}
		c := clients[id]
		c.ID = id
		c.RedirectURIs = append(c.RedirectURIs, redirect)
		clients[id] = c
	}
	return &OAuthServer{
		clients:       clients,
		pendingCodes:  map[string]pendingCode{},
		activeTokens:  map[string]issuedToken{},
		operatorToken: os.Getenv("MCP_OAUTH_OPERATOR_TOKEN"),
	}
}

// Enabled reporta se há ao menos um cliente registrado.
func (o *OAuthServer) Enabled() bool {
	return len(o.clients) > 0
}

// RegisterRoutes plugou os 4 endpoints OAuth no mux HTTP fornecido.
// Devolve um http.HandlerFunc que despacha; ou pode ser usado com
// um chi.Mux/etc.
func (o *OAuthServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/oauth/.well-known/oauth-authorization-server", o.handleMetadata)
	mux.HandleFunc("/oauth/authorize", o.handleAuthorize)
	mux.HandleFunc("/oauth/token", o.handleToken)
	mux.HandleFunc("/oauth/revoke", o.handleRevoke)
}

// ValidateToken confirma se o access_token está ativo e não expirou.
// Devolve scope + clientID associados. Chamado pelo server.Server quando
// OAuth está ligado (substitui a verificação do Bearer estático).
func (o *OAuthServer) ValidateToken(token string) (clientID, scope string, ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	t, exists := o.activeTokens[token]
	if !exists {
		return "", "", false
	}
	if time.Now().After(t.expiresAt) {
		delete(o.activeTokens, token)
		return "", "", false
	}
	return t.clientID, t.scope, true
}

// ---- handlers ----

// /oauth/.well-known/oauth-authorization-server (RFC 8414)
func (o *OAuthServer) handleMetadata(w http.ResponseWriter, r *http.Request) {
	issuer := schemeAndHost(r)
	doc := map[string]any{
		"issuer":                 issuer,
		"authorization_endpoint": issuer + "/oauth/authorize",
		"token_endpoint":         issuer + "/oauth/token",
		"revocation_endpoint":    issuer + "/oauth/revoke",
		"response_types_supported": []string{"code"},
		"grant_types_supported":    []string{"authorization_code"},
		"code_challenge_methods_supported": []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"}, // PKCE public client
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

// /oauth/authorize?response_type=code&client_id=...&redirect_uri=...
//                 &code_challenge=...&code_challenge_method=S256&state=...
//
// Em produção, redireciona para um login UI. Aqui, auto-aprova se o
// caller forneceu o header X-MCP-Operator-Token == MCP_OAUTH_OPERATOR_TOKEN.
func (o *OAuthServer) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("response_type") != "code" {
		http.Error(w, "unsupported_response_type", http.StatusBadRequest)
		return
	}
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	codeChallenge := q.Get("code_challenge")
	method := q.Get("code_challenge_method")
	state := q.Get("state")
	scope := q.Get("scope")

	o.mu.Lock()
	client, ok := o.clients[clientID]
	o.mu.Unlock()
	if !ok {
		http.Error(w, "invalid_client", http.StatusBadRequest)
		return
	}
	if !contains(client.RedirectURIs, redirectURI) {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	if codeChallenge == "" || method != "S256" {
		http.Error(w, "PKCE S256 obrigatório", http.StatusBadRequest)
		return
	}
	if o.operatorToken == "" {
		http.Error(w, "operator token não configurado", http.StatusServiceUnavailable)
		return
	}
	if subtle.ConstantTimeCompare(
		[]byte(r.Header.Get("X-MCP-Operator-Token")),
		[]byte(o.operatorToken),
	) != 1 {
		http.Error(w, "operator approval required", http.StatusUnauthorized)
		return
	}

	code := randomURLSafe(32)
	o.mu.Lock()
	o.pendingCodes[code] = pendingCode{
		clientID:            clientID,
		redirectURI:         redirectURI,
		codeChallenge:       codeChallenge,
		codeChallengeMethod: method,
		scope:               scope,
		expiresAt:           time.Now().Add(codeTTL),
	}
	o.mu.Unlock()

	// Redireciona para o client com ?code=...&state=...
	loc, _ := url.Parse(redirectURI)
	qs := loc.Query()
	qs.Set("code", code)
	if state != "" {
		qs.Set("state", state)
	}
	loc.RawQuery = qs.Encode()
	http.Redirect(w, r, loc.String(), http.StatusFound)
}

// POST /oauth/token (Content-Type: application/x-www-form-urlencoded)
//   grant_type=authorization_code
//   code=...
//   redirect_uri=...
//   client_id=...
//   code_verifier=...
func (o *OAuthServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, "invalid_request", err.Error())
		return
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		writeOAuthError(w, "unsupported_grant_type", "")
		return
	}

	code := r.PostForm.Get("code")
	clientID := r.PostForm.Get("client_id")
	redirectURI := r.PostForm.Get("redirect_uri")
	verifier := r.PostForm.Get("code_verifier")

	o.mu.Lock()
	pending, ok := o.pendingCodes[code]
	if ok {
		delete(o.pendingCodes, code) // single-use
	}
	o.mu.Unlock()

	if !ok {
		writeOAuthError(w, "invalid_grant", "code not found")
		return
	}
	if time.Now().After(pending.expiresAt) {
		writeOAuthError(w, "invalid_grant", "code expired")
		return
	}
	if pending.clientID != clientID {
		writeOAuthError(w, "invalid_grant", "client mismatch")
		return
	}
	if pending.redirectURI != redirectURI {
		writeOAuthError(w, "invalid_grant", "redirect_uri mismatch")
		return
	}
	// PKCE: verifica SHA256(verifier) == challenge.
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(challenge), []byte(pending.codeChallenge)) != 1 {
		writeOAuthError(w, "invalid_grant", "PKCE verifier mismatch")
		return
	}

	access := randomURLSafe(48)
	o.mu.Lock()
	o.activeTokens[access] = issuedToken{
		clientID:  pending.clientID,
		scope:     pending.scope,
		expiresAt: time.Now().Add(tokenTTL),
	}
	o.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": access,
		"token_type":   "Bearer",
		"expires_in":   int(tokenTTL.Seconds()),
		"scope":        pending.scope,
	})
}

// POST /oauth/revoke?token=... (RFC 7009)
func (o *OAuthServer) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	token := r.PostForm.Get("token")
	o.mu.Lock()
	delete(o.activeTokens, token)
	o.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// ---- helpers ----

func schemeAndHost(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func randomURLSafe(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func writeOAuthError(w http.ResponseWriter, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	out := map[string]any{"error": code}
	if desc != "" {
		out["error_description"] = desc
	}
	_ = json.NewEncoder(w).Encode(out)
}
