-- Adiciona "predicted_regression" como kind válido de alert_rule.
-- Dispara quando a task predict:weekly detecta tendência degrading
-- com confidence >= medium pelo regression linear do tier rank.
--
-- Em contraste com tier_regression (reativo — só dispara depois que a
-- métrica caiu), predicted_regression é proativo: avisa N dias antes
-- da degradação se a tendência continuar.

ALTER TABLE platform.alert_rule
    DROP CONSTRAINT IF EXISTS alert_rule_kind_check;

ALTER TABLE platform.alert_rule
    ADD CONSTRAINT alert_rule_kind_check
    CHECK (kind IN ('tier_regression', 'tier_change', 'predicted_regression'));

COMMENT ON COLUMN platform.alert_rule.kind IS
    'tier_regression (reativo: tier caiu), tier_change (qualquer mudança), predicted_regression (proativo: regressão linear projeta degradação)';
