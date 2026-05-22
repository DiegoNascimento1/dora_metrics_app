// Atlassian OAuth 2.0 (3LO) — Authorization Code Flow para o admin do
// tenant conectar a conta Jira na UI da plataforma DORA.
//
// Endpoints oficiais (https://developer.atlassian.com/cloud/jira/platform/oauth-2-3lo-apps/):
//
//   GET  https://auth.atlassian.com/authorize         (redirect com state)
//   POST https://auth.atlassian.com/oauth/token       (code → access+refresh)
//   GET  https://api.atlassian.com/oauth/token/accessible-resources
//                                                     (cloud_id + site URL)
//
// Scopes que pedimos (mínimos pra DORA):
//   read:jira-work   — listar issues
//   read:jira-user   — resolver accountId → display name
//   offline_access   — REQUIRED pra ganhar refresh_token
//
// Em produção, o app OAuth é registrado uma vez no Developer Console
// (https://developer.atlassian.com/console/myapps/) e as credenciais
// vão como env vars NO BACKEND (não na UI). Cada tenant usa as mesmas
// credenciais; o que muda é o token gerado por usuário/tenant.
package atlassian

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// OAuthConfig é a config global do app OAuth (uma por instalação).
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string // ex: https://dora.acme.com/api/v1/integrations/atlassian/callback
	Scopes       []string
}

// NewOAuthConfigFromEnv lê:
//
//	ATLASSIAN_OAUTH_CLIENT_ID
//	ATLASSIAN_OAUTH_CLIENT_SECRET
//	ATLASSIAN_OAUTH_REDIRECT_URI   (default http://localhost:8080/api/v1/integrations/atlassian/callback)
func NewOAuthConfigFromEnv() (*OAuthConfig, error) {
	id := os.Getenv("ATLASSIAN_OAUTH_CLIENT_ID")
	sec := os.Getenv("ATLASSIAN_OAUTH_CLIENT_SECRET")
	if id == "" || sec == "" {
		return nil, fmt.Errorf("ATLASSIAN_OAUTH_CLIENT_ID + ATLASSIAN_OAUTH_CLIENT_SECRET obrigatórios")
	}
	redirect := os.Getenv("ATLASSIAN_OAUTH_REDIRECT_URI")
	if redirect == "" {
		redirect = "http://localhost:8080/api/v1/integrations/atlassian/callback"
	}
	return &OAuthConfig{
		ClientID:     id,
		ClientSecret: sec,
		RedirectURI:  redirect,
		Scopes: []string{
			"read:jira-work",
			"read:jira-user",
			"offline_access", // necessário pra refresh_token
		},
	}, nil
}

// BuildAuthorizeURL devolve a URL pra onde o frontend redireciona o
// usuário. `state` deve ser opaco, gerado no backend e guardado em
// platform.oauth_state com TTL curto pra validar no callback.
func (c *OAuthConfig) BuildAuthorizeURL(state string) string {
	q := url.Values{}
	q.Set("audience", "api.atlassian.com")
	q.Set("client_id", c.ClientID)
	q.Set("scope", strings.Join(c.Scopes, " "))
	q.Set("redirect_uri", c.RedirectURI)
	q.Set("state", state)
	q.Set("response_type", "code")
	q.Set("prompt", "consent") // força tela de consentimento (UX previsível)
	return "https://auth.atlassian.com/authorize?" + q.Encode()
}

// TokenSet é o payload normalizado da resposta /oauth/token.
type TokenSet struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int    // segundos
	Scope        string // espaço-delimitado
	TokenType    string // sempre "Bearer"
}

// ExpiresAt converte ExpiresIn para timestamp absoluto.
func (t TokenSet) ExpiresAt(now time.Time) time.Time {
	return now.Add(time.Duration(t.ExpiresIn) * time.Second)
}

// ExchangeCode troca o `code` retornado pelo Atlassian por access +
// refresh tokens. Chamado no /callback após validar o state CSRF.
func (c *OAuthConfig) ExchangeCode(ctx context.Context, code string) (*TokenSet, error) {
	body := url.Values{}
	body.Set("grant_type", "authorization_code")
	body.Set("client_id", c.ClientID)
	body.Set("client_secret", c.ClientSecret)
	body.Set("code", code)
	body.Set("redirect_uri", c.RedirectURI)

	return c.postToken(ctx, body)
}

// RefreshToken troca o refresh_token por um novo access_token (e novo
// refresh, conforme rotation policy do Atlassian).
func (c *OAuthConfig) RefreshToken(ctx context.Context, refreshToken string) (*TokenSet, error) {
	body := url.Values{}
	body.Set("grant_type", "refresh_token")
	body.Set("client_id", c.ClientID)
	body.Set("client_secret", c.ClientSecret)
	body.Set("refresh_token", refreshToken)
	return c.postToken(ctx, body)
}

func (c *OAuthConfig) postToken(ctx context.Context, body url.Values) (*TokenSet, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://auth.atlassian.com/oauth/token", strings.NewReader(body.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("post token: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("atlassian token http %d: %s", resp.StatusCode, respBody)
	}

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("atlassian devolveu access_token vazio: %s", respBody)
	}
	return &TokenSet{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ExpiresIn:    out.ExpiresIn,
		Scope:        out.Scope,
		TokenType:    out.TokenType,
	}, nil
}

// AccessibleResource é o que /accessible-resources retorna por site.
type AccessibleResource struct {
	ID        string   `json:"id"`     // cloudId — UUID estável
	Name      string   `json:"name"`
	URL       string   `json:"url"`    // ex: https://acme.atlassian.net
	Scopes    []string `json:"scopes"`
	AvatarURL string   `json:"avatarUrl"`
}

// ListAccessibleResources resolve o cloudId — necessário pra compor URLs
// api.atlassian.com/ex/jira/{cloudId}/... quando NÃO usa o MCP. Para
// MCP, o token já carrega o site no escopo.
func ListAccessibleResources(ctx context.Context, accessToken string) ([]AccessibleResource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.atlassian.com/oauth/token/accessible-resources", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("accessible-resources: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("accessible-resources http %d: %s", resp.StatusCode, body)
	}
	var out []AccessibleResource
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode accessible-resources: %w", err)
	}
	return out, nil
}

// httpClient devolve um client default. Sobrescrito em testes via
// httpClientOverride (variável de pacote).
var httpClientOverride *http.Client

func httpClient() *http.Client {
	if httpClientOverride != nil {
		return httpClientOverride
	}
	return &http.Client{Timeout: 15 * time.Second}
}
