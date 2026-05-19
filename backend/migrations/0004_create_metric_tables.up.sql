-- Agregações pré-computadas.
-- Detalhes em ../../docs/06-data-model.md

CREATE TABLE metrics.metric_daily (
  tenant_id        UUID NOT NULL,
  project_id       UUID NOT NULL,
  day              DATE NOT NULL,
  deploy_count     INT NOT NULL DEFAULT 0,
  deploy_failures  INT NOT NULL DEFAULT 0,
  lead_time_p50    BIGINT,
  lead_time_p90    BIGINT,
  incidents_opened INT NOT NULL DEFAULT 0,
  incidents_closed INT NOT NULL DEFAULT 0,
  mttr_seconds_sum BIGINT,
  mttr_count       INT NOT NULL DEFAULT 0,
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, project_id, day)
);

CREATE TABLE metrics.metric_window (
  tenant_id            UUID NOT NULL,
  scope_kind           TEXT NOT NULL CHECK (scope_kind IN ('project', 'team', 'tenant')),
  scope_id             UUID NOT NULL,
  window_days          INT NOT NULL CHECK (window_days IN (7, 30, 90)),
  computed_at          TIMESTAMPTZ NOT NULL,
  deployment_frequency NUMERIC,
  lead_time_median_s   BIGINT,
  change_failure_rate  NUMERIC,
  mttr_mean_s          BIGINT,
  classification       TEXT,
  sample_size          INT NOT NULL DEFAULT 0,
  PRIMARY KEY (tenant_id, scope_kind, scope_id, window_days, computed_at)
);

CREATE INDEX metric_window_lookup_idx
  ON metrics.metric_window (tenant_id, scope_kind, scope_id, window_days, computed_at DESC);

CREATE TABLE metrics.metric_monthly_snapshot (
  tenant_id            UUID NOT NULL,
  scope_kind           TEXT NOT NULL,
  scope_id             UUID NOT NULL,
  month                DATE NOT NULL,
  deployment_frequency NUMERIC NOT NULL,
  lead_time_median_s   BIGINT,
  change_failure_rate  NUMERIC,
  mttr_mean_s          BIGINT,
  classification       TEXT,
  PRIMARY KEY (tenant_id, scope_kind, scope_id, month)
);
