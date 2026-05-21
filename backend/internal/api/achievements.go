package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/gamification"
	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

type achievementsDTO struct {
	ProjectID             string                     `json:"projectId"`
	WindowDays            int                        `json:"windowDays"`
	DaysSinceLastIncident int                        `json:"daysSinceLastIncident"`
	CurrentClassification string                     `json:"currentClassification"`
	Achievements          []gamification.Achievement `json:"achievements"`
}

func (s *Server) handleProjectAchievements() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
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

		days, err := q.DaysSinceLastIncidentForProject(r.Context(), project.ID)
		if err != nil {
			log.Error().Err(err).Msg("days since last incident")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		mwRow, err := q.GetLatestMetricWindow(r.Context(), queries.GetLatestMetricWindowParams{
			TenantID:   project.TenantID,
			ScopeKind:  "project",
			ScopeID:    project.ID,
			WindowDays: int32(windowDays),
		})
		classification := "insufficient_data"
		sample := 0
		if err == nil {
			if mwRow.Classification != nil {
				classification = *mwRow.Classification
			}
			sample = int(mwRow.SampleSize)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			log.Error().Err(err).Msg("get latest metric window for achievements")
		}

		ach := gamification.EvaluateAchievements(
			gamification.ProjectStats{
				DaysSinceLastIncident: int(days),
				CurrentClassification: classification,
				SampleSize:            sample,
			},
			time.Now().UTC().Format("2006-01-02"),
		)

		writeJSON(w, http.StatusOK, achievementsDTO{
			ProjectID:             projectID.String(),
			WindowDays:            windowDays,
			DaysSinceLastIncident: int(days),
			CurrentClassification: classification,
			Achievements:          ach,
		})
	}
}
