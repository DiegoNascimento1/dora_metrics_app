-- OAuth 2.0 3LO connections com providers externos (Atlassian/Jira).
--
-- Cada tenant pode ter uma conexão por provider. Tokens são guardados
-- criptografados (AES-GCM) com chave master em env OAUTH_ENCRYPTION_KEY
-- — DB dump por si só não revela tokens utilizáveis.
--
-- Fluxo:
--   1. usuário clica "Conectar Jira" → POST /authorize devolve URL pro
--      Atlassian + state CSRF guardado em platform.oauth_state.
--   2. Atlassian redireciona para /callback?code=...&state=...
--   3. Backend troca code por access_token + refresh_token via POST
--      auth.atlassian.com/oauth/token, salva criptografado.
--   4. Worker usa Service.AccessToken(tenant) — renova via refresh
--      token quando expires_at < now() + 5min.

CREATE TABLE platform.oauth_connection (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES platform.tenant(id) ON DELETE CASCADE,
    provider            TEXT NOT NULL CHECK (provider IN ('atlassian')),
    -- Cloud ID retornado pelo Atlassian (1 por site). Necessário para
    -- compor URLs api.atlassian.com/ex/jira/{cloudId}/.../search/jql
    -- quando NÃO usar o MCP. Para MCP, o token já carrega o escopo.
    cloud_id            TEXT,
    -- Atlassian site URL ("acme.atlassian.net") — útil pra UI mostrar.
    site_url            TEXT,
    -- Tokens ciphered (base64 do envelope nonce+ciphertext+tag).
    access_token_enc    TEXT NOT NULL,
    refresh_token_enc   TEXT,
    -- Quando o access_token expira (NOW + expires_in).
    expires_at          TIMESTAMPTZ NOT NULL,
    -- Espaço-delimitado, como vem da resposta OAuth.
    scope               TEXT,
    -- Para auditar: quem (usuário admin) iniciou a conexão.
    connected_by        TEXT,
    connected_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Última vez que renovamos com sucesso. Telemetria de saúde.
    last_refreshed_at   TIMESTAMPTZ,
    -- Última falha de refresh (se houver). Aviso na UI.
    last_refresh_error  TEXT,
    UNIQUE (tenant_id, provider)
);

CREATE INDEX idx_oauth_connection_tenant
    ON platform.oauth_connection (tenant_id, provider);

-- State CSRF temporário do flow OAuth. TTL curto.
-- Não FK em tenant pra permitir state "pre-tenant" no futuro
-- (sign-up via OAuth), mas hoje sempre tem tenant_id.
CREATE TABLE platform.oauth_state (
    state               TEXT PRIMARY KEY,            -- random URL-safe ~32 bytes
    tenant_id           UUID NOT NULL,
    provider            TEXT NOT NULL,
    -- Onde o frontend quer voltar depois do OAuth (default /settings).
    return_to           TEXT NOT NULL DEFAULT '/settings',
    expires_at          TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_oauth_state_expires
    ON platform.oauth_state (expires_at);

COMMENT ON TABLE platform.oauth_connection IS
    'OAuth 3LO connections com providers externos. Tokens encriptados via OAUTH_ENCRYPTION_KEY.';
COMMENT ON TABLE platform.oauth_state IS
    'State CSRF temporário do flow OAuth. TTL 10min.';
