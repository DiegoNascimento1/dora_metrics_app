-- name: DeploymentsPerDayInWindow :many
-- Série temporal de deployments de produção bem-sucedidos por dia (UTC).
-- Drive da curva de Deployment Frequency.
SELECT
  DATE_TRUNC('day', d.finished_at AT TIME ZONE 'UTC')::date AS day,
  COUNT(*)::int AS deploy_count
FROM platform.deployment d
JOIN platform.environment e ON e.id = d.environment_id
WHERE d.project_id = sqlc.arg(project_id)
  AND e.is_production
  AND d.status = 'success'
  AND d.finished_at >= sqlc.arg(finished_since)
GROUP BY 1
ORDER BY 1;

-- name: ListProductionDeploymentsInWindow :many
-- Lista bruta dos deploys de produção na janela. Drive do drill-down
-- "clique no tile/ponto → ver os deploys que compõem".
SELECT
  d.id,
  d.sha,
  d.ref,
  d.status,
  d.triggered_by,
  d.started_at,
  d.finished_at,
  e.name AS environment_name
FROM platform.deployment d
JOIN platform.environment e ON e.id = d.environment_id
WHERE d.project_id = sqlc.arg(project_id)
  AND e.is_production
  AND d.status = 'success'
  AND d.finished_at >= sqlc.arg(finished_since)
ORDER BY d.finished_at DESC;
