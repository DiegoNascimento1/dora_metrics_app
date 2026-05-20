-- name: CreateSourceInstance :one
INSERT INTO platform.source_instance (tenant_id, kind, base_url, display_name, auth_ref)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSourceInstance :one
SELECT * FROM platform.source_instance WHERE id = $1;

-- name: ListSourceInstancesByTenant :many
SELECT * FROM platform.source_instance
WHERE tenant_id = $1
ORDER BY created_at;
