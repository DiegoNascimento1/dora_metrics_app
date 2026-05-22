// Endpoint REST que devolve a predição de degradação para um escopo.
//
//   GET /api/v1/projects/{id}/predict?window=30d&lookback=180
//   GET /api/v1/teams/{id}/predict?window=30d&lookback=180
//
// Lê os últimos N metric_window do escopo e roda prediction.Predict.
// Devolve Prediction direto — sem persistência (caching opcional no
// futuro).
package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/prediction"
)

const predictionDefaultLookbackDays = 180
const predictionMaxSamples = 50

func (s *Server) handleProjectPredict() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "projectId"))
		if err != nil {
			http.Error(w, "invalid project id", http.StatusBadRequest)
			return
		}
		s.runPredict(w, r, "project", id)
	}
}

func (s *Server) handleTeamPredict() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "teamId"))
		if err != nil {
			http.Error(w, "invalid team id", http.StatusBadRequest)
			return
		}
		s.runPredict(w, r, "team", id)
	}
}

func (s *Server) runPredict(w http.ResponseWriter, r *http.Request, scopeKind string, scopeID uuid.UUID) {
	windowDays := windowDays(r.URL.Query().Get("window"))

	// Lookback configurável (default 180 dias = 6 meses).
	lookback := predictionDefaultLookbackDays
	if v := r.URL.Query().Get("lookback"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 365 {
			lookback = n
		}
	}

	samples, err := s.fetchTrendSamples(r.Context(), scopeKind, scopeID, windowDays, lookback)
	if err != nil {
		log.Error().Err(err).Msg("fetch trend samples")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	pred := prediction.Predict(samples)
	writeJSON(w, http.StatusOK, map[string]any{
		"scopeKind":  scopeKind,
		"scopeId":    scopeID.String(),
		"windowDays": windowDays,
		"lookback":   lookback,
		"prediction": pred,
	})
}

// fetchTrendSamples lê metric_window do scope na janela `windowDays`,
// dos últimos `lookback` dias, devolve até `predictionMaxSamples`
// ordenados por computed_at ASC.
//
// Usa SQL inline (sem regerar sqlc) porque é leitura simples e
// específica desta feature.
func (s *Server) fetchTrendSamples(
	ctx context.Context,
	scopeKind string,
	scopeID uuid.UUID,
	windowDays, lookback int,
) ([]prediction.Sample, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -lookback)

	rows, err := s.db.Pool.Query(ctx, `
		SELECT computed_at, COALESCE(classification, 'insufficient_data') AS tier
		FROM metrics.metric_window
		WHERE scope_kind = $1 AND scope_id = $2 AND window_days = $3
		  AND computed_at >= $4
		ORDER BY computed_at ASC
		LIMIT $5
	`, scopeKind, scopeID, windowDays, cutoff, predictionMaxSamples)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []prediction.Sample
	for rows.Next() {
		var ts time.Time
		var t string
		if err := rows.Scan(&ts, &t); err != nil {
			return nil, err
		}
		out = append(out, prediction.Sample{T: ts, Tier: t})
	}
	return out, rows.Err()
}
