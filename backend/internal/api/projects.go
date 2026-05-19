package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// TODO Fase 1: implementar com storage real.

func (s *Server) handleListProjects() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	}
}

func (s *Server) handleProjectMetrics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectId")
		window := r.URL.Query().Get("window")
		if window == "" {
			window = "30d"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"projectId":            projectID,
			"windowDays":           windowDays(window),
			"classification":       "insufficient_data",
			"sampleSize":           0,
			"deploymentFrequency":  0,
		})
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
