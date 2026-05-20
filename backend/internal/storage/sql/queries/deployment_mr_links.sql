-- name: UpsertDeploymentMRLink :exec
INSERT INTO platform.deployment_mr_link (deployment_id, merge_request_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListProductionDeploymentsForProject :many
-- Deployments de produção do projeto ordenados por finished_at ASC.
-- Usado pela correlação MR ↔ deployment.
SELECT d.id, d.finished_at, d.environment_id, d.sha
FROM platform.deployment d
JOIN platform.environment e ON e.id = d.environment_id
WHERE d.project_id = $1
  AND e.is_production
  AND d.status = 'success'
  AND d.finished_at IS NOT NULL
ORDER BY d.finished_at ASC;
