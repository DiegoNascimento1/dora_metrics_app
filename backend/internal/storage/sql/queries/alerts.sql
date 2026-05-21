-- name: CreateAlertRule :one
INSERT INTO platform.alert_rule (
  tenant_id, name, enabled, kind, scope_kind, scope_id, window_days, webhook_url
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: GetAlertRule :one
SELECT * FROM platform.alert_rule WHERE id = $1;

-- name: ListAlertRulesByTenant :many
SELECT * FROM platform.alert_rule
WHERE tenant_id = $1
ORDER BY created_at DESC;

-- name: UpdateAlertRule :one
UPDATE platform.alert_rule
SET name        = COALESCE(sqlc.narg(name), name),
    enabled     = COALESCE(sqlc.narg(enabled), enabled),
    kind        = COALESCE(sqlc.narg(kind), kind),
    scope_kind  = COALESCE(sqlc.narg(scope_kind), scope_kind),
    scope_id    = COALESCE(sqlc.narg(scope_id), scope_id),
    window_days = COALESCE(sqlc.narg(window_days), window_days),
    webhook_url = COALESCE(sqlc.narg(webhook_url), webhook_url),
    updated_at  = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteAlertRule :exec
DELETE FROM platform.alert_rule WHERE id = $1;

-- name: FindMatchingAlertRules :many
-- Localiza regras habilitadas que cobrem um scope+window específico.
-- scope_id NULL na regra = matches all scopes do tipo dentro do tenant.
SELECT * FROM platform.alert_rule
WHERE tenant_id = $1
  AND enabled = TRUE
  AND scope_kind = $2
  AND window_days = $3
  AND (scope_id IS NULL OR scope_id = $4);

-- name: GetAlertEvent :one
SELECT * FROM platform.alert_event WHERE id = $1;

-- name: InsertAlertEvent :one
INSERT INTO platform.alert_event (
  rule_id, tenant_id, scope_kind, scope_id,
  previous_tier, current_tier, payload
) VALUES (
  $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: MarkAlertEventDelivered :exec
UPDATE platform.alert_event
SET delivery_status = 'delivered',
    http_status     = $2,
    delivered_at    = now(),
    last_error      = NULL
WHERE id = $1;

-- name: MarkAlertEventFailed :exec
UPDATE platform.alert_event
SET delivery_status = 'failed',
    http_status     = $2,
    last_error      = $3
WHERE id = $1;

-- name: ListRecentAlertEvents :many
SELECT * FROM platform.alert_event
WHERE tenant_id = $1
ORDER BY fired_at DESC
LIMIT $2;

-- name: ListAlertEventsByRule :many
SELECT * FROM platform.alert_event
WHERE rule_id = $1
ORDER BY fired_at DESC
LIMIT $2;
