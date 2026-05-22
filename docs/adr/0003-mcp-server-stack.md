# ADR 0003 — Stack do servidor MCP próprio

> **Status:** Aceito
> **Data:** 2026-05-22
> **Decisor:** time DORA Metrics
> **Contexto da decisão:** Fase 5 do [roadmap](../07-roadmap.md).

## Contexto

A Fase 5 do roadmap exige um **servidor MCP (Model Context Protocol) próprio**
que exponha as métricas DORA como tools e resources para LLMs / agentes
(Claude Desktop, IDEs, automações).

Há três caminhos plausíveis:

1. Adotar o SDK Go oficial: `github.com/modelcontextprotocol/go-sdk`
2. Implementar JSON-RPC 2.0 manualmente sobre HTTP POST
3. Usar um SDK Python e expor via gRPC / sidecar

## Decisão

Implementação **custom JSON-RPC 2.0 sobre HTTP** (opção 2), em
`backend/internal/mcp/server`.

## Razões

- **SDK Go pré-1.0.** O `modelcontextprotocol/go-sdk` mudou de API
  quebrante 3 vezes em ~6 meses. Vendorá-lo agora implica retrabalho
  recorrente, sem ganho funcional — a superfície que usamos
  (`initialize`, `tools/list`, `tools/call`, `resources/list`,
  `resources/read`) cabe em ~250 LOC. Quando o SDK estabilizar em ≥ 1.0,
  é trivial migrar.

- **Coerência de stack.** Backend é Go 1.26 monolítico (`api`, `worker`,
  `cli`). Adicionar um sidecar Python só pelo MCP introduz: outra runtime
  para hospedar, outra cadeia de build, outra dependência operacional. O
  custo de operar duas linguagens supera o benefício do SDK Python.

- **Auth simples no MVP.** OAuth 2.1 completo (PKCE + Dynamic Client
  Registration) é o que a spec MCP exige, mas o cenário de uso interno
  (DORA Metrics é ferramenta privada do time) admite *Bearer estático*
  via env `MCP_SERVER_TOKEN`. Marcamos TODO no `cmd/mcp-server/main.go`
  para a evolução.

- **Transport HTTP, não stdio.** A spec MCP define dois transports:
  stdio (filho do cliente) e Streamable HTTP. Escolhemos HTTP porque o
  servidor compartilha o mesmo banco da `api` e roda em container — o
  cliente conecta remoto, não como subprocesso.

## Consequências

**Positivas**

- Zero dependência externa nova específica de MCP — só `net/http` +
  `encoding/json` da stdlib.
- Tests `httptest` cobrem 100% do dispatcher; integração com o resto
  (DB, queries sqlc) reusa o pool que `api` e `worker` já usam.
- Container `mcp-server` minúsculo (mesmo Dockerfile base distroless da
  api).

**Negativas / dívida técnica**

- Quando o SDK Go estabilizar, é provável que valha migrar — perdemos
  feature parity automática com a spec (ex: streaming/SSE incremental,
  signed prompts).
- OAuth 2.1 fica para iteração futura.
- A tool `getDeployments` está em estado parcial — devolve apenas o
  metadata da janela, sem lista paginada. Resolver com nova query sqlc
  `ListProjectDeploymentsForWindow` quando houver caso de uso real.

## Implementação

| Arquivo | Função |
|---|---|
| `backend/internal/mcp/server/server.go` | Dispatcher JSON-RPC + handlers de initialize/tools-list/resources |
| `backend/internal/mcp/server/tools.go` | Implementação das 4 tools (`getDoraMetrics`, `getDeployments`, `compareTeams`, `explainTrend`) |
| `backend/internal/mcp/server/server_test.go` | 14 testes cobrindo dispatcher + auth + parsing |
| `backend/cmd/mcp-server/main.go` | Binário standalone (porta `:8090` default) |
| `backend/internal/mcp/client/atlassian.go` | Reuso: cliente MCP genérico usado pelo coletor Jira refatorado |

## Outros caminhos considerados e rejeitados

- **SDK Python via sidecar:** custo operacional alto para benefício baixo (ver acima).
- **gRPC entre `mcp-server` e clientes:** quebra interoperabilidade — clientes MCP esperam HTTP+JSON.
- **WebSocket:** spec MCP fala em Streamable HTTP (SSE), não WS. Implementar SSE quando precisarmos de eventos progressivos.
