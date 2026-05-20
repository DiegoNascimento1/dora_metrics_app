package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

type projectDTO struct {
	ID                string  `json:"id"`
	Slug              string  `json:"slug"`
	Name              string  `json:"name"`
	PathWithNamespace string  `json:"pathWithNamespace"`
	TeamID            *string `json:"teamId"`
	Active            bool    `json:"active"`
}

func (s *Server) handleListProjects() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := queries.New(s.db.Pool)
		rows, err := q.ListProjects(r.Context())
		if err != nil {
			log.Error().Err(err).Msg("list projects")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		out := make([]projectDTO, 0, len(rows))
		for _, p := range rows {
			out = append(out, toProjectDTO(p))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func toProjectDTO(p queries.PlatformProject) projectDTO {
	var teamID *string
	if p.TeamID.Valid {
		id, err := uuid.FromBytes(p.TeamID.Bytes[:])
		if err == nil {
			v := id.String()
			teamID = &v
		}
	}
	return projectDTO{
		ID:                p.ID.String(),
		Slug:              p.PathWithNamespace,
		Name:              p.PathWithNamespace,
		PathWithNamespace: p.PathWithNamespace,
		TeamID:            teamID,
		Active:            p.Active,
	}
}

type doraMetricsDTO struct {
	ProjectID             string   `json:"projectId"`
	WindowDays            int      `json:"windowDays"`
	ComputedAt            string   `json:"computedAt"`
	DeploymentFrequency   float64  `json:"deploymentFrequency"`
	LeadTimeMedianSeconds *int64   `json:"leadTimeMedianSeconds"`
	ChangeFailureRate     *float64 `json:"changeFailureRate"`
	MTTRMeanSeconds       *int64   `json:"mttrMeanSeconds"`
	Classification        string   `json:"classification"`
	SampleSize            int      `json:"sampleSize"`
}

func (s *Server) handleProjectMetrics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectIDStr := chi.URLParam(r, "projectId")
		projectID, err := uuid.Parse(projectIDStr)
		if err != nil {
			http.Error(w, "invalid project id", http.StatusBadRequest)
			return
		}

		windowDays := windowDays(r.URL.Query().Get("window"))

		q := queries.New(s.db.Pool)

		project, err := q.GetProject(r.Context(), projectID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "project not found", http.StatusNotFound)
				return
			}
			log.Error().Err(err).Msg("get project")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		row, err := q.GetLatestMetricWindow(r.Context(), queries.GetLatestMetricWindowParams{
			TenantID:   project.TenantID,
			ScopeKind:  "project",
			ScopeID:    project.ID,
			WindowDays: int32(windowDays),
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			log.Error().Err(err).Msg("get latest metric window")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		out := doraMetricsDTO{
			ProjectID:      projectID.String(),
			WindowDays:     windowDays,
			Classification: "insufficient_data",
		}

		if errors.Is(err, pgx.ErrNoRows) {
			out.ComputedAt = time.Now().UTC().Format(time.RFC3339)
			writeJSON(w, http.StatusOK, out)
			return
		}

		out.ComputedAt = row.ComputedAt.UTC().Format(time.RFC3339)
		out.SampleSize = int(row.SampleSize)
		out.DeploymentFrequency = numericToFloat(row.DeploymentFrequency)
		out.LeadTimeMedianSeconds = row.LeadTimeMedianS
		out.MTTRMeanSeconds = row.MttrMeanS
		if row.Classification != nil {
			out.Classification = *row.Classification
		}
		out.ChangeFailureRate = numericToFloatPtr(row.ChangeFailureRate)

		writeJSON(w, http.StatusOK, out)
	}
}

func windowDays(w string) int {
	switch w {
	case "7d":
		return 7
	case "90d":
		return 90
	default:
		return 30
	}
}

func numericToFloat(n pgtype.Numeric) float64 {
	if !n.Valid {
		return 0
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}

func numericToFloatPtr(n pgtype.Numeric) *float64 {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	v := f.Float64
	return &v
}
