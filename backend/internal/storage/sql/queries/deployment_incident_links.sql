-- name: UpsertDeploymentIncidentLink :exec
INSERT INTO platform.deployment_incident_link (deployment_id, incident_id, link_reason)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: FindDeploymentForIncident :one
-- Acha o deployment de produção bem-sucedido mais recente do projeto cujo
-- finished_at está em (incident.created_at - lookback, incident.created_at].
-- Usado pelo time-window linking de CFR (default lookback = 24h).
SELECT d.id
FROM platform.deployment d
JOIN platform.environment e ON e.id = d.environment_id
WHERE d.project_id = sqlc.arg(project_id)
  AND e.is_production
  AND d.status = 'success'
  AND d.finished_at IS NOT NULL
  AND d.finished_at <= sqlc.arg(incident_created_at)
  AND d.finished_at >= sqlc.arg(lookback_floor)
ORDER BY d.finished_at DESC
LIMIT 1;
