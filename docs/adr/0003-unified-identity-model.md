# 0003 — Modelo de identidades unificadas (GitLab ↔ Jira)

- **Status:** Accepted
- **Data:** 2026-05-20
- **Autores:** Diego
- **Decisores:** Diego

## Contexto

Toda métrica DORA agregada por time é confiável; toda métrica DORA agregada por **pessoa** depende de saber que `alice_dev` no GitLab e `alice@acme.com` no Jira são a mesma humana. Sem esse vínculo:

- O dashboard conta "10 contribuidores" quando na verdade são 5 pessoas com 2 identidades cada
- Métricas pessoais (Lead Time individual, recorrência de incidents) ficam fragmentadas
- Alertas direcionados ("o último deploy quebrou — avisar quem disparou") batem no username sem contexto, em vez de na pessoa que pode resolver

Esta ADR consolida as decisões tomadas durante a entrega da Fase 3.5 (commits `31b2136`, `e2bd28c`, `3813cab`, `af7c432`, `245ce04`, `12baf28`, `3bb9c50`).

## Decisão

Adotar um modelo de **identidade canônica com identidades fonte N:1**, materialização opcional nos eventos e auto-match heurístico que sugere mas não decide.

### Schema (migration 0006 + 0007)

```
platform.person                  Identidade canônica.
├── id (UUID), tenant_id, display_name, primary_email, avatar_url, created_at
└── 1 person : N identities

platform.person_identity         Vínculo com sistema externo.
├── id, tenant_id, person_id (NULL = unlinked), source_instance_id
├── kind ('gitlab' | 'jira'), external_id, external_username, external_email
├── linked_at, linked_by, created_at
└── UNIQUE (tenant_id, kind, external_username)

platform.merge_request.author_person_id      UUID NULL  (materialização)
platform.deployment.triggerer_person_id      UUID NULL  (materialização)
```

### Fluxo

1. **Backfill:** `cli people backfill --tenant X` lê `merge_request.author_username` e `deployment.triggered_by`, cria uma linha `person_identity` por username distinto (kind=gitlab, sem person_id).
2. **Auto-match:** pacote `internal/identities` produz **sugestões** rankeadas (email_exact 1.0, username_exact 0.7) entre identities cross-kind (gitlab × jira). Nunca persiste sozinho.
3. **Aprovação humana:** via CLI (`cli people link`), REST (`POST /identities/{id}/link`) ou UI (drag-and-drop em `/people`).
4. **Propagação:** após cada `LinkIdentityToPerson`, queries CTE `PropagatePersonToMergeRequests` / `PropagatePersonToDeployments` populam `author_person_id` / `triggerer_person_id` nos eventos.
5. **Métricas pessoais:** queries específicas (`CountDeploymentsByPersonInWindow`, `LeadTimeMedianByPersonInWindow`, `CountIncidentsLinkedToPersonInWindow`) usam as FKs materializadas — sem JOIN por username a cada leitura.

## Alternativas consideradas

- **Sem schema canônico, só JOIN por username em query time.** Mais simples mas o frontend não tem um conceito de "pessoa" pra exibir, métricas pessoais ficam frágeis a mudanças de username, e o operador não pode merger duas contas que mudaram email. Descartado.

- **Auto-match autônomo (sem aprovação humana).** Seria mais rápido na operação mas inevitavelmente comete erros — falsos positivos custariam horas para diagnosticar quando aparecessem em alertas/relatórios. Descartado em favor de "máquina sugere, humano decide".

- **Materializar `person_id` direto em todos os eventos (merge_request, deployment, incident, commit).** Considerado, mas para a Fase 3.5 entrega só MR + deployment cobre as 4 métricas DORA. Incidents ficam mapeados por `jira_project_key` → `project.jira_project_keys`. Refactor de `commit` (se um dia tivermos commit-level data) é separado.

- **OAuth-driven identity (cada usuário liga seu próprio GitLab+Jira).** Solução "certa" a longo prazo, mas pressupõe OIDC funcionando e adoção voluntária. Operacionalmente impossível na Fase 3.5; adotamos backfill+merge admin-driven como caminho mais rápido.

## Consequências

### Positivas

- **Único registro por humano** — UI e analytics ganham um "quem é quem" estável que sobrevive a mudanças de username
- **Performance** — queries de métricas pessoais não fazem JOIN cross-table por username; `WHERE author_person_id = $1` usa índice parcial
- **Auditável** — `linked_at` / `linked_by` deixam rastro de quem decidiu cada merge
- **Reversível** — `UnlinkIdentity` reverte a decisão sem perder o histórico
- **Sem schema lock-in pro futuro** — quando Atlassian MCP server expor accountId resolvido, ou quando OIDC chegar, o `person_identity` simplesmente ganha mais linhas; o resto do código não muda

### Negativas

- **Backfill manual** é necessário em projetos com histórico — `cli people backfill` + `cli people automatch` + revisão; pode levar 1-2h de operador em uma org de 100+ pessoas
- **Materialização exige propagate** — após cada link, eventos antigos precisam ser atualizados. Resolvemos com propagação automática no link, mas alguém que mude credentials direto no banco precisa rodar `cli people propagate` manualmente
- **Métricas pessoais são politicamente sensíveis** — facilmente weaponizáveis para ranking punitivo. Mitigado documentando o caveat, mas o risco social existe

### Mitigação de riscos

- **Caveat ético** documentado em [docs/01-dora-metrics.md](../01-dora-metrics.md) e no godoc do endpoint `personMetricsDTO` — métricas pessoais são para **coaching/mentoria**, nunca ranking
- **Heurísticas conservadoras** — só email_exact (1.0) e username_exact (0.7); nada de Levenshtein/fuzzy até termos confiança e telemetria
- **Mínimo viável de UI** — drag-and-drop oferece feedback claro do que está sendo decidido; nada de auto-merge silencioso

## Notas de implementação

**Onde o código vive:**

- Schema: [migrations/0006_create_person_tables.up.sql](../../backend/migrations/0006_create_person_tables.up.sql) + [0007_add_person_fks_to_events.up.sql](../../backend/migrations/0007_add_person_fks_to_events.up.sql)
- Heurística: [internal/identities/automatch.go](../../backend/internal/identities/automatch.go) (97% test coverage)
- Queries: [internal/storage/sql/queries/people.sql](../../backend/internal/storage/sql/queries/people.sql)
- API handlers: [internal/api/people.go](../../backend/internal/api/people.go)
- CLI: 6 subcomandos sob `people` em [cmd/cli/main.go](../../backend/cmd/cli/main.go)
- UI: [frontend/src/app/features/people/people.component.ts](../../frontend/src/app/features/people/people.component.ts) (drag-and-drop CDK)

**Caveat técnico registrado:** o primeiro draft das queries `Propagate*` usou `UPDATE ... FROM table JOIN table ON ...`, que o Postgres parseia mas não resolve `mr.project_id` no JOIN ON do FROM porque a tabela target não está no FROM. Reescrevi como CTE que monta o set `(event_id, person_id)` primeiro, depois `UPDATE ... FROM cte`. Documentado em commit `245ce04`.

**Pendências fora desta ADR:**

- Coletor de membros direto via GitLab `ListProjectMembers` e Jira `/users/search` (depende de tokens reais)
- Refactor pra `incident.reporter_person_id` (incidents ainda mapeiam por jira_project_key)
- Métrica de "frequência de incident como reporter vs assignee" (Fase 6)

## Referências

- [docs/07-roadmap.md § Fase 3.5](../07-roadmap.md#fase-35--identidades-unificadas-gitlab--jira---)
- [docs/01-dora-metrics.md](../01-dora-metrics.md) — caveat ético sobre métricas pessoais
- [docs/06-data-model.md](../06-data-model.md) — modelo de dados consolidado
- Commits da Fase 3.5: `31b2136`, `e2bd28c`, `3813cab`, `af7c432`, `245ce04`, `12baf28`, `3bb9c50`
