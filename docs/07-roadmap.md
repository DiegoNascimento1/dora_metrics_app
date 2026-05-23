# 07 — Roadmap

Fases de construção da plataforma. Cada fase tem **critérios de saída** — só passa para a próxima quando todos os critérios estão verdes.

A intenção é ter algo útil **em produção interna** já na Fase 1, e ir adicionando valor sem grandes rewrites.

> **Status (2026-05-22):** **Fases 0–6 ✅ todas completas.** Reliability v2 com 4 SLO providers pluggable (Datadog/Sentry/Prometheus+sloth/YAML); predição de degradação via regressão linear (funciona com histórico real OU sintético); novo alert_kind `predicted_regression` proativo. Secret management nos 3 grandes (Vault/AWS/Azure). Risco "métrica gameada" mitigado. 14 suites unit Go + 7 Testcontainers + 3 Karma. **Nenhum item bloqueante pendente** — só backlog livre de refinamentos.

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
- [x] CLI também expõe `collect now --project UUID` e `compute now --project UUID --window N` para disparo manual durante desenvolvimento/incidente
- [x] `platform.team` no schema com CRUD (sqlc) e FK opcional em `project.team_id` — tabela populada via SQL; sem endpoint de admin ainda (Fase 4)
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

## Fase 3 — Dashboard e MCP Jira — ✅ Completa

**Objetivo:** apresentação visual + migrar coleta Jira para MCP.

- [x] Frontend com os 4 tiles principais (Angular 21 + Material)
- [x] Séries temporais 90 dias — bar chart de deploys/dia via `/timeseries` + ng2-charts
- [x] Drill-down — clicar/abrir mostra lista de deployments via `/deployments`
- [x] Autenticação OIDC — `angular-auth-oidc-client@21` adicionada. Authorization Code Flow + PKCE + refresh token + silent renew. Config em runtime via `public/auth.config.js` (override por Docker mount sem rebuild). Service `AuthService` em `core/auth/` exposto + HTTP interceptor injetando `Bearer` em todas as requests para `/api/`. Botão Login/Logout no shell ativa só quando `enabled=true`. Callback route `/auth/callback`. **Para ativar:** preencher `public/auth.config.js` com authority/clientId do IdP. Pronto pra plugar Keycloak/Auth0/Okta/Azure AD sem mais código.
- [x] Refactor do coletor Jira para usar MCP Atlassian (`mcp.atlassian.com/v1/mcp`) com REST como fallback — cliente MCP HTTP/JSON-RPC em `internal/mcp/client/atlassian.go` (handshake `initialize` lazy + `tools/call` para `searchJiraIssuesUsingJql`); auth Bearer estático no MVP (env `JIRA_MCP_TOKEN`); `MCPSource.WithFallback(RESTSource)` cai automaticamente para REST se MCP errar. Testes httptest com mock JSON-RPC. ADR 0003 documenta a escolha. OAuth 2.1 fica para próxima iteração.
- [x] Multi-projeto e multi-time — dashboard tem toggle "Escopo" (projeto/time); quando time, métricas DORA são agregadas via SQL JOIN em `platform.project.team_id`. 5 queries novas (`*ForTeamInWindow`) + 2 endpoints `/api/v1/teams/{id}/metrics` + `/timeseries`. Drill-down + achievements ainda são project-scope only.

**Critério de saída:** stakeholders conseguem abrir o dashboard, comparar 2 times, e identificar visualmente uma piora. Hoje conseguem ver 1 projeto por vez com tiles + curva + drill-down.

## Fase 3.5 — Identidades unificadas (GitLab ↔ Jira) — 🟢 ~100% (members discovery entregue em ambas as fontes)

**Objetivo:** atribuir cada evento DORA (commit, MR, incident, deployment) a uma **pessoa real**, não a um username solto. Sem isso, Alice no GitLab (`alice_dev`) e Alice no Jira (`alice@acme.com`) viram dois "autores" diferentes, distorcendo per-person analytics e quem deve ser notificado em alertas.

### Modelo de dados (novo)

```
person                      Identidade canônica.
├── id, tenant_id, display_name, primary_email, avatar_url
└── created_at

person_identity             Vínculos com sistemas externos. N por pessoa.
├── person_id, source_instance_id, kind (gitlab|jira)
├── external_id              GitLab user ID (int) / Jira accountId (opaque)
├── external_username        gitlab "alice_dev" / jira "alice@acme.com"
├── external_email           opcional, ajuda no auto-match
└── linked_at, linked_by
```

### Itens

- [x] Migration 0006 — tabelas `platform.person` e `platform.person_identity`
- [x] Coletor GitLab: `ListProjectMembers` em `internal/collector/gitlab/client.go` — endpoint `/members/all` (inclui herdados do grupo), paginação por `X-Next-Page`, struct `Member{ID,Username,Name,PublicEmail,WebURL}` que casa com a heurística do `internal/identities` (email_exact + username_exact). Testes httptest cobrindo endpoint + paginação. Upsert em `person_identity` segue padrão do backfill via CLI (`cli people backfill`) — agora alimentável pela coleta automática quando o token estiver configurado.
- [x] Coletor Jira: `RESTSource.SearchUsers` chamando `/rest/api/3/users/search` com paginação por `startAt`/`maxResults`. Filtra server-side somente; client filtra `accountType=atlassian` + `active=true` (descarta bots e contas inativas, que o Jira não permite filtrar server-side). 4 testes httptest (happy path com filtragem, paginação 2 páginas, 401, respect limit).
- [x] Backfill: usernames de `merge_request.author_username` e `deployment.triggered_by` → `person_identity` unlinked (`cli people backfill`)
- [x] Auto-match heurístico: pacote `internal/identities` com email_exact (score 1.0) + username_exact (score 0.7); 97% cobertura de testes
- [x] Endpoints REST: `GET/POST /api/v1/people`, `GET /api/v1/identities/unlinked`, `GET /api/v1/identities/automatch`, `POST /api/v1/identities/{id}/link`
- [x] CLI: `cli people backfill | list-unlinked | create | link | automatch --tenant X`
- [x] Frontend: tela "Pessoas" (`/people`) com sugestões automatch, lista de pessoas vinculadas e identidades não-vinculadas. Botões "Aplicar sugestão" (cria pessoa + linka 2 identities) e "Criar nova pessoa" (a partir de 1 identidade)
- [x] Frontend: UX de **drag-and-drop** entre identidades — CDK drag-drop arrastando da coluna "Não vinculadas" para o card de uma pessoa; hover usa as classes nativas do CDK (`cdk-drop-list-receiving` / `cdk-drop-list-dragging`) tingindo o card-alvo em azul brand
- [x] Refactor: `merge_request.author_person_id` + `deployment.triggerer_person_id` (migration 0007) populados por `PropagatePersonToMergeRequests` / `PropagatePersonToDeployments`. Propagação automática após `LinkIdentityToPerson` (API + CLI) + CLI manual `cli people propagate`
- [x] Métricas por pessoa: `GET /api/v1/people/{id}/metrics?window=30d` (deploys triggered, lead time mediano, incidents vinculados). Render inline em cada person card no `/people`. Caveat ético registrado no endpoint + OpenAPI

**Caveat ético/cultural:** DORA é pensado para times, não indivíduos. Métricas por pessoa servem para identificar quem precisa de mentoria, NÃO para ranking punitivo. Documentar isso em [docs/01-dora-metrics.md](01-dora-metrics.md) e marcar a tela de métricas pessoais como "modo admin/coach", não dashboard público.

**Critério de saída:** dado um deploy GitLab disparado por `alice_dev` e um incident Jira aberto por `alice@acme.com`, o sistema atribui ambos à **mesma** pessoa Alice. Métricas DORA do time não dobram contagem.

## Fase 4 — Alertas e múltiplos tenants — ✅ Completa

**Objetivo:** operacionalizar como ferramenta de uso diário.

- [x] Engine de alertas com regras configuráveis — `platform.alert_rule` + `platform.alert_event` (migração `0010`), CRUD `/api/v1/alert-rules`, detecção plugada em `HandleComputeMetricWindow` (compara `previous_tier` vs `current_tier` da `metric_window` recém-gravada). Tipos: `tier_regression` (Elite→High etc) e `tier_change` (qualquer mudança). Frontend em `/alerts` com listagem + dialog de criação/edição + histórico de disparos.
- [x] Webhook out HTTP genérico — task asynq `dispatch:alert` envia POST JSON Slack-compatible (`{text, alert:{...}}`); status de entrega rastreado em `alert_event.delivery_status` (`pending`/`delivered`/`failed`) + `http_status` + `last_error` para retry/auditoria. 4xx não-transitório vira `SkipRetry`; 5xx/timeout/429 retry com backoff (até 5x). Teams e email são apenas variantes de webhook genérico (próximo passo: templates por destino).
- [x] Suporte a múltiplas `source_instance` simultâneas — agendador asynq já itera por projeto (cada projeto pode apontar a uma source-instance distinta); CLI `cli source-instance add` permite cadastrar quantas forem necessárias por tenant. Coleta paralela respeita `Queue: collect` (concurrency configurável via `WORKER_CONCURRENCY`). Múltiplas instâncias REST simultâneas testadas no integration suite.
- [x] Suporte a múltiplos tenants reais — `TenantMiddleware` em `internal/api/tenant.go` resolve tenant via 3 estratégias (header `X-Tenant-Slug`, subdomínio, query `?tenant=`), injeta `TenantInfo{ID,Slug}` no context. `RequireTenant` middleware adicional para rotas que exigem isolamento. 10 testes unitários cobrindo cada estratégia + edge cases (www/api ignorados, header wins, 2-label hosts). Queries existentes já filtram por `tenant_id`.
- [x] Histórico mensal congelado (`metric_monthly_snapshot`) — task `snapshot:monthly` agendada `0 0 1 * *` (1º dia do mês 00:00 UTC) lê o último `metric_window` 30d de cada projeto ativo e congela em `metric_monthly_snapshot` com mês = mês anterior. Idempotente
- [x] **Weekly digest** — task asynq `digest:weekly` agendada `0 9 * * 1` (segunda 09:00 UTC) calcula, por projeto e por time ativos: deploys da semana, incidents, tier atual vs anterior, top 3 contributors via `person_id`. Persiste em `platform.digest_snapshot` (migration 0011) com PK `(tenant, scope, iso_week)` → idempotente. Endpoints `GET /api/v1/{projects,teams}/{id}/digest?week=YYYY-Www`. Card `app-weekly-digest-card` no dashboard com botão "Copiar como markdown" (clipboard API)
- [x] Exportação CSV/JSON — `GET /api/v1/projects/{id}/export?kind=deployments|incidents|merge_requests&format=csv|json&window=30d`. CSV via `encoding/csv`; JSON pelo `writeJSON` padrão. Resposta com `Content-Disposition: attachment; filename="<kind>-<slug>-<window>-<date>.<ext>"`. Frontend: menu "Exportar" no dashboard (botão `mat-stroked-button` no header de filtros) com submenus por tipo e formato, usando `<a [href] download>` para download direto sem JS extra

**Critério de saída:** time recebe alerta no Teams quando CFR ultrapassa limiar, com ruído controlado (regra de "mudança de estado", não disparar todo dia).

## Fase 5 — Servidor MCP próprio + análise — ✅ Completa (OAuth 2.1 PKCE entregue)

**Objetivo:** expor as métricas para consumo por agentes/LLMs e adicionar análise contextual.

- [x] Servidor MCP próprio expondo tools: `getDoraMetrics`, `getDeployments`, `compareTeams`, `explainTrend` — binário `cmd/mcp-server` (porta `:8090`); pacote `internal/mcp/server` implementa JSON-RPC 2.0 sobre HTTP POST (handshake `initialize` + `tools/list` + `tools/call` + `resources/list` + `resources/read` + `ping`). Stack documentada em [ADR 0003](adr/0003-mcp-server-stack.md). Container distroless adicionado ao `docker-compose.yml` (profile `full`/`mcp`)
- [x] Autenticação OAuth 2.1 do nosso MCP — Authorization Code Flow com PKCE (S256) implementado em `internal/mcp/server/oauth.go`. Endpoints: `/oauth/.well-known/oauth-authorization-server` (RFC 8414), `/oauth/authorize`, `/oauth/token`, `/oauth/revoke` (RFC 7009). Clientes pré-cadastrados via env `MCP_OAUTH_CLIENTS=id:redirect|id2:redirect2` (DCR/RFC 7591 não suportado — produto interno). Auto-approval no `/authorize` quando header `X-MCP-Operator-Token` confere (substitui UI de login até IdP central existir). Token estático (`MCP_SERVER_TOKEN`) continua aceito como fallback durante rollout. 10 testes unitários cobrindo: metadata, authorize redirect, operator token required, unknown client, bad redirect URI, PKCE S256 required, token swap happy path, PKCE verifier mismatch, single-use code (replay protection), revoke.
- [x] Tool `explainTrend` — narrativa **determinística** template-based comparando metric_window atual vs anterior (sem LLM no MVP, hook documentado para integração futura)
- [x] Recursos: cada métrica acessível por URI MCP estável — `dora://project/{id}/dora-metrics`, `dora://team/{id}/dora-metrics`, `dora://schema` (thresholds DORA Report)

**Critério de saída:** um SRE pergunta ao Claude "como está nosso CFR?" via desktop e recebe resposta com dados reais. Atendido (modo Bearer estático).

## Fase 6 — Métricas auxiliares e refinamentos (contínuo) — ✅ Completa

Evolução contínua. Marcações:

- [x] **Code Review Time + Pickup Time** — `internal/calculator/subcomponents.go` decompõe o Lead Time em Pickup (commit → MR open), Review (MR open → merge) e Deploy Lag (merge → deploy). `AggregateLeadTime` calcula medianas. 7 testes unit cobrindo casos completos, parciais, negativos descartados, mediana ímpar/par, nil-tolerant agregação.
- [x] **Comparação com benchmarks de indústria (anonimizado)** — endpoint REST `GET /api/v1/benchmarks` + resource MCP estável `dora://benchmarks` expõe percentis p50/p75/p90 do DORA Report 2024. Frontend pode dizer "seu projeto está no percentil X" sem agregar dados de outros clientes.
- [x] **Integração com PagerDuty/Opsgenie para incidentes** — `internal/collector/alert_destinations.go` detecta destino pelo host da webhook_url e adapta o body: PagerDuty Events API v2 (com `dedup_key=event_id` para suprimir duplicatas + `event_action=resolve` em promoções), Opsgenie Alerts API v2 (auth via `GenieKey` no header, `api_key` removido da URL para não vazar). Destino genérico (Slack/Teams) preserva o formato original. 6 testes unit cobrindo detect + payload PagerDuty (severity por tier, resolve em promoção, exige routing_key), Opsgenie (alias dedup, priority P2 para low, tags), e no-op genérico.
- [x] **DORA Reliability v2** — interface `reliability.Provider` pluggable com 4 backends prontos: **Datadog** (API v1 SLO + history endpoint), **Sentry** (Performance stats_v2 → SLI sintético contra target via env), **Prometheus** (PromQL contra métricas do sloth/Pyrra/OpenSLO), **YAML** (Google SRE-style local file). `New(kind)` dispatch + `NoopProvider` default seguro. Status comum `SLOStatus{name,target,actual,errorBudget,periodDays,status}` exposto via `GET /api/v1/reliability/slos?scope=...` + `GET /api/v1/reliability/info`. Cobertura: 19 testes httptest (Datadog SLO+history+placeholder, Sentry SLI+scope filter, Prometheus query parser, YAML file load+filter+missing dir + helpers + JSON shape).
- [x] **Predição: alertar antes de degradação** — pacote `internal/prediction` com regressão linear OLS pura (~60 LOC, sem ML lib) sobre histórico de `metric_window`. `Predict(samples)` devolve `slopePerWeek`, `r2`, `direction` (degrading/improving/stable), `confidence` (low/medium/high pelo R²+sample size), `projectedTierIn`, `willBreachInDays` (extrapolação até cair para o próximo tier). Funciona com **histórico real ou sintético** (≥6 amostras). Endpoint `GET /api/v1/{projects,teams}/{id}/predict?lookback=180`. Task asynq `predict:weekly @ 0 10 * * 1` dispara `alert_event` com `kind=predicted_regression` (novo, migration 0012) quando `direction=degrading && confidence>=medium`. Idempotente por (rule, dia). 11 testes cobrindo: histórico insuficiente, degradação clara, improving, stable, filtra insufficient_data, breach projection, rankToTier boundaries, linear regression OLS, R² para série constante.

## Trilhas transversais

Não pertencem a uma fase específica; evoluem em paralelo.

### Design UX/UI — corporativo com pegada de gamificação — 🟡 Base entregue

**Princípio:** a plataforma é interna e tem uma audiência mista (engenharia, EM, direção). O visual precisa transmitir **confiança e seriedade corporativa** quando exposto em reunião de C-level, mas **engajar o time de eng** no dia-a-dia — DORA é um espelho, e times só olham num espelho que dá feedback emocional.

#### Identidade visual (base corporativa)

- [x] **Design system** com tokens versionados em [src/styles/_tokens.scss](../frontend/src/styles/_tokens.scss) (cores, espaçamento, tipografia, sombras, radius, transições)
- [x] Paleta: neutros sóbrios (navy `#1e3a8a`, slate-graphite, off-white) + **acentos reservados pra status** (Elite verde sóbrio, High azul, Medium âmbar, Low vermelho refinado)
- [x] Tipografia: Inter (variable) + JetBrains Mono pra SHAs/IDs, carregados via Google Fonts em [index.html](../frontend/src/index.html)
- [x] Tema **claro + escuro** com persistência — `ThemeService` reativo (signal + `prefers-color-scheme`) + toggle na toolbar + `localStorage`. Modos: `light` / `dark` / `system` (default). Chart e Material form-fields cobertos no dark
- [x] WCAG AA: paleta calibrada (Elite verde escuro sobre branco; chip Medium âmbar usa texto preto para contraste). Foco visível via `:focus-visible` global com outline brand 2px e offset 2-3px; só mostra em keyboard nav (não em mouse click)
- [x] Iconografia: Material Symbols Outlined (variable) carregado globalmente
- [x] Loading skeletons compartilhados (`<app-skeleton>` com 5 variantes + shimmer respeitando `prefers-reduced-motion`); aplicados em dashboard, projects, people, settings
- [x] Empty states desenhados (`<app-empty-state>` com ícone-em-círculo + título + descrição + slot CTA); 6 ocorrências nas páginas
- [x] Error states desenhados — `<app-error-state>` em `frontend/src/app/shared/error-state.component.ts` com 4 variantes (`network`/`not-found`/`forbidden`/`generic`), tons coordenados (warning/danger/muted via tokens), slot CTA, role=alert + aria-live=polite, slot opcional para detalhes técnicos em `<details>`. Aplicado em load fail do dashboard + compare. Aceitou também CTA para "Tentar novamente"
- [x] Logo SVG + favicon + theme-color + description meta (4 barras crescentes representando as 4 métricas DORA + dot Elite no topo)
- [x] Open graph — meta tags `og:title`, `og:description`, `og:type=website`, `og:image` (logo SVG), `og:url`, `og:locale=pt_BR`, twitter card `summary_large_image` em `frontend/src/index.html`

#### Camada de gamificação (engajamento)

Sem rebaixar a seriedade do produto — gamificação é **opt-in visual**, nunca métrica punitiva.

- [x] **Tier badges animados** — chip Elite tem animação `tier-breathe` 4s (scale 1.000→1.015, soft glow); respeita `prefers-reduced-motion`. Cada tier carrega ícone Material Symbol via `::before` (workspace_premium / trending_up / remove / trending_down / help) — atende SC 1.4.1 (cor não pode ser única forma de transmitir informação)
- [x] **Streaks** — endpoint `/projects/{id}/achievements` retorna `daysSinceLastIncident`; card no dashboard com contador grande + ícone `local_fire_department`. Empty state (`-1`) orienta a configurar `jira_project_keys`
- [x] **Team identity** — cada time tem nome, cor (hex CSS) e emoji. Tela `/teams` com cards coloridos + dialog de criação/edição (preview ao vivo, 8 cores Tailwind WCAG AA + 10 emojis sugeridos). Página `/projects` mostra chip do time. Migration 0009 + REST `/teams` CRUD + `/teams/{id}/projects` (assign) + `/projects/{id}/unassign-team`
- [x] **Achievements** (primeira batch — pacote `internal/gamification`, 100% test coverage):
    - 🔥 *Week Streak* — 7+ dias sem incident
    - 🛡️ *Steady Hand* — 30+ dias sem incident
    - 🛡️ *100 Green Days* — 100+ dias sem incident
    - 🏆 *Elite Tier* — classificação combinada = elite na janela atual
    Tiers de streak são mutuamente exclusivos (só o mais alto fica visível).
- [x] **Achievements** (segunda batch):
    - 🚀 *First Elite Month* — `EliteMonthsCount >= 1` lendo de `metrics.metric_monthly_snapshot`
    - ⚡ *Speed Demon* — Lead Time mediano < 1h com `sample_size >= 4` (proxy de "consistentemente rápido" enquanto não temos histórico semanal)
    - 🔁 *Recovery Master* — últimos 5 incidents resolvidos todos com MTTR < 1h (requer 5 reais — não desbloqueia em projeto sem dados)
- [x] **Achievements** (terceira batch):
    - 📈 *Most Improved* — salto de pelo menos 2 tiers ao longo do histórico curto. Conservador por design: regressão não desbloqueia; `insufficient_data` no início é ignorado no cálculo do mínimo. Drive: novo campo `TierProgressionLast3Months` em `ProjectStats` populado a partir de `metric_monthly_snapshot`. 5 testes cobrindo salto válido, salto de 1 rank (não desbloqueia), regressão, insufficient_data, mix com insufficient inicial.
- [x] **Leaderboard entre times** — rota `/leaderboard` com ranking por tier (Elite/High/Medium/Low) + tiebreaker por DF + alfabético. Badge "Liderando" no #1 (workspace_premium), "Em crescimento" no último (trending_up); copy no header reforça que é celebração, não ranking punitivo. Frontend-only por enquanto (forkJoin de N `getTeamMetrics`); endpoint dedicado `/leaderboard` quando time-count crescer
- [x] **Progress bars** mostrando "quão perto" o time está do próximo tier — `<mat-progress-bar>` fina (4px) em cada tile do dashboard + texto "+X.YZ para Elite" derivado client-side via helpers `nextTierProgress`/`cutoffsFor` em `frontend/src/app/shared/dora-tiers.ts` (replica os thresholds do backend até o `/metrics` devolver na resposta). "🏆 Você está no topo" quando já é Elite. Pacote dora-tiers.ts é testável (funções puras)
- [x] **Weekly digest** — card `<app-weekly-digest-card>` no dashboard com KPIs (deploys/incidents), delta de tier vs semana anterior, top 3 contributors. Botão "Copiar como markdown" usa Clipboard API para gerar release-notes-ready. Fetch via `GET /api/v1/{projects,teams}/{id}/digest`
- [x] **Team identity** — duplicado, já marcado [x] acima na linha de "Team identity"
- [x] **Micro-animações** — `tier-up`/`tier-breathe` no Elite chip (já existente) + animação `pop` (200ms) no painel de onboarding + `prefers-reduced-motion: reduce` honrado em ambos. Animações elaboradas (confete SVG tier-up) ainda em TODO mas a base de Material já inclui ripple sutil no clique de chip/botão

#### Discoverability e profundidade

- [x] **Onboarding tour** primeira visita — 4 steps (intro DORA / leitura de tile / curva 90d / drill-down). Service `OnboardingService` persiste flag em `localStorage` (`dora.tour.seen`); overlay com spotlight via `radial-gradient` (sem lib externa) + panel reposicionado contra viewport; respeita `prefers-reduced-motion`. Componente `<app-onboarding-tour />` montado no shell
- [x] **Tooltip explicativo** em cada métrica — ícone `info` no header de cada tile com `matTooltip` curto + link "saiba mais" abaixo da grid apontando para `docs/01-dora-metrics.md`. `aria-label` específico para leitor de tela
- [x] **"Por que esse tier?"** — clicar no chip de classificação abre `<app-tier-explain-dialog>` (Material Dialog) mostrando os 4 valores reais + cutoffs do tier atual e próximo, destacando em laranja a(s) métrica(s) que rebaixam o combinado (via `worstTier` que replica `WorstOf` do backend)
- [x] **Compare mode** — rota `/compare` com seletor multi-select de 2-4 times/projetos + tabela das 4 métricas DORA com "melhor por linha" em verde + chart com séries sobrepostas (reusa `<app-timeseries-chart>`). Link na toolbar com ícone `compare_arrows`
- [x] **Print-friendly** view — `_print.scss` (importado em `styles.scss`) com `@media print` ocultando navbar/sidebar/botões `.no-print`, forçando tinta legível, watermark canto inferior, page-break-inside avoid nos cards. Botão "Imprimir / PDF" no header do dashboard dispara `window.print()`

#### Anti-padrões explicitamente evitados

- ❌ Pontuação numérica individual (XP do desenvolvedor) — DORA é métrica de time
- ❌ Notificações de "alguém te ultrapassou" — competição saudável é entre times, não entre pessoas
- ❌ Loot boxes / surpresas — corporativo demanda previsibilidade
- ❌ Som / efeitos de áudio sem opt-in — ambiente de trabalho silencioso por default
- ❌ Mascotes 3D ou ilustrações infantilizadas — manter visual sóbrio

**Critério de saída da trilha:** stakeholder de C-level abre o dashboard sem treino e identifica em < 30s qual time precisa de atenção; engenheiros conferem o dashboard ≥ 1x/semana voluntariamente (medido por DAU/WAU do próprio app).

### Testes e qualidade — 🟡 Iniciado

- [x] Unit tests do `calculator` — 100% coverage
- [x] Unit tests do cliente GitLab — 52% coverage (httptest)
- [x] Unit tests do `internal/identities` (auto-match heurístico) — 97% coverage
- [x] Unit tests do `internal/gamification` (regras de achievements) — 100% coverage
- [x] CI executa `make test` (backend, com Postgres 18 como service) e `npm test -- --watch=false --browsers=ChromeHeadless` (frontend) em todo push/PR ([.github/workflows/ci.yml](../.github/workflows/ci.yml))
- [x] Unit tests do cliente Jira REST — 14 testes em `internal/collector/jira/source_test.go` (paginação por `nextPageToken`, 401/429/5xx mapeados em `APIError`, parsing completo de fields, 2 formatos de data Jira `-0300` e RFC3339, labels nil → slice vazio, body request inclui fields default, context timeout). Coverage 91.9%
- [x] Cliente MCP Atlassian — `internal/mcp/client/atlassian_test.go` com mock JSON-RPC (initialize, tools/call sucesso, isError=true, HTTP error, content vazio, default endpoint). Coverage 82.6%
- [x] Integration tests via Testcontainers — `internal/api/integration_test.go` tag `//go:build integration` sobe Postgres 18 real, aplica migrations via container `migrate/migrate`, testa `/healthz`, `/api/v1/projects` (lista vazia), `/api/v1/projects/{id}/metrics` (404). Target `make test-integration` no Makefile
- [x] Testes E2E do API server via Testcontainers — 6 cenários: `/healthz`, `/api/v1/projects` (lista vazia), `/api/v1/projects/{id}/metrics` (404), `/api/v1/teams/{id}/metrics` (404), `/api/v1/projects/{id}/digest` (404 sem snapshot), UUIDs malformados → 400 (3 paths), `/metrics` exposição Prometheus.
- [x] Karma/Jasmine specs reais — 3 suites cobrindo: `dora-tiers.spec.ts` (helpers puros: classifyMetric/worstTier/nextTierProgress/cutoffsFor/format), `error-state.component.spec.ts` (variantes, slot, aria), `onboarding-tour.service.spec.ts` (localStorage, start/next/prev/finish/reset). Run via `docker compose run --rm web npm test -- --watch=false`.

### Observabilidade — 🟡 Base entregue

- [x] Prometheus middleware no chi — `observability.HTTPMiddleware`, label por RoutePattern (não cardinalidade explosiva com path params), histogram de duração + counter por route+method+status
- [x] Endpoint `/metrics` exposto na API (`:8080/metrics`) e no worker (`:9090/metrics`, servidor HTTP dedicado)
- [x] Métricas asynq — `observability.AsynqMiddleware` envolve o ServeMux. `dora_asynq_tasks_total{type,status}` classifica success / error / skip_retry; `dora_asynq_task_duration_seconds{type}` histogram com buckets 50ms-5min (cobre coletas que batem em APIs remotas)
- [x] Tracing OpenTelemetry — `internal/observability/tracing.go` inicializa TracerProvider OTLP gRPC. Variáveis: `OTEL_EXPORTER_OTLP_ENDPOINT` (no-op se vazia), `OTEL_EXPORTER_OTLP_INSECURE=true`, `OTEL_SERVICE_VERSION`. Inicializado em `cmd/api`, `cmd/worker` e `cmd/mcp-server`. Shutdown idempotente com flush
- [x] Dashboard Grafana exemplo — `ops/grafana/dashboards/dora-overview.json` com 6 painéis (HTTP P95 por rota, throughput por status, asynq throughput/tipo, asynq error rate com thresholds 5%/20%, latência média de task, total requests 1h). Compose acessório em `ops/grafana/docker-compose.yml` sobe Prometheus + Grafana provisionados. README com queries PromQL completas

### Segurança — ✅ Completa

- [x] OIDC para o frontend — entregue na Fase 3 (`angular-auth-oidc-client@21` com Authorization Code Flow + PKCE).
- [x] Secret management real — **`VaultProvider`** (HashiCorp Vault KVv2) + **`AWSSecretsManagerProvider`** (AWS Secrets Manager via HTTP+SigV4 minimalista) + **`AzureKeyVaultProvider`** (Azure Key Vault via REST + AAD OAuth Client Credentials, token cache 60s). Os 3 providers compartilham a estratégia de lookup em 2 passos (chave direta `{prefix}-{key}` ou fallback `{prefix}-credentials` JSON-agrupado). Cobertura: Vault 6 testes, AWS 7 testes (inclui SigV4 Authorization bem-formado + session token), Azure 6 testes (inclui token cache reuse + sanitização underscore→hífen).
- [x] Hardening de containers — backend já era distroless (`gcr.io/distroless/static-debian12:nonroot`); frontend migrado de `nginx:alpine` para `nginxinc/nginx-unprivileged:1.27-alpine` rodando como uid 101 (sem root, sem CAP_NET_BIND_SERVICE). Porta interna :8080 (mapeamento compose 4200→8080). nginx.conf ganhou headers de hardening: X-Content-Type-Options nosniff, X-Frame-Options DENY, Referrer-Policy strict-origin-when-cross-origin, Permissions-Policy negando camera/mic/geo, Content-Security-Policy conservadora, `server_tokens off`. Healthcheck Docker no `/index.html`.
- [x] Rotação de credenciais GitLab/Jira — CLI `cli secrets check` valida que todos `auth_ref` das source-instances resolvem no provider corrente (exit 1 se algum falha). `cli secrets rotate --tenant X --source N --new-ref NEW` reaponta a source-instance para um novo nome de segredo sem tocar no valor (admin provisiona o novo segredo no backend → roda rotate → check → revoga o antigo).

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
| Métrica gameada (deploy de MR vazio para inflar DF)                  | `internal/calculator/gaming.go`: heurística `AnalyzeGaming` calcula % de MRs triviais (≤5 linhas) e mediana de tamanho na janela; sinaliza `gamingFlag=true` quando ≥50% dos deploys vieram de MRs triviais. Frontend renderiza aviso neutro ("considere revisar"). 9 testes unit cobrindo small sample, no-gaming, flag, ignora unknown, mediana, exato no threshold. |

## Fase 7 — Inteligência aumentada (IA contextual) — 🔵 Em implementação

**Objetivo:** transformar o dashboard de "ferramenta de medição" em "copiloto de Engineering Manager". O histórico já existe, o MCP já está em pé — fase de maior ROI imediato.

- [x] **LLM no `explainTrend`** — `internal/llm/client.go` com `anthropics/anthropic-sdk-go`; prompt caching no `system` prompt (thresholds DORA + glossário); input: snapshots `metric_window` atual + anterior + top 5 deploys + incidents; output: 2-3 parágrafos com causa provável + ações sugeridas. Fallback para template determinístico se `ANTHROPIC_API_KEY` não configurado. Tool MCP `explainTrend` atualizada.
- [x] **Anomaly detection multivariada** — `internal/prediction/anomaly.go` com z-score sobre DF + Lead Time + CFR simultâneos (janela deslizante 90d, threshold ±2σ). Endpoint `GET /api/v1/{projects,teams}/{id}/anomalies?window=90d`. Retorna lista de `{date, metric, z_score, direction, severity}`. Detecta spikes que a regressão linear por métrica única perderia (ex.: CFR sobe enquanto DF cai — sinal de crise silenciosa).
- [x] **Root cause analysis para incidents** — ao resolver incident, endpoint `GET /api/v1/incidents/{id}/root-cause` correlaciona com deploys das últimas 24h e lista os MRs suspeitos (por janela temporal e autor). Tool MCP `findRootCause`.
- [ ] Chat conversacional no dashboard — UI de chat que consome o MCP server próprio. Escopo: Fase 8.

**Critério de saída:** EM abre o dashboard sexta às 17h, lê a narrativa gerada, identifica em < 1min o que pedir para o time priorizar na próxima sprint.

## Fase 8 — Plataforma multi-fonte (GitHub + Linear) — 🔵 Em implementação

**Objetivo:** quebrar a dependência de GitLab+Jira. Maioria das empresas tem times em GitHub; Linear é o tracker moderno preferido por times de eng.

- [x] **Coletor GitHub** — `internal/collector/github/client.go`: REST API v3 (deployments, pull_requests, commits). Webhook handler em `api/webhooks.go` (event `push`, `pull_request`, `deployment_status`, `workflow_run`). Validação via `X-Hub-Signature-256`. Source instance `kind=github`. Migration `0014` adiciona `github` ao enum de source kinds. Tasks asynq `collect:github_deployments`, `collect:github_mrs`. Mapeamento: PR → `merge_request`, Deployment → `deployment`, Workflow Run falho → `incident`.
- [ ] Coletor Bitbucket Cloud — próxima iteração após GitHub estável.
- [ ] Coletor Azure DevOps — próxima iteração.
- [ ] Coletor Linear — GraphQL API para times que não usam Jira.
- [ ] Abstração `SourceProvider` formal — interface Go com `FetchDeployments`/`FetchMRs`/`FetchIncidents`; refactor dos coletores GitLab/GitHub para implementá-la.
- [ ] UI seleção de fonte — wizard `/settings/sources/new` com cards por provider + OAuth flow por kind.

**Critério de saída:** empresa com times no GitLab e no GitHub vê métricas DORA de todos os times no mesmo dashboard.

## Fase 9 — Onboarding self-service + Real-time — 🔵 Em implementação

**Objetivo:** reduzir time-to-first-value de "requer CLI + DBA" para "< 5 min via browser".

- [x] **Setup wizard frontend** — rota `/setup` com 4 steps: (1) criar tenant, (2) conectar fonte via OAuth/token, (3) escolher projetos, (4) backfill 30d + primeiro número. Substituição funcional dos comandos `cli tenant add` + `cli source-instance add` + `cli project add`. Componente `SetupWizardComponent` com `MatStepper`.
- [x] **Server-Sent Events** — endpoint `GET /api/v1/projects/{id}/metrics/stream` (SSE, `text/event-stream`). Após `HandleComputeMetricWindow` gravar nova `metric_window`, publica evento no Redis canal `metrics:{project_id}`. Handler SSE assina o canal e faz push ao cliente. Dashboard Angular assina SSE e atualiza tiles sem F5. Badge "Atualizado agora" aparece por 3s.
- [x] **Demo mode** — flag `?demo=true` na URL carrega dataset sintético pré-gerado (90 dias, 3 times, variação Elite→Medium→recovering). Sem login, sem backend live. `DemoService` injetado em lugar do `ApiClient` quando flag ativa. Útil para showroom com C-level.
- [ ] Slack/Teams app nativo — slash command `/dora <time>` retorna card rich. Requer signing secret + bot install flow.

**Critério de saída:** novo time entra no app, conecta GitHub via wizard, vê primeiro tile em < 5 min; deploy feito aparece em tiles em < 30s sem refresh.

## Fase 10 — Governança e escala

**Objetivo:** habilitar crescimento além de 50 projetos e adoção em organizações com requisitos de compliance.

- [ ] **RBAC granular** — roles `viewer`/`editor`/`admin` por team/project. Tabela `platform.permission` + middleware `RequireRole`. Hoje qualquer usuário logado vê tudo do tenant.
- [ ] **Audit log estruturado** — toda ação admin (criar source, editar alert_rule, linkar identity) vai para `platform.audit_event`. Endpoint `GET /api/v1/audit?actor=&action=&from=&to=`.
- [ ] **TimescaleDB para raw_event + metric_daily** — gatilho do ADR 0002 (> 50M linhas, P95 dashboard > 500ms). Migration converte tabela em hypertable; continuous aggregates substituem cron de `compute:metric_window` para janelas grandes.
- [ ] **Read replica para dashboard** — pgx pool com endpoint separado; queries de timeseries vão para replica.
- [ ] **Cache de séries temporais em Redis** — TTL 5 min para `/timeseries`, invalidação por webhook.
- [ ] **DCR (RFC 7591)** no servidor MCP — auto-registro de cliente OAuth.

**Critério de saída:** app comporta 500 projetos + 5000 usuários sem degradação perceptível; audit trail aceita revisão SOC2-like.

## Fase 11 — Além de DORA (SPACE + DevEx)

**Objetivo:** para times maduros que superaram as 4 métricas DORA e querem visão holística de developer experience.

- [ ] **SPACE metrics** — Satisfaction (survey trimestral integrado via `/surveys`), Performance (DORA já cobre), Activity (commits/MRs por person), Communication (PR review time já temos), Efficiency (focus time via integração calendário).
- [ ] **DevEx framework** (Forsgren/Houman/Storey) — feedback loops, cognitive load, flow state. Survey ligado a `person_id`, resultados em `platform.devex_response`.
- [ ] **Cost analytics** — deploys × tempo médio × engenheiros → custo estimado de delivery. Conecta com cloud spend (AWS Cost Explorer) por team.
- [ ] **Retrospective integration** — botão "Criar retro com este incident" gera template Markdown pré-preenchido e abre Confluence/Notion via API.
- [ ] **Coaching playbooks** — biblioteca de padrões anti-DORA (deploy às sextas, MRs gigantes, review tardio) com ações sugeridas; sistema sugere o playbook certo baseado no perfil do time.

**Critério de saída:** EMs e VPs usam o app como ferramenta de coaching ativa, não só relatório passivo.

## Fontes

- Doc de métricas: [01-dora-metrics.md](01-dora-metrics.md)
- Doc de arquitetura: [05-architecture.md](05-architecture.md)
- Doc de modelo de dados: [06-data-model.md](06-data-model.md)
