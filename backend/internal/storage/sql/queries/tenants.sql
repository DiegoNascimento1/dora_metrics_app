-- name: CreateTenant :one
INSERT INTO platform.tenant (slug, name)
VALUES ($1, $2)
RETURNING *;

-- name: GetTenantBySlug :one
SELECT * FROM platform.tenant WHERE slug = $1;

-- name: GetTenantByID :one
SELECT * FROM platform.tenant WHERE id = $1;

-- name: ListTenants :many
SELECT * FROM platform.tenant ORDER BY created_at;
