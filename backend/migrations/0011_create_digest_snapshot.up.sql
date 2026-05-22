-- Weekly digest — snapshot semanal congelado com contagens + tier delta.
-- PK por (tenant, scope, iso_week) garante idempotência: re-rodar a task
-- mesmo no mesmo dia sobrescreve o registro do mesmo isoweek.

CREATE TABLE platform.digest_snapshot (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES platform.tenant(id) ON DELETE CASCADE,
    scope_kind          TEXT NOT NULL CHECK (scope_kind IN ('project', 'team')),
    scope_id            UUID NOT NULL,
    iso_week            TEXT NOT NULL,           -- e.g. "2026-W21"
    week_start          DATE NOT NULL,           -- segunda-feira da semana
    week_end            DATE NOT NULL,
    deployments_count   INTEGER NOT NULL DEFAULT 0,
    incidents_count     INTEGER NOT NULL DEFAULT 0,
    current_tier        TEXT,                    -- tier da janela 30d ao fim da semana
    previous_tier       TEXT,                    -- tier 30d ao fim da semana anterior
    tier_delta          INTEGER NOT NULL DEFAULT 0, -- +1 = subiu, -1 = caiu, 0 = igual
    top_contributors    JSONB NOT NULL DEFAULT '[]'::jsonb, -- [{person_id, name, deploys}]
    computed_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, scope_kind, scope_id, iso_week)
);

CREATE INDEX idx_digest_snapshot_scope
    ON platform.digest_snapshot (tenant_id, scope_kind, scope_id, iso_week DESC);

COMMENT ON TABLE platform.digest_snapshot IS
    'Snapshot semanal calculado pela task digest:weekly (segunda 09:00 UTC). Idempotente por iso_week.';
