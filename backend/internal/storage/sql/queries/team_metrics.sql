-- Agregações DORA por time. Todas as queries usam a mesma estratégia:
-- expande os projetos do time via subselect em platform.project.team_id e
-- aplica a mesma lógica da versão "by project".
--
-- Convenção: COALESCE em saídas opcionais (PERCENTILE_CONT/AVG/divisão por 0)
-- pra sqlc gerar tipo concreto. Caller decide via sample_size se o valor
-- tem significado.

-- name: CountSuccessfulProductionDeploymentsForTeamInWindow :one
SELECT COUNT(*)::bigint AS deploy_count
FROM platform.deployment d
JOIN platform.environment e ON e.id = d.environment_id
JOIN platform.project p     ON p.id = d.project_id
WHERE p.team_id = sqlc.arg(team_id)
  AND e.is_production
  AND d.status = 'success'
  AND d.finished_at >= sqlc.arg(finished_since);

-- name: LeadTimeMedianForTeamInWindow :one
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
JOIN platform.project p            ON p.id = d.project_id
JOIN platform.deployment_mr_link l ON l.deployment_id = d.id
JOIN platform.merge_request mr     ON mr.id = l.merge_request_id
WHERE p.team_id = sqlc.arg(team_id)
  AND e.is_production
  AND d.status = 'success'
  AND d.finished_at >= sqlc.arg(finished_since)
  AND mr.first_commit_at IS NOT NULL
  AND NOT mr.author_is_bot;

-- name: ChangeFailureRateForTeamInWindow :one
SELECT
  COALESCE(
    CAST(SUM(CASE WHEN linked_count > 0 THEN 1 ELSE 0 END) AS numeric)
      / NULLIF(COUNT(*), 0),
    0
  ) AS cfr,
  COUNT(*) AS sample_size
FROM (
  SELECT d.id, COUNT(dil.incident_id) AS linked_count
  FROM platform.deployment d
  JOIN platform.environment e ON e.id = d.environment_id
  JOIN platform.project p     ON p.id = d.project_id
  LEFT JOIN platform.deployment_incident_link dil ON dil.deployment_id = d.id
  WHERE p.team_id = sqlc.arg(team_id)
    AND e.is_production
    AND d.status = 'success'
    AND d.finished_at >= sqlc.arg(finished_since)
  GROUP BY d.id
) AS by_deploy;

-- name: MTTRMeanSecondsForTeamInWindow :one
SELECT
  COALESCE(
    AVG(EXTRACT(EPOCH FROM (i.resolved_at - i.created_at))),
    0
  ) AS mean_seconds,
  COUNT(*) AS sample_size
FROM platform.incident i
JOIN platform.project p
  ON p.tenant_id = i.tenant_id
  AND i.jira_project_key = ANY(p.jira_project_keys)
WHERE p.team_id = sqlc.arg(team_id)
  AND i.resolved_at IS NOT NULL
  AND i.resolved_at >= sqlc.arg(resolved_since);

-- name: DeploymentsPerDayForTeamInWindow :many
SELECT
  DATE_TRUNC('day', d.finished_at AT TIME ZONE 'UTC')::date AS day,
  COUNT(*)::int AS deploy_count
FROM platform.deployment d
JOIN platform.environment e ON e.id = d.environment_id
JOIN platform.project p     ON p.id = d.project_id
WHERE p.team_id = sqlc.arg(team_id)
  AND e.is_production
  AND d.status = 'success'
  AND d.finished_at >= sqlc.arg(finished_since)
GROUP BY 1
ORDER BY 1;
