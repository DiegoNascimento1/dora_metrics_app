-- name: DaysSinceLastIncidentForProject :one
-- Dias completos desde o último incident production-impactante do projeto
-- (via mapping jira_project_keys). Retorna -1 quando o projeto nunca teve
-- incident registrado (ou não tem jira_project_keys configurado).
-- O caller trata -1 como "sem incidents — streak infinito ainda não é mérito".
SELECT
  COALESCE(
    FLOOR(EXTRACT(EPOCH FROM (now() - MAX(i.created_at))) / 86400)::int,
    -1
  ) AS days_since
FROM platform.incident i
JOIN platform.project p
  ON p.tenant_id = i.tenant_id
  AND i.jira_project_key = ANY(p.jira_project_keys)
WHERE p.id = sqlc.arg(project_id);
