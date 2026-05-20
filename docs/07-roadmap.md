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

## Fase 3 — Dashboard e MCP Jira — 🟡 Em andamento (~50%)

**Objetivo:** apresentação visual + migrar coleta Jira para MCP.

- [x] Frontend com os 4 tiles principais (Angular 21 + Material)
- [x] Séries temporais 90 dias — bar chart de deploys/dia via `/timeseries` + ng2-charts
- [x] Drill-down — clicar/abrir mostra lista de deployments via `/deployments`
- [ ] Autenticação OIDC — pendente (requer IdP do cliente)
- [ ] Refactor do coletor Jira para usar MCP Atlassian (`mcp.atlassian.com/v1/mcp`) com REST como fallback
- [ ] Multi-projeto e multi-time (filtros e agrupamentos no UI) — projeto já tem dropdown; agrupamento por time ainda não

**Critério de saída:** stakeholders conseguem abrir o dashboard, comparar 2 times, e identificar visualmente uma piora. Hoje conseguem ver 1 projeto por vez com tiles + curva + drill-down.

## Fase 3.5 — Identidades unificadas (GitLab ↔ Jira) — 🟢 ~85% (faltam só ListMembers GitLab/Jira + drag-and-drop UX)

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
- [ ] Coletor GitLab: `ListProjectMembers` em `internal/collector/gitlab/client.go` + upsert em `person_identity` (depende de token GitLab real)
- [ ] Coletor Jira: chamada a `/rest/api/3/users/search` + upsert idêntico (depende de token Jira real)
- [x] Backfill: usernames de `merge_request.author_username` e `deployment.triggered_by` → `person_identity` unlinked (`cli people backfill`)
- [x] Auto-match heurístico: pacote `internal/identities` com email_exact (score 1.0) + username_exact (score 0.7); 97% cobertura de testes
- [x] Endpoints REST: `GET/POST /api/v1/people`, `GET /api/v1/identities/unlinked`, `GET /api/v1/identities/automatch`, `POST /api/v1/identities/{id}/link`
- [x] CLI: `cli people backfill | list-unlinked | create | link | automatch --tenant X`
- [x] Frontend: tela "Pessoas" (`/people`) com sugestões automatch, lista de pessoas vinculadas e identidades não-vinculadas. Botões "Aplicar sugestão" (cria pessoa + linka 2 identities) e "Criar nova pessoa" (a partir de 1 identidade)
- [x] Frontend: UX de **drag-and-drop** entre identidades — CDK drag-drop arrastando da coluna "Não vinculadas" para o card de uma pessoa; com hover state "drop-zone-active" tingindo o card alvo
- [x] Refactor: `merge_request.author_person_id` + `deployment.triggerer_person_id` (migration 0007) populados por `PropagatePersonToMergeRequests` / `PropagatePersonToDeployments`. Propagação automática após `LinkIdentityToPerson` (API + CLI) + CLI manual `cli people propagate`
- [x] Métricas por pessoa: `GET /api/v1/people/{id}/metrics?window=30d` (deploys triggered, lead time mediano, incidents vinculados). Render inline em cada person card no `/people`. Caveat ético registrado no endpoint + OpenAPI

**Caveat ético/cultural:** DORA é pensado para times, não indivíduos. Métricas por pessoa servem para identificar quem precisa de mentoria, NÃO para ranking punitivo. Documentar isso em [docs/01-dora-metrics.md](01-dora-metrics.md) e marcar a tela de métricas pessoais como "modo admin/coach", não dashboard público.

**Critério de saída:** dado um deploy GitLab disparado por `alice_dev` e um incident Jira aberto por `alice@acme.com`, o sistema atribui ambos à **mesma** pessoa Alice. Métricas DORA do time não dobram contagem.

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

### Design UX/UI — corporativo com pegada de gamificação — 🟡 Base entregue

**Princípio:** a plataforma é interna e tem uma audiência mista (engenharia, EM, direção). O visual precisa transmitir **confiança e seriedade corporativa** quando exposto em reunião de C-level, mas **engajar o time de eng** no dia-a-dia — DORA é um espelho, e times só olham num espelho que dá feedback emocional.

#### Identidade visual (base corporativa)

- [x] **Design system** com tokens versionados em [src/styles/_tokens.scss](../frontend/src/styles/_tokens.scss) (cores, espaçamento, tipografia, sombras, radius, transições)
- [x] Paleta: neutros sóbrios (navy `#1e3a8a`, slate-graphite, off-white) + **acentos reservados pra status** (Elite verde sóbrio, High azul, Medium âmbar, Low vermelho refinado)
- [x] Tipografia: Inter (variable) + JetBrains Mono pra SHAs/IDs, carregados via Google Fonts em [index.html](../frontend/src/index.html)
- [ ] Tema **claro + escuro** com persistência — tokens já preparados para `[data-theme="dark"]`, falta toggle UI + storage
- [x] WCAG AA: paleta calibrada (Elite verde escuro sobre branco; chip Medium âmbar usa texto preto para contraste). Foco visível: pendente revisar todos os interativos
- [x] Iconografia: Material Symbols Outlined (variable) carregado globalmente
- [ ] Empty states, loading skeletons e error states desenhados (mat-spinner básico hoje; falta polish)
- [ ] Logo + favicon + open graph

#### Camada de gamificação (engajamento)

Sem rebaixar a seriedade do produto — gamificação é **opt-in visual**, nunca métrica punitiva.

- [x] **Tier badges animados** — chip Elite tem animação `tier-breathe` 4s (scale 1.000→1.015, soft glow); respeita `prefers-reduced-motion`. Pendente: pareamento com ícone Material por tier (cor sozinha não basta para acessibilidade)
- [ ] **Streaks** — "23 dias sem Change Failure" com fogo emoji ou ícone de chama; quebra de streak mostra duração anterior + botão "retomar"
- [ ] **Achievements / conquistas** (desbloqueáveis por time):
    - 🚀 *First Elite Month* — primeiro mês inteiro classificação Elite combinada
    - 🛡️ *100 Green Days* — 100 dias sem incident production-impacting
    - ⚡ *Speed Demon* — Lead Time mediano < 1h por 4 semanas consecutivas
    - 🔁 *Recovery Master* — MTTR < 1h em 5 incidents consecutivos
    - 📈 *Most Improved* — maior salto de tier no trimestre
- [ ] **Leaderboard entre times** do mesmo tenant (opt-in por tenant) — comparação por tier combinado e por métrica individual. Sem punição: bottom-team aparece como "in growth"
- [ ] **Progress bars** mostrando "quão perto" o time está do próximo tier (ex: "+0.12 deploys/dia para Elite")
- [ ] **Weekly digest** — card semanal: "essa semana 12 deploys, 0 incidents, +1 tier em LT" — formato compartilhável (PNG/Slack-friendly)
- [ ] **Team identity** — cada time escolhe nome, cor, mascote/emoji opcional (afeta o leaderboard e os cards)
- [ ] **Micro-animações** parceiras de eventos: deploy success (✓ verde sutil), tier-up (confete discreto SVG, < 800ms), incident closed (badge fade-in)

#### Discoverability e profundidade

- [ ] **Onboarding tour** primeira visita (4 steps: o que é DORA, leitura de tile, tour da curva, drill-down)
- [ ] **Tooltip explicativo** em cada métrica com link pra [docs/01-dora-metrics.md](01-dora-metrics.md) ancorado na seção
- [ ] **"Por que esse tier?"** — clicar no chip de classificação abre painel mostrando os 4 valores + cutoffs configurados, com destaque do que rebaixou
- [ ] **Compare mode** — botão "comparar" pega 2-4 times/projetos lado-a-lado (gráficos sobrepostos + delta destacado)
- [ ] **Print-friendly** view para exportar relatório mensal em PDF (CSS print + watermark)

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
- [x] CI executa `make test` (backend, com Postgres 18 como service) e `npm test -- --watch=false --browsers=ChromeHeadless` (frontend) em todo push/PR ([.github/workflows/ci.yml](../.github/workflows/ci.yml))
- [ ] Unit tests do cliente Jira REST
- [ ] Integration tests dos handlers asynq (com Postgres real via Testcontainers)
- [ ] Testes E2E do API server (httptest + sqlc mock ou DB)
- [ ] Karma/Jasmine specs do frontend (CI roda, mas só há specs default geradas pelo Angular)

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
