package api

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

// handleProjectExport serve o dump bruto da janela em CSV ou JSON.
//
// Query params:
//   - kind: deployments | incidents | merge_requests (obrigatório)
//   - format: csv | json (default csv)
//   - window: 7d | 30d | 90d (default 30d)
func (s *Server) handleProjectExport() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
		if err != nil {
			http.Error(w, "invalid project id", http.StatusBadRequest)
			return
		}

		kind := r.URL.Query().Get("kind")
		switch kind {
		case "deployments", "incidents", "merge_requests":
		default:
			http.Error(w, "kind must be one of: deployments, incidents, merge_requests", http.StatusBadRequest)
			return
		}

		format := r.URL.Query().Get("format")
		if format == "" {
			format = "csv"
		}
		if format != "csv" && format != "json" {
			http.Error(w, "format must be csv or json", http.StatusBadRequest)
			return
		}

		days := windowDays(r.URL.Query().Get("window"))
		since := time.Now().UTC().AddDate(0, 0, -days)

		q := queries.New(s.db.Pool)
		project, err := q.GetProject(r.Context(), projectID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "project not found", http.StatusNotFound)
				return
			}
			log.Error().Err(err).Msg("export: get project")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		filename := fmt.Sprintf("%s-%s-%dd-%s.%s",
			kind,
			safeSlug(project.PathWithNamespace),
			days,
			time.Now().UTC().Format("2006-01-02"),
			format,
		)
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

		switch kind {
		case "deployments":
			rows, err := q.ListProductionDeploymentsInWindow(r.Context(),
				queries.ListProductionDeploymentsInWindowParams{
					ProjectID:     projectID,
					FinishedSince: pgTime(since),
				})
			if err != nil {
				log.Error().Err(err).Msg("export deployments")
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			writeDeployments(w, format, rows)
		case "incidents":
			rows, err := q.ListIncidentsInWindowForProject(r.Context(),
				queries.ListIncidentsInWindowForProjectParams{
					ProjectID: projectID,
					Since:     since,
				})
			if err != nil {
				log.Error().Err(err).Msg("export incidents")
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			writeIncidents(w, format, rows)
		case "merge_requests":
			rows, err := q.ListMergedMRsInWindowForProject(r.Context(),
				queries.ListMergedMRsInWindowForProjectParams{
					ProjectID: projectID,
					Since:     pgTime(since),
				})
			if err != nil {
				log.Error().Err(err).Msg("export merge_requests")
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			writeMergeRequests(w, format, rows)
		}
	}
}

func writeDeployments(w http.ResponseWriter, format string, rows []queries.ListProductionDeploymentsInWindowRow) {
	type dto struct {
		ID              string  `json:"id"`
		SHA             string  `json:"sha"`
		Ref             *string `json:"ref"`
		Status          string  `json:"status"`
		TriggeredBy     *string `json:"triggeredBy"`
		StartedAt       *string `json:"startedAt"`
		FinishedAt      *string `json:"finishedAt"`
		EnvironmentName string  `json:"environmentName"`
	}
	out := make([]dto, 0, len(rows))
	for _, d := range rows {
		out = append(out, dto{
			ID:              d.ID.String(),
			SHA:             d.Sha,
			Ref:             d.Ref,
			Status:          d.Status,
			TriggeredBy:     d.TriggeredBy,
			StartedAt:       formatTimestamptz(d.StartedAt),
			FinishedAt:      formatTimestamptz(d.FinishedAt),
			EnvironmentName: d.EnvironmentName,
		})
	}

	if format == "json" {
		writeJSON(w, http.StatusOK, out)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "sha", "ref", "status", "triggered_by", "started_at", "finished_at", "environment"})
	for _, d := range out {
		_ = cw.Write([]string{
			d.ID,
			d.SHA,
			strOrEmpty(d.Ref),
			d.Status,
			strOrEmpty(d.TriggeredBy),
			strOrEmpty(d.StartedAt),
			strOrEmpty(d.FinishedAt),
			d.EnvironmentName,
		})
	}
	cw.Flush()
}

func writeIncidents(w http.ResponseWriter, format string, rows []queries.ListIncidentsInWindowForProjectRow) {
	type dto struct {
		ID             string  `json:"id"`
		Key            string  `json:"key"`
		ProjectKey     string  `json:"projectKey"`
		Summary        string  `json:"summary"`
		Status         string  `json:"status"`
		StatusCategory string  `json:"statusCategory"`
		Priority       *string `json:"priority"`
		IssueType      *string `json:"issueType"`
		CreatedAt      string  `json:"createdAt"`
		ResolvedAt     *string `json:"resolvedAt"`
		DurationSec    *int64  `json:"durationSeconds"`
	}
	out := make([]dto, 0, len(rows))
	for _, i := range rows {
		d := dto{
			ID:             i.ID.String(),
			Key:            i.ExternalID,
			ProjectKey:     i.JiraProjectKey,
			Summary:        i.Summary,
			Status:         i.Status,
			StatusCategory: i.StatusCategory,
			Priority:       i.Priority,
			IssueType:      i.Issuetype,
			CreatedAt:      i.CreatedAt.UTC().Format(time.RFC3339),
			ResolvedAt:     formatTimestamptz(i.ResolvedAt),
		}
		if i.ResolvedAt.Valid {
			sec := int64(i.ResolvedAt.Time.Sub(i.CreatedAt).Seconds())
			d.DurationSec = &sec
		}
		out = append(out, d)
	}

	if format == "json" {
		writeJSON(w, http.StatusOK, out)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "key", "project_key", "summary", "status", "status_category", "priority", "issue_type", "created_at", "resolved_at", "duration_seconds"})
	for _, d := range out {
		dur := ""
		if d.DurationSec != nil {
			dur = fmt.Sprintf("%d", *d.DurationSec)
		}
		_ = cw.Write([]string{
			d.ID,
			d.Key,
			d.ProjectKey,
			d.Summary,
			d.Status,
			d.StatusCategory,
			strOrEmpty(d.Priority),
			strOrEmpty(d.IssueType),
			d.CreatedAt,
			strOrEmpty(d.ResolvedAt),
			dur,
		})
	}
	cw.Flush()
}

func writeMergeRequests(w http.ResponseWriter, format string, rows []queries.ListMergedMRsInWindowForProjectRow) {
	type dto struct {
		ID             string  `json:"id"`
		ExternalID     string  `json:"externalId"`
		IID            int32   `json:"iid"`
		Title          string  `json:"title"`
		Author         *string `json:"author"`
		TargetBranch   string  `json:"targetBranch"`
		SourceBranch   *string `json:"sourceBranch"`
		MergedAt       *string `json:"mergedAt"`
		FirstCommitAt  *string `json:"firstCommitAt"`
		LeadTimeSec    *int64  `json:"leadTimeSeconds"`
		Additions      *int32  `json:"additions"`
		Deletions      *int32  `json:"deletions"`
		MergeCommitSHA *string `json:"mergeCommitSha"`
		WebURL         *string `json:"webUrl"`
	}
	out := make([]dto, 0, len(rows))
	for _, m := range rows {
		d := dto{
			ID:             m.ID.String(),
			ExternalID:     m.ExternalID,
			IID:            m.Iid,
			Title:          m.Title,
			Author:         m.AuthorUsername,
			TargetBranch:   m.TargetBranch,
			SourceBranch:   m.SourceBranch,
			MergedAt:       formatTimestamptz(m.MergedAt),
			FirstCommitAt:  formatTimestamptz(m.FirstCommitAt),
			Additions:      m.Additions,
			Deletions:      m.Deletions,
			MergeCommitSHA: m.MergeCommitSha,
			WebURL:         m.WebUrl,
		}
		if m.MergedAt.Valid && m.FirstCommitAt.Valid {
			sec := int64(m.MergedAt.Time.Sub(m.FirstCommitAt.Time).Seconds())
			d.LeadTimeSec = &sec
		}
		out = append(out, d)
	}

	if format == "json" {
		writeJSON(w, http.StatusOK, out)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "external_id", "iid", "title", "author", "target_branch", "source_branch", "merged_at", "first_commit_at", "lead_time_seconds", "additions", "deletions", "merge_commit_sha", "web_url"})
	for _, d := range out {
		add := ""
		if d.Additions != nil {
			add = fmt.Sprintf("%d", *d.Additions)
		}
		del := ""
		if d.Deletions != nil {
			del = fmt.Sprintf("%d", *d.Deletions)
		}
		lt := ""
		if d.LeadTimeSec != nil {
			lt = fmt.Sprintf("%d", *d.LeadTimeSec)
		}
		_ = cw.Write([]string{
			d.ID,
			d.ExternalID,
			fmt.Sprintf("%d", d.IID),
			d.Title,
			strOrEmpty(d.Author),
			d.TargetBranch,
			strOrEmpty(d.SourceBranch),
			strOrEmpty(d.MergedAt),
			strOrEmpty(d.FirstCommitAt),
			lt,
			add,
			del,
			strOrEmpty(d.MergeCommitSHA),
			strOrEmpty(d.WebURL),
		})
	}
	cw.Flush()
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// safeSlug converte path com namespace ("gitlab-org/gitlab") em slug seguro
// para nome de arquivo: troca / e espaço por _, remove qualquer outro caractere.
func safeSlug(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == '/', r == ' ':
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "project"
	}
	return b.String()
}

