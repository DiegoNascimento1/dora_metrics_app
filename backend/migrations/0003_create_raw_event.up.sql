-- Tabela append-only de eventos brutos vindos de webhooks/polling.
-- Atua como fila de processamento via índice parcial em processed_at.

CREATE TABLE raw.raw_event (
  id              BIGSERIAL PRIMARY KEY,
  tenant_id       UUID NOT NULL,
  source          TEXT NOT NULL CHECK (source IN (
    'gitlab_webhook', 'gitlab_poll',
    'jira_webhook', 'jira_mcp', 'jira_rest'
  )),
  kind            TEXT NOT NULL,
  external_id     TEXT,
  received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  payload         JSONB NOT NULL,
  processed_at    TIMESTAMPTZ,
  process_error   TEXT
);

CREATE INDEX raw_event_pending_idx
  ON raw.raw_event (id)
  WHERE processed_at IS NULL;

CREATE INDEX raw_event_source_kind_received_idx
  ON raw.raw_event (source, kind, received_at);

CREATE INDEX raw_event_tenant_received_idx
  ON raw.raw_event (tenant_id, received_at);
