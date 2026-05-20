-- name: UpsertEnvironment :one
INSERT INTO platform.environment (project_id, name, is_production, external_id, first_seen_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (project_id, external_id) DO UPDATE
  SET name = EXCLUDED.name,
      is_production = EXCLUDED.is_production
RETURNING *;

-- name: ListEnvironmentsByProject :many
SELECT * FROM platform.environment WHERE project_id = $1 ORDER BY name;

-- name: GetProductionEnvironmentIDs :many
SELECT id FROM platform.environment
WHERE project_id = $1 AND is_production;
