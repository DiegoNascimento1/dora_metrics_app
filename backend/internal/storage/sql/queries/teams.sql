-- name: CreateTeam :one
INSERT INTO platform.team (tenant_id, slug, name, color, emoji)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetTeam :one
SELECT * FROM platform.team WHERE id = $1;

-- name: ListTeamsByTenant :many
SELECT * FROM platform.team
WHERE tenant_id = $1
ORDER BY created_at;

-- name: UpdateTeam :one
UPDATE platform.team
SET name  = COALESCE(sqlc.narg(name), name),
    color = COALESCE(sqlc.narg(color), color),
    emoji = COALESCE(sqlc.narg(emoji), emoji)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteTeam :exec
DELETE FROM platform.team WHERE id = $1;

-- name: AssignProjectToTeam :one
UPDATE platform.project
SET team_id = sqlc.narg(team_id)
WHERE id = sqlc.arg(project_id)
RETURNING *;

-- name: ListProjectsByTeam :many
SELECT * FROM platform.project
WHERE team_id = $1
ORDER BY path_with_namespace;
