-- name: InsertRawEvent :one
INSERT INTO raw.raw_event (tenant_id, source, kind, external_id, payload)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListUnprocessedRawEvents :many
-- Fila do processamento incremental — usa o índice parcial
-- raw_event_pending_idx (processed_at IS NULL).
SELECT * FROM raw.raw_event
WHERE processed_at IS NULL
ORDER BY id
LIMIT sqlc.arg(batch_size);

-- name: MarkRawEventProcessed :exec
UPDATE raw.raw_event
SET processed_at = now(),
    process_error = sqlc.narg(process_error)
WHERE id = sqlc.arg(id);
