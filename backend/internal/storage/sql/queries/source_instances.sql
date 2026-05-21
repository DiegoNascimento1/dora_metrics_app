-- name: CreateSourceInstance :one
INSERT INTO platform.source_instance (
  tenant_id, kind, base_url, display_name, auth_ref, secret_value, auth_email
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetSourceInstance :one
SELECT * FROM platform.source_instance WHERE id = $1;

-- name: ListSourceInstancesByTenant :many
SELECT * FROM platform.source_instance
WHERE tenant_id = $1
ORDER BY created_at;

-- name: GetFirstSourceInstanceForTenantKind :one
SELECT * FROM platform.source_instance
WHERE tenant_id = sqlc.arg(tenant_id)
  AND kind = sqlc.arg(kind)
ORDER BY created_at
LIMIT 1;

-- name: UpdateSourceInstanceSecret :one
UPDATE platform.source_instance
SET secret_value = sqlc.narg(secret_value),
    auth_email   = sqlc.narg(auth_email),
    auth_ref     = COALESCE(sqlc.narg(auth_ref), auth_ref)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteSourceInstance :exec
DELETE FROM platform.source_instance WHERE id = $1;
