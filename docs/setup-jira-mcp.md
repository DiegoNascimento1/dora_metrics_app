# Conectar ao MCP do Jira (Atlassian Rovo)

Guia operacional para habilitar a coleta Jira via **MCP** em vez de REST.

> **TL;DR:** o coletor escolhe MCP quando `JIRA_MCP_TOKEN` está
> configurado; senão cai pra REST direto. Não é "ou um ou outro" no
> código — o `MCPSource` já vem com fallback automático para o
> `RESTSource`, então mesmo em falha do MCP a coleta continua.

## Arquitetura do que entregamos

```
┌──────────────────────────┐
│  collector handler       │
│  (cmd/worker)            │
└──────────┬───────────────┘
           │
           │  if JIRA_MCP_TOKEN != ""
           ▼
┌──────────────────────────┐    error  ┌─────────────────────────┐
│  MCPSource               │──────────►│  RESTSource (fallback)  │
│  internal/collector/jira │           │  /rest/api/3/search/jql │
└──────────┬───────────────┘           └─────────────────────────┘
           │  JSON-RPC tools/call
           ▼
┌──────────────────────────┐
│  mcp.atlassian.com/v1/mcp│
│  tool: searchJiraIssuesU │
│        singJql           │
└──────────────────────────┘
```

Código relevante:

- `backend/internal/mcp/client/atlassian.go` — cliente HTTP/JSON-RPC
  do MCP genérico
- `backend/internal/collector/jira/source.go` — `MCPSource`, `RESTSource`,
  `WithFallback()`
- `backend/internal/collector/handlers.go` — escolhe MCP vs REST com
  base em `JIRA_MCP_TOKEN`

## Passo a passo

### 1. Obter um Bearer Token para o Atlassian Rovo MCP

O servidor oficial é `https://mcp.atlassian.com/v1/mcp` e usa
**OAuth 2.1** em produção. Para o MVP, o nosso cliente aceita um
Bearer estático — qualquer um dos abaixo funciona:

**Opção A — Token OAuth (recomendado quando você tiver o app cadastrado):**

1. Acessar [Atlassian Developer Console](https://developer.atlassian.com/console/myapps/).
2. Criar um app OAuth 2.0 (3LO) ou usar um existente.
3. Adicionar os scopes:
   - `read:jira-work`
   - `read:jira-user`
4. Completar o flow OAuth uma vez (ex: via `curl` ou Postman) e
   armazenar o `access_token` final.
5. Esse access_token é seu `JIRA_MCP_TOKEN`.

**Opção B — Pessoal/dev:** seu próprio Atlassian Account Token
> Atlassian permite gerar tokens pessoais para o Rovo MCP em alguns
> tenants beta — verifique se aparece no seu admin. Se aparecer, use
> esse token diretamente.

**Opção C — Pular tudo:** não setar `JIRA_MCP_TOKEN`. O coletor cai
para REST e segue funcionando (já está configurado hoje).

### 2. Configurar as variáveis de ambiente

No `.env` do projeto (ou no orquestrador de container):

```env
# URL padrão do MCP Atlassian Cloud (default — pode omitir).
JIRA_MCP_URL=https://mcp.atlassian.com/v1/mcp

# Token Bearer obtido no passo 1. Vazio = usa REST.
JIRA_MCP_TOKEN=seu-token-aqui

# Mantenha o JIRA_API_TOKEN também — ele é o fallback automático
# quando o MCP retornar erro.
JIRA_EMAIL=você@empresa.com
JIRA_API_TOKEN=ATATT3xFfGF0...
JIRA_BASE_URL=https://acme.atlassian.net
```

> **Por que manter os 2:** se o token MCP expirar (oauth refresh
> falhar, scope removido, etc), o coletor continua coletando via REST
> e você recebe o sinal nos logs sem perder dados.

### 3. Subir o worker

```bash
docker compose --profile full up -d worker
docker compose logs -f worker | grep -i jira
```

No log você deve ver:

```
DBG jira coletor: MCP primary + REST fallback project_id=...
```

quando uma coleta de incidents Jira é disparada (a cada 5 min via
`scan:active_projects` ou imediatamente via `cli collect now`).

### 4. Validar que está usando MCP de verdade

Disparar coleta imediata + grep no log:

```bash
docker compose run --rm cli collect now --project <UUID>
docker compose logs worker --tail=200 | grep -E "atlassian-mcp|jira-rest"
```

- `source=atlassian-mcp` → MCP funcionou.
- `source=jira-rest` após `atlassian-mcp` → MCP falhou e fallback rodou.
  Veja o `mcpErr` no log linha imediatamente anterior pra saber por quê
  (token expirado, scope insuficiente, rate limit, etc).

## Troubleshooting

| Sintoma | Causa provável | Ação |
|---|---|---|
| `401 unauthorized` no MCP | Token inválido ou expirou | Renovar token; até lá, REST continua funcionando |
| `403 forbidden` no MCP | Faltam scopes `read:jira-work` / `read:jira-user` | Atualizar app no developer console |
| MCP responde mas issues vêm vazias | Conta do token não tem acesso aos projects | Adicionar o app/conta como member dos projects Jira |
| Coleta para de funcionar | Verifique se `JIRA_EMAIL` + `JIRA_API_TOKEN` ainda estão setados — o fallback REST depende deles | Manter REST sempre configurado |

## OAuth 2.1 completo (refresh automático)

Está fora do MVP. O `MCPSource` atual aceita só Bearer estático. Para
refresh automático, o roadmap futuro é:

1. Adicionar campo `oauth_refresh_token` em `platform.source_instance`.
2. Background job que renova o `access_token` antes do `expires_in`.
3. Trocar `JIRA_MCP_TOKEN` (env) por leitura via `secret.Provider`
   apontando para o token rotacionado.

Hoje, se você gera o token uma vez por mês e atualiza o env, está
coberto para a maioria dos casos. Para Cloud com refresh < 24h,
escrever um wrapper externo (cron) que regenere o token e reinicie o
worker é o caminho mais simples.

## Validar credenciais sem rodar o worker

CLI tem comando próprio:

```bash
docker compose run --rm cli secrets check
```

Lista todos os `auth_ref` das source-instances e verifica se o
provider corrente consegue resolver. Para rotação:

```bash
docker compose run --rm cli secrets rotate \
  --tenant acme --source jira-prod --new-ref JIRA_MCP_TOKEN_V2
```

## Referências

- [Atlassian Rovo MCP server](https://www.atlassian.com/blog/announcements/remote-mcp-server)
- [Model Context Protocol spec](https://modelcontextprotocol.io)
- [Atlassian OAuth 2.0 (3LO) docs](https://developer.atlassian.com/cloud/jira/platform/oauth-2-3lo-apps/)
- [ADR 0003 — escolha de stack do MCP server](adr/0003-mcp-server-stack.md)
- Código: [internal/mcp/client/atlassian.go](../backend/internal/mcp/client/atlassian.go), [internal/collector/jira/source.go](../backend/internal/collector/jira/source.go)
