-- name: UpsertMonthlySnapshot :one
INSERT INTO metrics.metric_monthly_snapshot (
  tenant_id, scope_kind, scope_id, month,
  deployment_frequency, lead_time_median_s, change_failure_rate, mttr_mean_s,
  classification
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (tenant_id, scope_kind, scope_id, month) DO UPDATE
  SET deployment_frequency = EXCLUDED.deployment_frequency,
      lead_time_median_s   = EXCLUDED.lead_time_median_s,
      change_failure_rate  = EXCLUDED.change_failure_rate,
      mttr_mean_s          = EXCLUDED.mttr_mean_s,
      classification       = EXCLUDED.classification
RETURNING *;

-- name: CountEliteMonthsForScope :one
-- Quantos meses na história desse scope foram classificados Elite.
-- Drive da achievement "First Elite Month" (count >= 1).
SELECT COUNT(*)::bigint AS elite_months
FROM metrics.metric_monthly_snapshot
WHERE tenant_id = sqlc.arg(tenant_id)
  AND scope_kind = sqlc.arg(scope_kind)
  AND scope_id   = sqlc.arg(scope_id)
  AND classification = 'elite';

-- name: GetLastIncidentsMTTRForProject :many
-- Últimos 5 incidents resolvidos do projeto (via jira_project_keys),
-- com o MTTR em segundos. Drive da achievement "Recovery Master".
SELECT
  i.id,
  i.resolved_at,
  EXTRACT(EPOCH FROM (i.resolved_at - i.created_at))::bigint AS mttr_seconds
FROM platform.incident i
JOIN platform.project p
  ON p.tenant_id = i.tenant_id
  AND i.jira_project_key = ANY(p.jira_project_keys)
WHERE p.id = sqlc.arg(project_id)
  AND i.resolved_at IS NOT NULL
ORDER BY i.resolved_at DESC
LIMIT sqlc.arg(limit_n);
