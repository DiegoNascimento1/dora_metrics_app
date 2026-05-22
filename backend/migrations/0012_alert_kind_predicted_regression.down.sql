-- Reverte para a constraint original (sem predicted_regression).
-- Atenção: registros com kind='predicted_regression' precisam ser
-- removidos manualmente antes de aplicar essa down — caso contrário a
-- constraint vai falhar. Em ambiente real, nunca rodar down sem snapshot.

ALTER TABLE platform.alert_rule
    DROP CONSTRAINT IF EXISTS alert_rule_kind_check;

ALTER TABLE platform.alert_rule
    ADD CONSTRAINT alert_rule_kind_check
    CHECK (kind IN ('tier_regression', 'tier_change'));
