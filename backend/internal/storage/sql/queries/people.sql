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

-- name: PropagatePersonToMergeRequests :execrows
-- Atualiza merge_request.author_person_id para todos os MRs cujo
-- author_username casa (case-insensitive) com alguma identity já linkada
-- à pessoa, no mesmo tenant. Idempotente.
WITH matches AS (
  SELECT mr.id AS mr_id, pi.person_id
  FROM platform.merge_request mr
  JOIN platform.project p ON p.id = mr.project_id
  JOIN platform.person_identity pi
    ON pi.tenant_id = p.tenant_id
   AND pi.kind = 'gitlab'
   AND pi.person_id IS NOT NULL
   AND lower(pi.external_username) = lower(mr.author_username)
)
UPDATE platform.merge_request mr
SET author_person_id = matches.person_id
FROM matches
WHERE mr.id = matches.mr_id
  AND (mr.author_person_id IS NULL OR mr.author_person_id <> matches.person_id);

-- name: PropagatePersonToDeployments :execrows
WITH matches AS (
  SELECT d.id AS d_id, pi.person_id
  FROM platform.deployment d
  JOIN platform.project p ON p.id = d.project_id
  JOIN platform.person_identity pi
    ON pi.tenant_id = p.tenant_id
   AND pi.kind = 'gitlab'
   AND pi.person_id IS NOT NULL
   AND lower(pi.external_username) = lower(d.triggered_by)
)
UPDATE platform.deployment d
SET triggerer_person_id = matches.person_id
FROM matches
WHERE d.id = matches.d_id
  AND (d.triggerer_person_id IS NULL OR d.triggerer_person_id <> matches.person_id);

-- name: CountDeploymentsByPersonInWindow :one
SELECT COUNT(*)::bigint AS deploy_count
FROM platform.deployment d
JOIN platform.environment e ON e.id = d.environment_id
WHERE d.triggerer_person_id = sqlc.arg(person_id)
  AND e.is_production
  AND d.status = 'success'
  AND d.finished_at >= sqlc.arg(finished_since);

-- name: LeadTimeMedianByPersonInWindow :one
-- Lead Time mediano dos MRs autorados pela pessoa cujos deployments de
-- produção bem-sucedidos vinculados caem na janela.
SELECT
  COALESCE(
    PERCENTILE_CONT(0.5) WITHIN GROUP (
      ORDER BY EXTRACT(EPOCH FROM (d.finished_at - mr.first_commit_at))
    ),
    0
  ) AS median_seconds,
  COUNT(*) AS sample_size
FROM platform.deployment d
JOIN platform.environment e        ON e.id = d.environment_id
JOIN platform.deployment_mr_link l ON l.deployment_id = d.id
JOIN platform.merge_request mr     ON mr.id = l.merge_request_id
WHERE mr.author_person_id = sqlc.arg(person_id)
  AND e.is_production
  AND d.status = 'success'
  AND d.finished_at >= sqlc.arg(finished_since)
  AND mr.first_commit_at IS NOT NULL;

-- name: CountIncidentsLinkedToPersonInWindow :one
-- Quantos incidents foram causados por deploys que ESSA pessoa disparou,
-- na janela. Aproximação per-person de CFR (denominador real é seus deploys).
SELECT COUNT(DISTINCT i.id)::bigint AS incident_count
FROM platform.incident i
JOIN platform.deployment_incident_link dil ON dil.incident_id = i.id
JOIN platform.deployment d                 ON d.id = dil.deployment_id
JOIN platform.environment e                ON e.id = d.environment_id
WHERE d.triggerer_person_id = sqlc.arg(person_id)
  AND e.is_production
  AND d.finished_at >= sqlc.arg(finished_since);

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
