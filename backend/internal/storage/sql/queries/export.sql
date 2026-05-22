-- name: ListIncidentsInWindowForProject :many
-- Incidents do projeto cuja janela toca o intervalo: tanto os criados
-- quanto os resolvidos a partir de `since` entram (cobre "abertos no
-- período" e "fechados no período" — útil para auditoria de MTTR).
SELECT i.id,
       i.external_id,
       i.jira_project_key,
       i.summary,
       i.status,
       i.status_category,
       i.priority,
       i.issuetype,
       i.created_at,
       i.resolved_at
FROM platform.incident i
JOIN platform.project p
  ON p.tenant_id = i.tenant_id
  AND i.jira_project_key = ANY(p.jira_project_keys)
WHERE p.id = sqlc.arg(project_id)
  AND (i.created_at >= sqlc.arg(since) OR i.resolved_at >= sqlc.arg(since))
ORDER BY i.created_at DESC;

-- name: ListMergedMRsInWindowForProject :many
-- MRs do projeto mergeados a partir de `since`. Usado pelo export do
-- drill-down (lista bruta sem depender de correlação com deploy).
SELECT id,
       external_id,
       iid,
       title,
       author_username,
       target_branch,
       source_branch,
       merged_at,
       merge_commit_sha,
       first_commit_at,
       additions,
       deletions,
       web_url
FROM platform.merge_request
WHERE project_id = sqlc.arg(project_id)
  AND merged_at IS NOT NULL
  AND merged_at >= sqlc.arg(since)
  AND NOT author_is_bot
ORDER BY merged_at DESC;
