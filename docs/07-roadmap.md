# 07 — Roadmap

Fases de construção da plataforma. Cada fase tem **critérios de saída** — só passa para a próxima quando todos os critérios estão verdes.

A intenção é ter algo útil **em produção interna** já na Fase 1, e ir adicionando valor sem grandes rewrites.

> **Status (2026-05-20):** Fases 0–2 completas. Fase 3 ~50% feita (faltam OIDC e MCP Jira). Slice de testes/observabilidade iniciado fora-de-fase. Detalhe item a item abaixo.

## Fase 0 — Fundação — ✅ Completa

**Objetivo:** preparar o terreno para que a Fase 1 seja só código de produto.

- [x] Decisão D1 (stack) registrada em ADR — [adr/0001-stack-go-angular.md](adr/0001-stack-go-angular.md)
- [x] Decisão D2 (banco) registrada em ADR — [adr/0002-database-postgresql.md](adr/0002-database-postgresql.md)
- [x] Repo inicializado com lint, formatter, CI básico — `.golangci.yml`, `eslint.config.js`, `.github/workflows/ci.yml`
- [x] Esqueleto de migrations rodando contra Postgres local em Docker — serviço `migrate` no `docker-compose.yml` aplica automaticamente
- [x] Camada de auth/secret management definida — `internal/secret.Provider` com `EnvProvider`
- [x] Decisões D3 (deployment GitLab) e D4 (incidente Jira) com defaults configurados — `production_env_pattern` e `incident_jql` por projeto

**Critério de saída:** `docker compose --profile full up` provisiona Postgres 18 + Redis + migrate + api + worker + web em uma chamada. ✅

## Fase 1 — MVP de coleta — ✅ Completa

**Objetivo:** ingerir dados suficientes para calcular **uma** métrica (Deployment Frequency) de **um** projeto.

- [x] Cadastro de tenant / source_instance / project via CLI (`docker compose run --rm cli ...`)
- [x] Coletor GitLab via REST (polling 5 min via `asynq.Scheduler`) que descobre deployments do projeto
- [x] Persistência em `raw_event` (audit) + `deployment` + `environment` (idempotente por external_id)
- [x] Recálculo da agregação `metric_window` para janela 30d com DF (`compute:metric_window` task)
- [x] Endpoint REST `/api/v1/projects/{id}/metrics?window=...` retornando DF real

**Critério de saída:** com 1 projeto cadastrado, o endpoint retorna DF correta e o número se atualiza automaticamente após novo deploy (validado e2e). ✅

## Fase 2 — As 4 métricas — ✅ Completa

**Objetivo:** calcular as 4 métricas DORA completas a partir de GitLab + Jira (REST direto, ainda sem MCP).

- [x] Coletor de Merge Requests do GitLab (incluindo commits para `first_commit_at`)
- [x] Cálculo de Lead Time com correlação MR ↔ deployment (time-window, ver [06-data-model.md](06-data-model.md))
- [x] Coletor Jira via REST API v3 (issues conforme `incident_jql` do projeto, paginação por `nextPageToken`)
- [x] Cálculo de MTTR a partir de incidents (média de `resolved_at - created_at`)
- [x] Cálculo de CFR com associação por janela temporal pós-deploy (24h padrão)
- [x] Endpoint REST `/projects/{id}/metrics` retornando as 4 métricas + classificação combinada
- [x] Webhooks GitLab (validação `X-Gitlab-Token`) e Jira (HMAC-SHA256 em `X-Hub-Signature`) recebendo eventos
- [x] Job de reconciliação noturna (`reconcile:projects` @ `0 3 * * *`, `BackfillDays=7`)
- [x] Classificação Elite/High/Medium/Low configurável por tenant via `platform.classification_threshold`

**Critério de saída:** todas as 4 métricas calculadas corretamente, validadas via dataset sintético (15 deploys, 5 incidents). Mudanças em produção chegam em < 5 min via webhook ou em < 5 min via scheduler. ✅

## Fase 3 — Dashboard e MCP Jira — 🟡 Em andamento (~50%)

**Objetivo:** apresentação visual + migrar coleta Jira para MCP.

- [x] Frontend com os 4 tiles principais (Angular 21 + Material)
- [x] Séries temporais 90 dias — bar chart de deploys/dia via `/timeseries` + ng2-charts
- [x] Drill-down — clicar/abrir mostra lista de deployments via `/deployments`
- [ ] Autenticação OIDC — pendente (requer IdP do cliente)
- [ ] Refactor do coletor Jira para usar MCP Atlassian (`mcp.atlassian.com/v1/mcp`) com REST como fallback
- [ ] Multi-projeto e multi-time (filtros e agrupamentos no UI) — projeto já tem dropdown; agrupamento por time ainda não

**Critério de saída:** stakeholders conseguem abrir o dashboard, comparar 2 times, e identificar visualmente uma piora. Hoje conseguem ver 1 projeto por vez com tiles + curva + drill-down.

## Fase 4 — Alertas e múltiplos tenants — Pendente

**Objetivo:** operacionalizar como ferramenta de uso diário.

- [ ] Engine de alertas com regras configuráveis
- [ ] Webhook out → Teams, email
- [ ] Suporte a múltiplas `source_instance` simultâneas (já está no schema; falta exercitar)
- [ ] Suporte a múltiplos tenants reais (isolamento, billing-like — mesmo que internamente)
- [ ] Histórico mensal congelado (`metric_monthly_snapshot`) — tabela existe, falta o cron
- [ ] Exportação CSV/JSON

**Critério de saída:** time recebe alerta no Teams quando CFR ultrapassa limiar, com ruído controlado (regra de "mudança de estado", não disparar todo dia).

## Fase 5 — Servidor MCP próprio + análise — Pendente

**Objetivo:** expor as métricas para consumo por agentes/LLMs e adicionar análise contextual.

- [ ] Servidor MCP próprio expondo tools: `getDoraMetrics`, `getDeployments`, `compareTeams`, `explainTrend`
- [ ] Autenticação OAuth 2.1 do nosso MCP
- [ ] Tool `explainTrend` que combina dados + LLM para produzir narrativa
- [ ] Recursos: cada métrica acessível por URI MCP estável

**Critério de saída:** um SRE pergunta ao Claude "como está nosso CFR?" via desktop e recebe resposta com dados reais.

## Fase 6 — Métricas auxiliares e refinamentos (contínuo)

A partir daqui, evolução contínua sem big bangs. Backlog:

- Code Review Time, Pickup Time (sub-componentes do Lead Time)
- DORA Reliability (v2): integração com SLOs externos
- Predição: alertar antes de degradação, baseado em sinais antecedentes
- Comparação com benchmarks de indústria (anonimizado)
- Integração com PagerDuty/Opsgenie para incidentes

## Trilhas transversais

Não pertencem a uma fase específica; evoluem em paralelo.

### Testes e qualidade — 🟡 Iniciado

- [x] Unit tests do `calculator` — 100% coverage
- [x] Unit tests do cliente GitLab — 52% coverage (httptest)
- [ ] Unit tests do cliente Jira REST
- [ ] Integration tests dos handlers asynq (com Postgres real via Testcontainers)
- [ ] Testes E2E do API server (httptest + sqlc mock ou DB)
- [ ] Karma/Jasmine tests do frontend

### Observabilidade — Pendente

- [ ] Prometheus middleware no chi (request duration histogram + counter por route)
- [ ] Endpoint `/metrics` exposto
- [ ] Métricas custom de asynq (tasks processadas, latência, retries)
- [ ] Tracing OpenTelemetry (opcional)
- [ ] Dashboard Grafana exemplo (opcional)

### Segurança — Pendente

- [ ] OIDC para o frontend (Fase 3 também lista, mas é trilha transversal)
- [ ] Secret management real (Vault ou AWS Secrets Manager) — interface já existe, falta implementação
- [ ] Hardening de containers (revisar Dockerfile distroless)
- [ ] Rotação de credenciais GitLab/Jira

## Princípios para priorização

- **Valor antes de elegância:** Fase 1 pode ter código quadradinho, desde que mostre o número certo.
- **Não construir o que não vamos usar em < 1 sprint.** Multi-tenant existe no schema desde o dia 1, mas o UI multi-tenant só na Fase 4.
- **Cada fase deve poder ir a produção.** Não há fase "só refactor" — sempre entrega valor visível.
- **Mudar uma decisão é barato se ela está documentada.** ADRs por decisão importante.

## Riscos e mitigação

| Risco                                                                | Mitigação                                                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Webhooks GitLab/Jira pouco confiáveis                                | Reconciliação noturna (`reconcile:projects`) com `BackfillDays=7`; já implementado                 |
| Definição de "produção" varia entre projetos                         | `production_env_pattern` regex configurável por projeto; default cobre `prod`, `production`, `prod-*` |
| Atlassian muda capabilities do MCP server                            | Discovery dinâmico de tools planejado para Fase 3; mantemos fallback REST                          |
| Volume de dados explode em organizações grandes                      | Particionamento de `raw_event` por dia + retenção (ADR 0002 documenta gatilhos para Timescale)     |
| Time não confia nas métricas (data quality)                          | Drill-down até os eventos brutos já entregue na Fase 3                                             |
| Métrica gameada (deploy de MR vazio para inflar DF)                  | Pendente — reportar tamanho médio de mudança junto da DF; sinalizar anomalias                      |

## Fontes

- Doc de métricas: [01-dora-metrics.md](01-dora-metrics.md)
- Doc de arquitetura: [05-architecture.md](05-architecture.md)
- Doc de modelo de dados: [06-data-model.md](06-data-model.md)
