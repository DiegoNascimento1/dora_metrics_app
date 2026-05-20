-- name: CreateProject :one
INSERT INTO platform.project (
  tenant_id, team_id, source_instance_id, external_id, path_with_namespace,
  default_branch, production_env_pattern, incident_jql, jira_project_keys
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetProject :one
SELECT * FROM platform.project WHERE id = $1;

-- name: GetProjectByExternalID :one
SELECT * FROM platform.project
WHERE source_instance_id = $1 AND external_id = $2;

-- name: ListProjects :many
SELECT * FROM platform.project ORDER BY path_with_namespace;

-- name: ListActiveProjects :many
SELECT * FROM platform.project WHERE active ORDER BY last_synced_at NULLS FIRST;

-- name: GetGitLabProjectByExternalID :one
-- Encontra o projeto pelo external_id assumindo source kind='gitlab'.
-- Em multi-tenant com múltiplos GitLab e overlap de IDs, retorna o mais antigo.
SELECT p.*
FROM platform.project p
JOIN platform.source_instance si ON si.id = p.source_instance_id
WHERE si.kind = 'gitlab'
  AND p.external_id = sqlc.arg(external_id)
ORDER BY p.created_at
LIMIT 1;

-- name: ListActiveProjectsByJiraProjectKey :many
-- Projects cujos jira_project_keys contêm a chave passada (case-sensitive).
-- Usado por webhook Jira para identificar quais nossos projetos refrescar.
SELECT * FROM platform.project
WHERE active
  AND sqlc.arg(jira_project_key)::text = ANY(jira_project_keys);

-- name: UpdateProjectLastSynced :exec
UPDATE platform.project SET last_synced_at = $2 WHERE id = $1;
