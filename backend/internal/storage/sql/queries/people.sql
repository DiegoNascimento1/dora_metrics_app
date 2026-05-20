-- name: CreatePerson :one
INSERT INTO platform.person (tenant_id, display_name, primary_email, avatar_url)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetPerson :one
SELECT * FROM platform.person WHERE id = $1;

-- name: ListPeople :many
SELECT * FROM platform.person
WHERE tenant_id = sqlc.arg(tenant_id)
ORDER BY display_name;

-- name: UpsertPersonIdentity :one
-- Idempotente em (tenant_id, kind, external_username). Atualiza external_email
-- e external_id no conflito; preserva person_id já vinculado.
INSERT INTO platform.person_identity (
  tenant_id, source_instance_id, kind,
  external_id, external_username, external_email
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (tenant_id, kind, external_username) DO UPDATE
  SET external_email = COALESCE(EXCLUDED.external_email, platform.person_identity.external_email),
      external_id    = COALESCE(EXCLUDED.external_id, platform.person_identity.external_id),
      source_instance_id = COALESCE(EXCLUDED.source_instance_id, platform.person_identity.source_instance_id)
RETURNING *;

-- name: LinkIdentityToPerson :one
UPDATE platform.person_identity
SET person_id = sqlc.arg(person_id),
    linked_at = now(),
    linked_by = sqlc.narg(linked_by)
WHERE id = sqlc.arg(identity_id)
RETURNING *;

-- name: UnlinkIdentity :exec
UPDATE platform.person_identity
SET person_id = NULL,
    linked_at = NULL,
    linked_by = NULL
WHERE id = $1;

-- name: ListUnlinkedIdentities :many
SELECT * FROM platform.person_identity
WHERE tenant_id = sqlc.arg(tenant_id)
  AND person_id IS NULL
ORDER BY kind, external_username;

-- name: ListIdentitiesByPerson :many
SELECT * FROM platform.person_identity
WHERE person_id = $1
ORDER BY kind, external_username;

-- name: FindPersonByEmail :one
-- Auto-match: dado um email, devolve a person que casa.
SELECT * FROM platform.person
WHERE tenant_id = sqlc.arg(tenant_id)
  AND lower(primary_email) = lower(sqlc.arg(email)::text)
LIMIT 1;

-- name: FindIdentitiesByEmail :many
-- Auto-match heurístico: identidades com mesmo email (ignorando case).
SELECT * FROM platform.person_identity
WHERE tenant_id = sqlc.arg(tenant_id)
  AND external_email IS NOT NULL
  AND lower(external_email) = lower(sqlc.arg(email)::text);

-- name: FindIdentitiesByUsername :many
-- Auto-match heurístico: identidades com mesmo username (ignorando case),
-- entre diferentes kinds. Usado para sugerir merges de "alice" do GitLab com
-- "alice" do Jira quando emails não estão disponíveis.
SELECT * FROM platform.person_identity
WHERE tenant_id = sqlc.arg(tenant_id)
  AND lower(external_username) = lower(sqlc.arg(username)::text);

-- name: ListGitlabUsernamesFromEvents :many
-- Backfill: usernames únicos que já apareceram em merge_request.author_username
-- ou deployment.triggered_by, restrito ao tenant via JOIN com project.
SELECT DISTINCT lower(username) AS username
FROM (
  SELECT mr.author_username AS username
  FROM platform.merge_request mr
  JOIN platform.project p ON p.id = mr.project_id
  WHERE p.tenant_id = sqlc.arg(tenant_id)
    AND mr.author_username IS NOT NULL
    AND mr.author_username <> ''
    AND NOT mr.author_is_bot
  UNION
  SELECT d.triggered_by AS username
  FROM platform.deployment d
  JOIN platform.project p ON p.id = d.project_id
  WHERE p.tenant_id = sqlc.arg(tenant_id)
    AND d.triggered_by IS NOT NULL
    AND d.triggered_by <> ''
) AS sources
WHERE username IS NOT NULL AND username <> ''
ORDER BY username;
