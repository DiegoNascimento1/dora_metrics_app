-- name: GetClassificationThreshold :one
SELECT * FROM platform.classification_threshold WHERE tenant_id = $1;

-- name: UpsertClassificationThreshold :one
INSERT INTO platform.classification_threshold (tenant_id, config)
VALUES ($1, $2)
ON CONFLICT (tenant_id) DO UPDATE
  SET config = EXCLUDED.config
RETURNING *;
