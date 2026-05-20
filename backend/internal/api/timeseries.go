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

type timeseriesPoint struct {
	Day         string `json:"day"`        // ISO date YYYY-MM-DD
	DeployCount int    `json:"deployCount"`
}

type timeseriesResponse struct {
	ProjectID  string            `json:"projectId"`
	WindowDays int               `json:"windowDays"`
	Metric     string            `json:"metric"`
	Points     []timeseriesPoint `json:"points"`
}

func (s *Server) handleProjectTimeseries() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
		if err != nil {
			http.Error(w, "invalid project id", http.StatusBadRequest)
			return
		}

		windowDays := windowDays(r.URL.Query().Get("window"))
		since := time.Now().UTC().AddDate(0, 0, -windowDays)

		q := queries.New(s.db.Pool)

		if _, err := q.GetProject(r.Context(), projectID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "project not found", http.StatusNotFound)
				return
			}
			log.Error().Err(err).Msg("get project")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		rows, err := q.DeploymentsPerDayInWindow(r.Context(),
			queries.DeploymentsPerDayInWindowParams{
				ProjectID:     projectID,
				FinishedSince: pgTime(since),
			})
		if err != nil {
			log.Error().Err(err).Msg("deployments per day")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Preenche dias sem deploy com 0 para a curva ficar contínua no front.
		filled := fillDays(since.Truncate(24*time.Hour), windowDays, rows)

		writeJSON(w, http.StatusOK, timeseriesResponse{
			ProjectID:  projectID.String(),
			WindowDays: windowDays,
			Metric:     "deployment_frequency",
			Points:     filled,
		})
	}
}

func fillDays(since time.Time, windowDays int, rows []queries.DeploymentsPerDayInWindowRow) []timeseriesPoint {
	byDay := make(map[string]int, len(rows))
	for _, r := range rows {
		if r.Day.Valid {
			byDay[r.Day.Time.Format("2006-01-02")] = int(r.DeployCount)
		}
	}
	out := make([]timeseriesPoint, 0, windowDays)
	for i := 0; i < windowDays; i++ {
		day := since.AddDate(0, 0, i).Format("2006-01-02")
		out = append(out, timeseriesPoint{Day: day, DeployCount: byDay[day]})
	}
	return out
}

type deploymentDTO struct {
	ID              string  `json:"id"`
	SHA             string  `json:"sha"`
	Ref             *string `json:"ref"`
	Status          string  `json:"status"`
	TriggeredBy     *string `json:"triggeredBy"`
	StartedAt       *string `json:"startedAt"`
	FinishedAt      *string `json:"finishedAt"`
	EnvironmentName string  `json:"environmentName"`
}

func (s *Server) handleProjectDeployments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
		if err != nil {
			http.Error(w, "invalid project id", http.StatusBadRequest)
			return
		}

		windowDays := windowDays(r.URL.Query().Get("window"))
		since := time.Now().UTC().AddDate(0, 0, -windowDays)

		q := queries.New(s.db.Pool)
		rows, err := q.ListProductionDeploymentsInWindow(r.Context(),
			queries.ListProductionDeploymentsInWindowParams{
				ProjectID:     projectID,
				FinishedSince: pgTime(since),
			})
		if err != nil {
			log.Error().Err(err).Msg("list deployments")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		out := make([]deploymentDTO, 0, len(rows))
		for _, d := range rows {
			out = append(out, deploymentDTO{
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
		writeJSON(w, http.StatusOK, out)
	}
}

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func formatTimestamptz(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.UTC().Format(time.RFC3339)
	return &s
}
