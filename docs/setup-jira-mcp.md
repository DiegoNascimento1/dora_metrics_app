# Conectar ao Jira (Atlassian Rovo MCP)

Há **3 caminhos**, em ordem de preferência:

1. **OAuth 3LO via UI** — admin clica "Conectar Jira" em Settings,
   autoriza no Atlassian, tokens ficam criptografados no servidor e
   renovados automaticamente. **É o caminho recomendado.**
2. **JIRA_MCP_TOKEN via env** — Bearer estático no `.env`. Útil em
   CI/dev sem UI.
3. **REST com Basic auth** (email + API token) — sempre ativo como
   fallback automático se MCP falhar.

O coletor escolhe na ordem acima. Não é "ou/ou": o MCP tem fallback
automático para REST se algo der errado, então a coleta não para.

## 1) OAuth 3LO via UI (recomendado)

### 1.1 Cadastrar o app uma vez no Atlassian Developer Console

Você só precisa fazer isso **uma vez por instalação** do DORA Metrics.
As credenciais do app servem para todos os tenants — o que muda é o
usuário (Atlassian account) que autoriza.

1. Acesse <https://developer.atlassian.com/console/myapps/>
2. Crie um app OAuth 2.0 (3LO).
3. Em **Permissions**, adicione os scopes:
   - `read:jira-work`
   - `read:jira-user`
   - `offline_access` (necessário para refresh tokens)
4. Em **Authorization > OAuth 2.0 (3LO)**, configure o callback URL:
   ```
   https://seu-dora.example.com/api/v1/integrations/atlassian/callback
   ```
   (em dev: `http://localhost:8080/api/v1/integrations/atlassian/callback`)
5. Copie **Client ID** e **Client Secret**.

### 1.2 Gerar chave de criptografia

Tokens OAuth são guardados criptografados (AES-256-GCM) no Postgres.
Gere a chave master:

```bash
openssl rand -base64 32
```

### 1.3 Setar env vars no backend

No `.env`:

```env
# Credenciais do app OAuth (NÃO confundir com tokens de usuário).
ATLASSIAN_OAUTH_CLIENT_ID=abcdef123456
ATLASSIAN_OAUTH_CLIENT_SECRET=xyz...
ATLASSIAN_OAUTH_REDIRECT_URI=http://localhost:8080/api/v1/integrations/atlassian/callback

# Chave master para AES-256-GCM dos tokens. Tratar como secret.
OAUTH_ENCRYPTION_KEY=<saída do openssl rand -base64 32>
```

Subir tudo:

```bash
docker compose --profile full up -d
```

### 1.4 Conectar pela UI

1. Acesse `http://localhost:4200/settings`.
2. No card **"Conectar Jira (Atlassian Rovo)"**, clique no botão
   **Conectar Jira**.
3. Você é redirecionado para `auth.atlassian.com` — autorize o app.
4. Volta para `/settings?atlassian=connected` mostrando site, cloudId,
   scopes e quando o token expira.

A partir desse momento o coletor usa os tokens daquele tenant. O
worker renova automaticamente antes de expirar (margem de 5min).

### 1.5 Validar

```bash
# Status pela API:
curl -H "X-Tenant-Slug: acme" \
     http://localhost:8080/api/v1/integrations/atlassian/status

# Disparar coleta imediata e ver qual fonte usou:
docker compose run --rm cli collect now --project <UUID>
docker compose logs worker --tail=200 | grep "jira coletor"
# → token_source=oauth-3lo (se a conexão está ativa)
# → token_source=env-static (se caiu pra JIRA_MCP_TOKEN)
```

### 1.6 Desconectar

Botão "Desconectar" em Settings ou via API:

```bash
curl -X DELETE -H "X-Tenant-Slug: acme" \
     http://localhost:8080/api/v1/integrations/atlassian/connection
```

Os tokens são apagados do DB. A coleta cai automaticamente para REST
(se `JIRA_API_TOKEN` configurado) ou para erro até alguém reconectar.

## 2) JIRA_MCP_TOKEN via env (sem UI)

Para CI/dev sem login interativo, basta colar um Bearer no `.env`:

```env
JIRA_MCP_TOKEN=<access_token>
JIRA_MCP_URL=https://mcp.atlassian.com/v1/mcp  # default, pode omitir
```

Útil quando você obtém o token via outro meio (Postman, curl pro flow
OAuth). O coletor usa esse token se não houver conexão OAuth no DB.

## 3) REST direto (fallback automático)

Mantenha sempre configurado — é o que salva quando o MCP falha:

```env
JIRA_BASE_URL=https://acme.atlassian.net
JIRA_EMAIL=você@empresa.com
JIRA_API_TOKEN=ATATT3xFfGF0...
```

Gere o `JIRA_API_TOKEN` em <https://id.atlassian.com/manage-profile/security/api-tokens>.

## Arquitetura do flow OAuth 3LO

```
┌──────────────────────┐         ┌──────────────────────┐
│  Frontend /settings  │  POST   │  /authorize          │
│  Card Conectar Jira  ├────────►│  gera state, salva   │
└──────────┬───────────┘ /az     │  TTL 10min, devolve  │
           │                     │  authorize URL       │
           │                     └─────────┬────────────┘
           │  302 redirect                 │
           ▼                               │
┌──────────────────────┐                   │
│  auth.atlassian.com  │                   │
│  /authorize          │                   │
│  user autoriza       │                   │
└──────────┬───────────┘                   │
           │  302 com code+state           │
           ▼                               │
┌──────────────────────┐                   │
│  /callback           │  valida state     │
│                      │  POST /oauth/token│
│  - consome state     │  → access+refresh │
│  - exchange code     │                   │
│  - cifra + persiste  │  AES-256-GCM      │
│  - redirect pra UI   │  OAUTH_ENCRYPTION │
└──────────────────────┘  _KEY             │

Em runtime (worker, cada coleta):
  1. Service.AccessToken(tenant)
     - se expires < now+5min: refresh + salva
     - decifra e devolve
  2. MCPSource(token).WithFallback(REST)
```

## Troubleshooting

| Sintoma | Causa | Ação |
|---|---|---|
| Botão "Conectar Jira" não aparece | `ATLASSIAN_OAUTH_CLIENT_ID` ou `OAUTH_ENCRYPTION_KEY` vazios | Setar env + reiniciar API |
| Redirect: `?atlassian_error=access_denied` | Usuário negou no Atlassian | Tentar novamente |
| Redirect: `?atlassian_error=oauth_failed` | Code expirou (state TTL 10min) ou client_id errado | Verificar logs: `docker compose logs api \| grep atlassian` |
| `connected: true` mas `lastRefreshError` populado | Refresh token expirou ou foi revogado no Atlassian | Reconectar (Desconectar → Conectar Jira) |
| Tokens não decriptam após reiniciar | `OAUTH_ENCRYPTION_KEY` mudou | Não mudar a chave em produção; usuários precisam reconectar |
| Coletor não usa OAuth (log `token_source=env-static`) | Conexão DB não existe pro tenant ou refresh falhou | Confirmar `getAtlassianStatus` connected=true |

## Rotação da OAUTH_ENCRYPTION_KEY

Mudar a chave invalida todos os tokens guardados. Hoje: trocar a chave
→ todas as conexões precisam ser **refeitas pela UI**. Não há perda de
dados (só do token, que é descartável); usuários apenas refazem o flow
OAuth. Rotação com 2 chaves simultâneas é roadmap futuro.

## Referências

- [Atlassian OAuth 2.0 (3LO)](https://developer.atlassian.com/cloud/jira/platform/oauth-2-3lo-apps/)
- [Atlassian Rovo MCP server](https://www.atlassian.com/blog/announcements/remote-mcp-server)
- [Model Context Protocol spec](https://modelcontextprotocol.io)
- Código: [internal/integrations/atlassian/](../backend/internal/integrations/atlassian/), [internal/mcp/client/atlassian.go](../backend/internal/mcp/client/atlassian.go)
- ADR: [docs/adr/0003-mcp-server-stack.md](adr/0003-mcp-server-stack.md)
