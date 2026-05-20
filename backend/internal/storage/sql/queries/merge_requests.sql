-- name: UpsertMergeRequest :one
INSERT INTO platform.merge_request (
  project_id, external_id, iid, title, author_username, author_is_bot,
  target_branch, source_branch, merged_at, merge_commit_sha, squash_commit_sha,
  first_commit_at, first_commit_sha, additions, deletions, labels, web_url, raw_payload
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
ON CONFLICT (project_id, external_id) DO UPDATE
  SET title             = EXCLUDED.title,
      author_username   = EXCLUDED.author_username,
      author_is_bot     = EXCLUDED.author_is_bot,
      target_branch     = EXCLUDED.target_branch,
      source_branch     = EXCLUDED.source_branch,
      merged_at         = EXCLUDED.merged_at,
      merge_commit_sha  = EXCLUDED.merge_commit_sha,
      squash_commit_sha = EXCLUDED.squash_commit_sha,
      first_commit_at   = EXCLUDED.first_commit_at,
      first_commit_sha  = EXCLUDED.first_commit_sha,
      additions         = EXCLUDED.additions,
      deletions         = EXCLUDED.deletions,
      labels            = EXCLUDED.labels,
      web_url           = EXCLUDED.web_url,
      raw_payload       = EXCLUDED.raw_payload
RETURNING *;

-- name: ListMergedMRsBetween :many
-- MRs do projeto cujo merged_at cai no intervalo (gt, lte]. Usado para
-- atribuir MRs ao deployment que "fechou" aquele intervalo de tempo.
SELECT * FROM platform.merge_request
WHERE project_id = sqlc.arg(project_id)
  AND target_branch = ANY(sqlc.arg(target_branches)::text[])
  AND merged_at > sqlc.arg(merged_after)
  AND merged_at <= sqlc.arg(merged_until)
  AND NOT author_is_bot;
