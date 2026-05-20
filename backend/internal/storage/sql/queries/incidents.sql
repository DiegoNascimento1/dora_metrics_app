-- name: UpsertIncident :one
INSERT INTO platform.incident (
  tenant_id, source_instance_id, external_id, jira_project_key,
  summary, status, status_category, priority, issuetype, labels,
  created_at, resolved_at, raw_payload
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (source_instance_id, external_id) DO UPDATE
  SET summary         = EXCLUDED.summary,
      status          = EXCLUDED.status,
      status_category = EXCLUDED.status_category,
      priority        = EXCLUDED.priority,
      issuetype       = EXCLUDED.issuetype,
      labels          = EXCLUDED.labels,
      resolved_at     = EXCLUDED.resolved_at,
      raw_payload     = EXCLUDED.raw_payload
RETURNING *;

-- name: ListIncidentsForProject :many
-- Incidents cujos jira_project_key estão no array do projeto. Usado pelo
-- linking incident ↔ deployment e pelos cálculos por janela.
SELECT i.*
FROM platform.incident i
JOIN platform.project p
  ON p.tenant_id = i.tenant_id
  AND i.jira_project_key = ANY(p.jira_project_keys)
WHERE p.id = sqlc.arg(project_id)
ORDER BY i.created_at;

-- name: MTTRMeanSecondsInWindow :one
-- Média (segundos) de (resolved_at - created_at) para incidents do projeto
-- resolvidos na janela. NULL quando não houver amostra; caller checa sample.
SELECT
  COALESCE(
    AVG(EXTRACT(EPOCH FROM (i.resolved_at - i.created_at))),
    0
  ) AS mean_seconds,
  COUNT(*) AS sample_size
FROM platform.incident i
JOIN platform.project p
  ON p.tenant_id = i.tenant_id
  AND i.jira_project_key = ANY(p.jira_project_keys)
WHERE p.id = sqlc.arg(project_id)
  AND i.resolved_at IS NOT NULL
  AND i.resolved_at >= sqlc.arg(resolved_since);
