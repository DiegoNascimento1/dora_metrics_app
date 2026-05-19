# 0001 — Stack: Go (backend) + Angular (frontend)

- **Status:** Accepted
- **Data:** 2026-05-19
- **Autores:** Diego
- **Decisores:** Diego

## Contexto

A plataforma DORA tem dois eixos de carga bem distintos:

1. **Coletor + calculadora** — workload intensivo em I/O concorrente: polling de GitLab/Jira em paralelo, processamento de webhooks, reconciliação noturna. O componente mais crítico do ponto de vista operacional.
2. **Dashboard web** — aplicação de visualização típica de produto interno corporativo: gráficos, tabelas, filtros, drill-down, possivelmente auth contra IdP empresarial.

Essas duas cargas têm requisitos não-funcionais diferentes. Em vez de forçar uma única linguagem que serve "mais ou menos" os dois lados, optamos por especializar cada lado com a melhor ferramenta para o trabalho.

A decisão precisa ser tomada na Fase 0 (ver [07-roadmap.md](../07-roadmap.md#fase-0--funda%C3%A7%C3%A3o-1-sprint)) porque trava migrations, esqueleto de build/CI e templates de módulo. As três opções avaliadas foram detalhadas em [05-architecture.md § Decisão D1](../05-architecture.md#decis%C3%A3o-d1--stack).

## Decisão

**Backend em Go; frontend em Angular.**

Escopo prático:

- **Backend (Go ≥ 1.23):** `chi` para HTTP routing, `sqlc` + `pgx/v5` para acesso ao Postgres (SQL type-safe), `asynq` para filas/workers, `zerolog` para logs estruturados, `viper` para config. Estrutura de pasta `cmd/api`, `cmd/worker`, `internal/...`.
- **Frontend (Angular ≥ 17, standalone components):** Angular Material para componentes base, `ng2-charts` (Chart.js) para visualizações, `@ngrx/signals` para estado, `RxJS` para streams. Build com Vite via Angular CLI.
- **Comunicação:** API REST do backend documentada via OpenAPI; types gerados para o frontend via `openapi-typescript`.
- **Repos:** monorepo único com pastas `backend/` e `frontend/`. Compartilham apenas a especificação OpenAPI.

## Alternativas consideradas

- **Python (FastAPI + React)** — descartado porque o coletor é o trabalho pesado e Python tem complexidade adicional para concorrência (asyncio + uvicorn workers, GIL para CPU). O ganho do MCP SDK mais maduro não compensa o custo operacional. Pandas seria útil para backfill, mas não justifica a stack inteira.

- **Node/TypeScript (Fastify + Next.js)** — descartado a favor de Angular no front por preferência pelo modelo opinado e enterprise-ready do Angular para dashboards corporativos. No backend, Node serve bem mas o single-binary e o desempenho de I/O concorrente do Go são vantagens estruturais para o coletor.

## Consequências

### Positivas

- **Coletor altamente concorrente** sem juggling de event loops; goroutines + channels resolvem polling paralelo de N projetos elegantemente.
- **Deploy simples:** single binary do backend, container imagem do front (servido por Nginx ou similar).
- **SQL type-safe** via `sqlc` evita runtime errors comuns em ORMs dinâmicos.
- **Angular dá padronização** — menos discussão sobre como estruturar feature modules, gestão de estado etc.
- **Performance** boa por default: tempo de resposta da API e uso de memória previsíveis.

### Negativas

- **MCP SDK em Go é comunitário** (`mark3labs/mcp-go`), menos polido que os SDKs Python/TS oficiais da Anthropic.
- **Dois ecossistemas para o time manter** (Go + TypeScript/Angular). Cada PR de feature ponta-a-ponta toca dois mundos.
- **Angular tem curva de aprendizado** mais íngreme que React para quem não conhece (templates, DI, RxJS, NgModules vs Standalone).
- **Geração de types do backend para o front** introduz um passo extra no build (geração via OpenAPI). Sem disciplina, o front e o back saem de sync.
- **Sem Pandas/NumPy** — análises ad-hoc de dados ficam menos confortáveis. Para queries de exploração, usar `psql` direto ou notebook separado.

### Mitigação de riscos

- **MCP Go comunitário:** envolver acesso ao Atlassian MCP atrás de uma interface `JiraSource` com duas implementações (`MCPJiraSource`, `RESTJiraSource`). Começar com REST direto (que sabemos funcionar) e adicionar MCP em paralelo na Fase 3. Se `mcp-go` não der conta, implementamos um cliente JSON-RPC mínimo (~200 linhas, é HTTP + JSON-RPC 2.0).
- **Drift backend/frontend:** CI obriga `make generate-types` e falha se diff. PR template tem checkbox.
- **Curva Angular:** o produto não precisa de features avançadas (RxJS multicast complexo, lazy NgModules) no MVP. Começar com standalone components + signals = bem mais simples que Angular clássico.
- **Análise ad-hoc:** manter um README em `tools/notebooks/` mostrando como conectar Jupyter ao Postgres da plataforma para queries livres.

## Notas de implementação

```
dora_metrics_app/
├── backend/
│   ├── cmd/
│   │   ├── api/main.go              # servidor HTTP
│   │   └── worker/main.go           # workers asynq
│   ├── internal/
│   │   ├── collector/               # gitlab, jira, webhook
│   │   ├── calculator/              # cálculo das 4 métricas
│   │   ├── storage/                 # sqlc-generated + repository
│   │   └── api/                     # handlers REST
│   ├── migrations/                  # *.up.sql / *.down.sql (golang-migrate)
│   ├── sqlc.yaml
│   └── go.mod
├── frontend/
│   ├── src/app/
│   │   ├── core/                    # auth, http interceptors
│   │   ├── shared/                  # componentes reutilizáveis
│   │   └── features/                # dashboard, projects, alerts
│   ├── src/api-types.ts             # gerado via openapi-typescript
│   └── angular.json
├── openapi.yaml                     # contrato
├── docs/
└── docker-compose.yml
```

**Versões alvo (Fase 0, maio/2026):**

- **Go: `1.26`** (1.26.3 lançado 2026-05-07)
- **Angular: `22`** (lançado mai/2026; alternativa LTS = Angular 20, suportada até nov/2026)
- **Node.js (toolchain do front): `24 LTS`**
- **PostgreSQL: `18`** (ver [ADR 0002](0002-database-postgresql.md))

**Bibliotecas Go iniciais (pinadas):**

- `github.com/go-chi/chi/v5`
- `github.com/jackc/pgx/v5`
- `github.com/hibiken/asynq`
- `github.com/rs/zerolog`
- `github.com/spf13/viper`
- `github.com/xanzy/go-gitlab`
- `github.com/mark3labs/mcp-go` (avaliação na Fase 3 — pode ser substituído)

## Referências

- [docs/05-architecture.md § Decisão D1](../05-architecture.md#decis%C3%A3o-d1--stack)
- [docs/02-mcp-protocol.md § SDKs cliente](../02-mcp-protocol.md#sdks-cliente)
- [docs/07-roadmap.md § Fase 0](../07-roadmap.md#fase-0--funda%C3%A7%C3%A3o-1-sprint)
