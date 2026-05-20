-- name: UpsertMetricWindow :one
-- Insere uma nova versão da janela (não substitui histórico).
INSERT INTO metrics.metric_window (
  tenant_id, scope_kind, scope_id, window_days, computed_at,
  deployment_frequency, lead_time_median_s, change_failure_rate, mttr_mean_s,
  classification, sample_size
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetLatestMetricWindow :one
SELECT * FROM metrics.metric_window
WHERE tenant_id = $1
  AND scope_kind = $2
  AND scope_id = $3
  AND window_days = $4
ORDER BY computed_at DESC
LIMIT 1;
