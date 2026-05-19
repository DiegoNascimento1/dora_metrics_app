# 06 — Modelo de dados

Modelo lógico — independente do banco escolhido (ver [Decisão D2](#decisão-d2--banco)). DDL exemplificado em PostgreSQL, mas a estrutura se transpõe.

## Princípios

1. **Eventos imutáveis no fundo.** Tudo que vem do GitLab/Jira é gravado em uma tabela append-only. Agregações são derivadas e recomputáveis.
2. **Idempotência por ID externo.** Toda inserção/atualização chaveada por `(source, external_id)`.
3. **UTC sempre.** Toda coluna de tempo em UTC. Conversões só na borda de apresentação.
4. **Multi-tenant desde o dia 1.** Toda tabela carrega `tenant_id`, mesmo que só haja um tenant.

## Diagrama de entidades

```
tenant ──┬── source_instance (GitLab.com / Jira site)
         │
         ├── team
         │
         ├── project ──┬── environment
         │             │
         │             ├── merge_request ──── mr_commit
         │             │
         │             └── deployment ─┬── deployment_mr_link
         │                             │
         │                             └── deployment_incident_link
         │
         └── incident
```

## Tabelas de domínio

### `tenant`

```sql
CREATE TABLE tenant (
  id              UUID PRIMARY KEY,
  slug            TEXT UNIQUE NOT NULL,
  name            TEXT NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### `source_instance`

Onde o dado vive (uma instância GitLab ou um site Atlassian).

```sql
CREATE TABLE source_instance (
  id              UUID PRIMARY KEY,
  tenant_id       UUID NOT NULL REFERENCES tenant(id),
  kind            TEXT NOT NULL CHECK (kind IN ('gitlab', 'jira')),
  base_url        TEXT NOT NULL,
  display_name    TEXT NOT NULL,
  auth_ref        TEXT NOT NULL,        -- pointer para vault (não o token)
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, kind, base_url)
);
```

### `team`

```sql
CREATE TABLE team (
  id              UUID PRIMARY KEY,
  tenant_id       UUID NOT NULL REFERENCES tenant(id),
  slug            TEXT NOT NULL,
  name            TEXT NOT NULL,
  UNIQUE (tenant_id, slug)
);
```

### `project`

Um repositório GitLab que é monitorado.

```sql
CREATE TABLE project (
  id                       UUID PRIMARY KEY,
  tenant_id                UUID NOT NULL REFERENCES tenant(id),
  team_id                  UUID REFERENCES team(id),
  source_instance_id       UUID NOT NULL REFERENCES source_instance(id),
  external_id              TEXT NOT NULL,                     -- gitlab project id
  path_with_namespace      TEXT NOT NULL,
  default_branch           TEXT NOT NULL DEFAULT 'main',
  production_env_pattern   TEXT NOT NULL DEFAULT '^prod(uction)?(-[a-z0-9-]+)?$',
  incident_jql             TEXT,                              -- JQL customizada para incidentes
  jira_project_keys        TEXT[],                            -- mapeamento para projetos Jira
  active                   BOOLEAN NOT NULL DEFAULT true,
  created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (source_instance_id, external_id)
);
```

### `environment`

Catálogo descoberto de environments por projeto (para auditar quais ambientes existem).

```sql
CREATE TABLE environment (
  id                   UUID PRIMARY KEY,
  project_id           UUID NOT NULL REFERENCES project(id),
  name                 TEXT NOT NULL,
  is_production        BOOLEAN NOT NULL,
  external_id          TEXT NOT NULL,
  first_seen_at        TIMESTAMPTZ NOT NULL,
  UNIQUE (project_id, external_id)
);
```

### `merge_request`

```sql
CREATE TABLE merge_request (
  id                    UUID PRIMARY KEY,
  project_id            UUID NOT NULL REFERENCES project(id),
  external_id           TEXT NOT NULL,                 -- gitlab MR id
  iid                   INT NOT NULL,                  -- gitlab MR iid
  title                 TEXT NOT NULL,
  author_username       TEXT,
  author_is_bot         BOOLEAN NOT NULL DEFAULT false,
  target_branch         TEXT NOT NULL,
  source_branch         TEXT,
  merged_at             TIMESTAMPTZ,
  merge_commit_sha      TEXT,
  squash_commit_sha     TEXT,
  first_commit_at       TIMESTAMPTZ,                   -- denominador do Lead Time
  first_commit_sha      TEXT,
  additions             INT,
  deletions             INT,
  labels                TEXT[],
  web_url               TEXT,
  raw_payload           JSONB,                         -- payload completo do último update
  UNIQUE (project_id, external_id)
);

CREATE INDEX ON merge_request (project_id, merged_at);
CREATE INDEX ON merge_request (project_id, target_branch, merged_at);
```

### `deployment`

```sql
CREATE TABLE deployment (
  id                    UUID PRIMARY KEY,
  project_id            UUID NOT NULL REFERENCES project(id),
  environment_id        UUID NOT NULL REFERENCES environment(id),
  external_id           TEXT NOT NULL,
  sha                   TEXT NOT NULL,
  ref                   TEXT,
  status                TEXT NOT NULL,                 -- running/success/failed/canceled
  triggered_by          TEXT,
  started_at            TIMESTAMPTZ,
  finished_at           TIMESTAMPTZ,
  is_rollback           BOOLEAN NOT NULL DEFAULT false,
  raw_payload           JSONB,
  UNIQUE (project_id, external_id)
);

CREATE INDEX ON deployment (environment_id, status, finished_at);
CREATE INDEX ON deployment (project_id, finished_at DESC);
```

### `deployment_mr_link`

Quais MRs entraram em quais deployments (calculado pela calculadora, não vem direto da API).

```sql
CREATE TABLE deployment_mr_link (
  deployment_id     UUID NOT NULL REFERENCES deployment(id),
  merge_request_id  UUID NOT NULL REFERENCES merge_request(id),
  PRIMARY KEY (deployment_id, merge_request_id)
);
```

### `incident`

Incidente de produção (vindo do Jira primariamente).

```sql
CREATE TABLE incident (
  id                    UUID PRIMARY KEY,
  tenant_id             UUID NOT NULL REFERENCES tenant(id),
  source_instance_id    UUID NOT NULL REFERENCES source_instance(id),
  external_id           TEXT NOT NULL,                 -- jira issue key, ex: PAY-1234
  jira_project_key      TEXT NOT NULL,
  summary               TEXT NOT NULL,
  status                TEXT NOT NULL,
  status_category       TEXT NOT NULL,                 -- new/indeterminate/done
  priority              TEXT,
  issuetype             TEXT,
  labels                TEXT[],
  created_at            TIMESTAMPTZ NOT NULL,          -- início do incidente
  resolved_at           TIMESTAMPTZ,                   -- fim
  raw_payload           JSONB,
  UNIQUE (source_instance_id, external_id)
);

CREATE INDEX ON incident (tenant_id, created_at);
CREATE INDEX ON incident (tenant_id, resolved_at);
```

### `deployment_incident_link`

Quais incidentes estão atribuídos a quais deployments (compõe o numerador de CFR).

```sql
CREATE TABLE deployment_incident_link (
  deployment_id     UUID NOT NULL REFERENCES deployment(id),
  incident_id       UUID NOT NULL REFERENCES incident(id),
  link_reason       TEXT NOT NULL,        -- 'time_window' | 'fix_version' | 'remote_link' | 'manual'
  linked_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (deployment_id, incident_id)
);
```

## Tabela de eventos brutos

```sql
CREATE TABLE raw_event (
  id              BIGSERIAL PRIMARY KEY,
  tenant_id       UUID NOT NULL,
  source          TEXT NOT NULL CHECK (source IN ('gitlab_webhook', 'gitlab_poll', 'jira_webhook', 'jira_mcp', 'jira_rest')),
  kind            TEXT NOT NULL,        -- 'deployment', 'merge_request', 'issue', etc
  external_id     TEXT,
  received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  payload         JSONB NOT NULL,
  processed_at    TIMESTAMPTZ,
  process_error   TEXT
);

CREATE INDEX ON raw_event (processed_at) WHERE processed_at IS NULL;
CREATE INDEX ON raw_event (source, kind, received_at);
```

A coluna `processed_at IS NULL` é um índice parcial barato que dá a fila de trabalho do worker.

## Tabelas de agregação

Pré-computadas para servir o dashboard sem cálculo em runtime.

### `metric_daily`

Granularidade diária por projeto + métrica.

```sql
CREATE TABLE metric_daily (
  tenant_id        UUID NOT NULL,
  project_id       UUID NOT NULL,
  day              DATE NOT NULL,
  deploy_count     INT NOT NULL DEFAULT 0,
  deploy_failures  INT NOT NULL DEFAULT 0,
  lead_time_p50    BIGINT,             -- segundos
  lead_time_p90    BIGINT,
  incidents_opened INT NOT NULL DEFAULT 0,
  incidents_closed INT NOT NULL DEFAULT 0,
  mttr_seconds_sum BIGINT,
  mttr_count       INT NOT NULL DEFAULT 0,
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, project_id, day)
);
```

### `metric_window`

Janelas rolantes (7d, 30d, 90d) recalculadas periodicamente. Servem direto ao dashboard.

```sql
CREATE TABLE metric_window (
  tenant_id            UUID NOT NULL,
  scope_kind           TEXT NOT NULL CHECK (scope_kind IN ('project', 'team', 'tenant')),
  scope_id             UUID NOT NULL,        -- project_id, team_id ou tenant_id
  window_days          INT NOT NULL,         -- 7 | 30 | 90
  computed_at          TIMESTAMPTZ NOT NULL,
  deployment_frequency NUMERIC,              -- deploys/dia
  lead_time_median_s   BIGINT,
  change_failure_rate  NUMERIC,              -- 0.0 a 1.0
  mttr_mean_s          BIGINT,
  classification       TEXT,                 -- 'elite' | 'high' | 'medium' | 'low'
  sample_size          INT NOT NULL,
  PRIMARY KEY (tenant_id, scope_kind, scope_id, window_days, computed_at)
);

CREATE INDEX ON metric_window (tenant_id, scope_kind, scope_id, window_days, computed_at DESC);
```

> Consultar sempre o `MAX(computed_at)` da chave — versões antigas ficam como histórico de auditoria.

### `metric_monthly_snapshot`

Snapshot imutável no fim de cada mês.

```sql
CREATE TABLE metric_monthly_snapshot (
  tenant_id            UUID NOT NULL,
  scope_kind           TEXT NOT NULL,
  scope_id             UUID NOT NULL,
  month                DATE NOT NULL,        -- sempre dia 1
  deployment_frequency NUMERIC NOT NULL,
  lead_time_median_s   BIGINT,
  change_failure_rate  NUMERIC,
  mttr_mean_s          BIGINT,
  classification       TEXT,
  PRIMARY KEY (tenant_id, scope_kind, scope_id, month)
);
```

## Cálculos canônicos

### Lead Time

```sql
WITH deploys_30d AS (
  SELECT d.id, d.finished_at
  FROM deployment d
  JOIN environment e ON e.id = d.environment_id
  WHERE d.project_id = $project
    AND e.is_production
    AND d.status = 'success'
    AND d.finished_at >= now() - INTERVAL '30 days'
)
SELECT
  PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (d.finished_at - mr.first_commit_at))) AS lead_time_median_s
FROM deploys_30d d
JOIN deployment_mr_link dml ON dml.deployment_id = d.id
JOIN merge_request mr        ON mr.id = dml.merge_request_id
WHERE NOT mr.author_is_bot;
```

### Deployment Frequency

```sql
SELECT COUNT(*) / 30.0 AS deploys_per_day
FROM deployment d
JOIN environment e ON e.id = d.environment_id
WHERE d.project_id = $project
  AND e.is_production
  AND d.status = 'success'
  AND d.finished_at >= now() - INTERVAL '30 days';
```

### Change Failure Rate

```sql
WITH deploys AS (
  SELECT d.id
  FROM deployment d
  JOIN environment e ON e.id = d.environment_id
  WHERE d.project_id = $project
    AND e.is_production
    AND d.status = 'success'
    AND d.finished_at >= now() - INTERVAL '30 days'
)
SELECT
  COUNT(*) FILTER (WHERE EXISTS (
    SELECT 1 FROM deployment_incident_link dil WHERE dil.deployment_id = deploys.id
  ))::numeric / NULLIF(COUNT(*), 0) AS cfr
FROM deploys;
```

### MTTR

```sql
SELECT AVG(EXTRACT(EPOCH FROM (resolved_at - created_at))) AS mttr_seconds
FROM incident
WHERE tenant_id = $tenant
  AND resolved_at IS NOT NULL
  AND resolved_at >= now() - INTERVAL '30 days';
```

## Classificação Elite/High/Medium/Low

Função pura aplicada sobre os 4 valores. Como os limiares variam entre relatórios, tornar **configuráveis por tenant**:

```sql
CREATE TABLE classification_threshold (
  tenant_id        UUID PRIMARY KEY REFERENCES tenant(id),
  config           JSONB NOT NULL    -- limiares por métrica e tier
);
```

A função de classificação aplica AND lógico: o time é Elite se todas as 4 métricas estiverem na faixa Elite; caso contrário cai para o pior tier individual.

## Decisão D2 — Banco

| Opção                | Prós                                                                 | Contras                                                       |
| -------------------- | -------------------------------------------------------------------- | ------------------------------------------------------------- |
| **PostgreSQL puro**  | Padrão, equipe conhece, JSONB ótimo, agregações com `tstzrange`     | Séries temporais > 1M linhas exigem cuidado                   |
| **PostgreSQL + TimescaleDB** | Particionamento automático, compressão, agregações contínuas  | Requer extensão; lock-in moderado                             |
| **ClickHouse**       | Performance brutal para agregações; ideal para 100+ projetos        | Modelo de dados diferente, deletes/updates difíceis, complica idempotência |

**Recomendação inicial:** **PostgreSQL puro**. Migrar para TimescaleDB **se** atingirmos > 50 projetos monitorados e queries de janela ficarem lentas (> 1s). ClickHouse só em escala muito grande.

## Retenção e arquivamento

- `raw_event`: 90 dias online, depois arquivar (S3/blob) compactado.
- `metric_daily`: indefinido (granularidade baixa, custa pouco).
- `metric_window`: manter apenas as últimas 10 versões por chave (histórico de recálculo).
- `metric_monthly_snapshot`: indefinido (histórico imutável).

## Migrations

Numeradas, idempotentes. Cada feature traz sua migration. Ferramenta: `flyway`, `alembic`, `prisma migrate` ou `drizzle-kit` dependendo da [stack escolhida](05-architecture.md#decis%C3%A3o-d1--stack).

## Fontes

- Doc de métricas: [01-dora-metrics.md](01-dora-metrics.md)
- Doc de arquitetura: [05-architecture.md](05-architecture.md)
