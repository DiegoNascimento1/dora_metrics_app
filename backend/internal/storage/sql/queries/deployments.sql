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
