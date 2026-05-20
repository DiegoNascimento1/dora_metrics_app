-- name: UpsertDeployment :one
INSERT INTO platform.deployment (
  project_id, environment_id, external_id, sha, ref, status,
  triggered_by, started_at, finished_at, is_rollback, raw_payload
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (project_id, external_id) DO UPDATE
  SET status = EXCLUDED.status,
      finished_at = EXCLUDED.finished_at,
      raw_payload = EXCLUDED.raw_payload
RETURNING *;

-- name: CountSuccessfulProductionDeploymentsInWindow :one
SELECT COUNT(*) AS deploy_count
FROM platform.deployment d
JOIN platform.environment e ON e.id = d.environment_id
WHERE d.project_id = $1
  AND e.is_production
  AND d.status = 'success'
  AND d.finished_at >= $2;

-- name: LeadTimeMedianSecondsInWindow :one
-- Mediana de (deploy.finished_at - mr.first_commit_at) para os MRs
-- atribuídos a deployments de produção bem-sucedidos na janela.
-- COALESCE para 0 quando sample_size = 0 (PERCENTILE_CONT retorna NULL nesse caso).
-- O caller DEVE checar sample_size > 0 antes de usar median_seconds.
SELECT
  COALESCE(
    PERCENTILE_CONT(0.5) WITHIN GROUP (
      ORDER BY EXTRACT(EPOCH FROM (d.finished_at - mr.first_commit_at))
    ),
    0
  ) AS median_seconds,
  COUNT(*) AS sample_size
FROM platform.deployment d
JOIN platform.environment e        ON e.id = d.environment_id
JOIN platform.deployment_mr_link l ON l.deployment_id = d.id
JOIN platform.merge_request mr     ON mr.id = l.merge_request_id
WHERE d.project_id = $1
  AND e.is_production
  AND d.status = 'success'
  AND d.finished_at >= $2
  AND mr.first_commit_at IS NOT NULL
  AND NOT mr.author_is_bot;
