// Service orquestra persistência das conexões OAuth + refresh
// automático. É a fachada que o resto do app (handlers REST, collector)
// usa — não devem mexer no DB ou no flow OAuth diretamente.
//
// Fluxo de uso típico do collector:
//
//   tok, err := svc.AccessToken(ctx, tenantID)
//   // se expires_at - now < 5min, faz refresh transparente
//   client := mcpclient.NewAtlassianClient("", tok)
package atlassian

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dora-metrics-app/backend/internal/storage"
)

// Service é a fachada.
type Service struct {
	DB     *storage.Pool
	Cipher *Cipher
	OAuth  *OAuthConfig

	// stateTTL controla a expiração do CSRF state.
	StateTTL time.Duration
	// refreshSafetyMargin: se expires_at - now < margin, refresh.
	RefreshSafetyMargin time.Duration
	// Now é injetável para testes.
	Now func() time.Time
}

// NewService aplica defaults sensatos.
func NewService(db *storage.Pool, cipher *Cipher, oauth *OAuthConfig) *Service {
	return &Service{
		DB:                  db,
		Cipher:              cipher,
		OAuth:               oauth,
		StateTTL:            10 * time.Minute,
		RefreshSafetyMargin: 5 * time.Minute,
		Now:                 time.Now,
	}
}

// Connection é a projeção sem dados sensíveis (UI consome).
type Connection struct {
	ID               uuid.UUID  `json:"id"`
	Provider         string     `json:"provider"`
	CloudID          string     `json:"cloudId,omitempty"`
	SiteURL          string     `json:"siteUrl,omitempty"`
	Scope            string     `json:"scope"`
	ExpiresAt        time.Time  `json:"expiresAt"`
	ConnectedAt      time.Time  `json:"connectedAt"`
	ConnectedBy      string     `json:"connectedBy,omitempty"`
	LastRefreshedAt  *time.Time `json:"lastRefreshedAt,omitempty"`
	LastRefreshError string     `json:"lastRefreshError,omitempty"`
	// Healthy = true se temos refresh_token válido e ainda dentro de
	// algum horizonte razoável (não fora de 90 dias sem uso).
	Healthy bool `json:"healthy"`
}

// ---- 1. Start: gera state CSRF e URL de redirect ----

// StartConnect cria um state opaco, persiste com TTL e devolve a URL
// pra onde o frontend redireciona o usuário.
func (s *Service) StartConnect(ctx context.Context, tenantID uuid.UUID, returnTo string) (string, string, error) {
	state, err := randomURLSafe(32)
	if err != nil {
		return "", "", err
	}
	if returnTo == "" {
		returnTo = "/settings"
	}
	_, err = s.DB.Pool.Exec(ctx, `
		INSERT INTO platform.oauth_state (state, tenant_id, provider, return_to, expires_at)
		VALUES ($1, $2, 'atlassian', $3, $4)
	`, state, tenantID, returnTo, s.Now().Add(s.StateTTL))
	if err != nil {
		return "", "", fmt.Errorf("save state: %w", err)
	}
	return s.OAuth.BuildAuthorizeURL(state), state, nil
}

// ---- 2. Callback: valida state, troca code, persiste ----

// CallbackResult traz o tenant_id resolvido (do state) + returnTo.
type CallbackResult struct {
	TenantID uuid.UUID
	ReturnTo string
	SiteURL  string
}

// CompleteConnect valida o state CSRF, troca code → tokens, descobre
// cloudId e persiste a conexão criptografada. `actor` é o identificador
// do usuário que iniciou (de OIDC, header ou "anonymous").
func (s *Service) CompleteConnect(ctx context.Context, state, code, actor string) (*CallbackResult, error) {
	// Valida state — single-use, expira em StateTTL.
	var row struct {
		TenantID uuid.UUID
		Provider string
		ReturnTo string
		Expires  time.Time
	}
	err := s.DB.Pool.QueryRow(ctx, `
		DELETE FROM platform.oauth_state
		WHERE state = $1
		RETURNING tenant_id, provider, return_to, expires_at
	`, state).Scan(&row.TenantID, &row.Provider, &row.ReturnTo, &row.Expires)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("state inválido ou já consumido (possível CSRF)")
		}
		return nil, fmt.Errorf("consume state: %w", err)
	}
	if s.Now().After(row.Expires) {
		return nil, errors.New("state expirado — tente conectar novamente")
	}

	// Troca code por tokens.
	tokens, err := s.OAuth.ExchangeCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	// Descobre cloudId / site URL.
	resources, err := ListAccessibleResources(ctx, tokens.AccessToken)
	if err != nil {
		// Não é fatal — conexão funciona pro MCP sem cloudId. Loga e segue.
		resources = nil
	}
	var cloudID, siteURL string
	if len(resources) > 0 {
		cloudID = resources[0].ID
		siteURL = resources[0].URL
	}

	// Criptografa e persiste.
	accessEnc, err := s.Cipher.Encrypt(tokens.AccessToken)
	if err != nil {
		return nil, err
	}
	refreshEnc, err := s.Cipher.Encrypt(tokens.RefreshToken)
	if err != nil {
		return nil, err
	}
	expiresAt := tokens.ExpiresAt(s.Now())

	_, err = s.DB.Pool.Exec(ctx, `
		INSERT INTO platform.oauth_connection
		    (tenant_id, provider, cloud_id, site_url,
		     access_token_enc, refresh_token_enc, expires_at, scope,
		     connected_by, last_refreshed_at)
		VALUES ($1, 'atlassian', $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, provider) DO UPDATE SET
		    cloud_id = EXCLUDED.cloud_id,
		    site_url = EXCLUDED.site_url,
		    access_token_enc = EXCLUDED.access_token_enc,
		    refresh_token_enc = EXCLUDED.refresh_token_enc,
		    expires_at = EXCLUDED.expires_at,
		    scope = EXCLUDED.scope,
		    connected_by = EXCLUDED.connected_by,
		    last_refreshed_at = EXCLUDED.last_refreshed_at,
		    last_refresh_error = NULL
	`, row.TenantID, cloudID, siteURL, accessEnc, refreshEnc, expiresAt, tokens.Scope, actor, s.Now())
	if err != nil {
		return nil, fmt.Errorf("save connection: %w", err)
	}

	return &CallbackResult{TenantID: row.TenantID, ReturnTo: row.ReturnTo, SiteURL: siteURL}, nil
}

// ---- 3. Get token (com refresh transparente) ----

// AccessToken devolve um access_token válido pro tenant. Renova
// automaticamente via refresh_token quando expires_at - now < margin.
// Retorna ErrNotConnected se o tenant não conectou ainda.
func (s *Service) AccessToken(ctx context.Context, tenantID uuid.UUID) (string, error) {
	var conn dbConnection
	err := s.DB.Pool.QueryRow(ctx, `
		SELECT id, cloud_id, access_token_enc, refresh_token_enc, expires_at, scope
		FROM platform.oauth_connection
		WHERE tenant_id = $1 AND provider = 'atlassian'
	`, tenantID).Scan(&conn.id, &conn.cloudID, &conn.accessEnc, &conn.refreshEnc, &conn.expires, &conn.scope)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotConnected
		}
		return "", fmt.Errorf("load connection: %w", err)
	}

	// Renova se está perto de expirar.
	if s.Now().Add(s.RefreshSafetyMargin).After(conn.expires) {
		access, err := s.refreshConnection(ctx, conn)
		if err != nil {
			return "", err
		}
		return access, nil
	}

	return s.Cipher.Decrypt(conn.accessEnc)
}

// CloudID retorna o cloudId persistido (útil pra URLs api.atlassian.com).
func (s *Service) CloudID(ctx context.Context, tenantID uuid.UUID) (string, error) {
	var cid sql.NullString
	err := s.DB.Pool.QueryRow(ctx, `
		SELECT cloud_id FROM platform.oauth_connection
		WHERE tenant_id = $1 AND provider = 'atlassian'
	`, tenantID).Scan(&cid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotConnected
		}
		return "", err
	}
	return cid.String, nil
}

// GetConnection devolve a projeção pública. Sem tokens.
func (s *Service) GetConnection(ctx context.Context, tenantID uuid.UUID) (*Connection, error) {
	var c Connection
	var lastRefreshed sql.NullTime
	var lastErr sql.NullString
	err := s.DB.Pool.QueryRow(ctx, `
		SELECT id, provider, COALESCE(cloud_id, ''), COALESCE(site_url, ''),
		       COALESCE(scope, ''), expires_at, connected_at,
		       COALESCE(connected_by, ''), last_refreshed_at, last_refresh_error
		FROM platform.oauth_connection
		WHERE tenant_id = $1 AND provider = 'atlassian'
	`, tenantID).Scan(&c.ID, &c.Provider, &c.CloudID, &c.SiteURL,
		&c.Scope, &c.ExpiresAt, &c.ConnectedAt,
		&c.ConnectedBy, &lastRefreshed, &lastErr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotConnected
		}
		return nil, err
	}
	if lastRefreshed.Valid {
		c.LastRefreshedAt = &lastRefreshed.Time
	}
	if lastErr.Valid {
		c.LastRefreshError = lastErr.String
	}
	c.Healthy = c.LastRefreshError == ""
	return &c, nil
}

// Disconnect remove a conexão. Tokens são apagados — Atlassian também
// permite revogar via /oauth/token/revoke se quiser ser estrito.
func (s *Service) Disconnect(ctx context.Context, tenantID uuid.UUID) error {
	_, err := s.DB.Pool.Exec(ctx, `
		DELETE FROM platform.oauth_connection
		WHERE tenant_id = $1 AND provider = 'atlassian'
	`, tenantID)
	return err
}

// ---- internals ----

type dbConnection struct {
	id         uuid.UUID
	cloudID    sql.NullString
	accessEnc  string
	refreshEnc string
	expires    time.Time
	scope      sql.NullString
}

// refreshConnection chama o /oauth/token com refresh_token, atualiza o
// registro e devolve o novo access_token. Falhas gravam last_refresh_error
// pra UI alertar.
func (s *Service) refreshConnection(ctx context.Context, conn dbConnection) (string, error) {
	refreshTok, err := s.Cipher.Decrypt(conn.refreshEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt refresh token: %w", err)
	}
	if refreshTok == "" {
		return "", errors.New("connection sem refresh_token — reconectar manualmente")
	}

	tokens, err := s.OAuth.RefreshToken(ctx, refreshTok)
	if err != nil {
		// Marca erro pra UI sem propagar.
		errMsg := err.Error()
		_, _ = s.DB.Pool.Exec(ctx, `
			UPDATE platform.oauth_connection
			SET last_refresh_error = $1
			WHERE id = $2
		`, errMsg, conn.id)
		return "", fmt.Errorf("refresh token: %w", err)
	}

	accessEnc, err := s.Cipher.Encrypt(tokens.AccessToken)
	if err != nil {
		return "", err
	}
	// Atlassian rotaciona refresh tokens — se voltou novo, atualizamos.
	newRefreshEnc := conn.refreshEnc
	if tokens.RefreshToken != "" {
		newRefreshEnc, err = s.Cipher.Encrypt(tokens.RefreshToken)
		if err != nil {
			return "", err
		}
	}

	_, err = s.DB.Pool.Exec(ctx, `
		UPDATE platform.oauth_connection
		SET access_token_enc = $1,
		    refresh_token_enc = $2,
		    expires_at = $3,
		    last_refreshed_at = $4,
		    last_refresh_error = NULL
		WHERE id = $5
	`, accessEnc, newRefreshEnc, tokens.ExpiresAt(s.Now()), s.Now(), conn.id)
	if err != nil {
		return "", fmt.Errorf("update refreshed tokens: %w", err)
	}
	return tokens.AccessToken, nil
}

// ErrNotConnected é devolvido quando o tenant não tem conexão ativa.
var ErrNotConnected = errors.New("atlassian: tenant não conectou — abra Settings > Conectar Jira")

// randomURLSafe gera string base64-URL (32 bytes ≈ 43 chars).
func randomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
