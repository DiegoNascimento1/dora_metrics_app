-- Alert engine (Fase 4): regras de alerta + histórico de eventos disparados.
--
-- Modelo intencionalmente enxuto pro MVP:
--   * Uma regra observa mudanças na classificação combinada (metric_window)
--     de um escopo (project | team | tenant).
--   * Tipos suportados:
--       - tier_regression : dispara quando classificação atual < anterior
--                           (elite -> high, high -> medium, etc).
--       - tier_change     : dispara em qualquer mudança de tier (regressão
--                           ou melhoria) — útil pra celebrar promoções.
--   * Entrega via webhook HTTP (Slack-compatible payload por default).
--
-- Documentação: ../../docs/07-roadmap.md § Fase 4 § Engine de alertas.

CREATE TABLE platform.alert_rule (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    UUID NOT NULL REFERENCES platform.tenant(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  enabled      BOOLEAN NOT NULL DEFAULT TRUE,
  kind         TEXT NOT NULL CHECK (kind IN ('tier_regression', 'tier_change')),
  scope_kind   TEXT NOT NULL CHECK (scope_kind IN ('project', 'team', 'tenant')),
  -- scope_id NULL = aplica a todos os escopos do tipo dentro do tenant.
  scope_id     UUID,
  window_days  INT NOT NULL DEFAULT 30 CHECK (window_days IN (7, 30, 90)),
  webhook_url  TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX alert_rule_tenant_idx
  ON platform.alert_rule (tenant_id)
  WHERE enabled = TRUE;

CREATE INDEX alert_rule_scope_idx
  ON platform.alert_rule (tenant_id, scope_kind, scope_id, window_days)
  WHERE enabled = TRUE;

CREATE TABLE platform.alert_event (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  rule_id         UUID NOT NULL REFERENCES platform.alert_rule(id) ON DELETE CASCADE,
  tenant_id       UUID NOT NULL REFERENCES platform.tenant(id) ON DELETE CASCADE,
  fired_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  scope_kind      TEXT NOT NULL,
  scope_id        UUID NOT NULL,
  previous_tier   TEXT,
  current_tier    TEXT NOT NULL,
  -- Payload congelado no momento do dispatch — preserva contexto mesmo se a
  -- regra/threshold mudar depois.
  payload         JSONB NOT NULL,
  delivery_status TEXT NOT NULL DEFAULT 'pending'
    CHECK (delivery_status IN ('pending', 'delivered', 'failed')),
  http_status     INT,
  last_error      TEXT,
  delivered_at    TIMESTAMPTZ
);

CREATE INDEX alert_event_rule_idx
  ON platform.alert_event (rule_id, fired_at DESC);

CREATE INDEX alert_event_tenant_idx
  ON platform.alert_event (tenant_id, fired_at DESC);

CREATE INDEX alert_event_pending_idx
  ON platform.alert_event (delivery_status, fired_at)
  WHERE delivery_status = 'pending';

COMMENT ON TABLE platform.alert_rule IS
  'Regras de alerta sobre mudança de classificação DORA. Disparam por scope+window_days.';

COMMENT ON COLUMN platform.alert_rule.scope_id IS
  'NULL = aplica a todos os escopos do scope_kind dentro do tenant.';

COMMENT ON TABLE platform.alert_event IS
  'Histórico imutável de alertas disparados. Status de entrega rastreado pra retry/debug.';
