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

-- name: UpdateProjectLastSynced :exec
UPDATE platform.project SET last_synced_at = $2 WHERE id = $1;
