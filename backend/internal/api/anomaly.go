// Endpoints de detecção de anomalias multivariadas nas métricas DORA.
//
//	GET /api/v1/projects/{id}/anomalies?window=90d
//	GET /api/v1/teams/{id}/anomalies?window=90d
//
// Lê as metric_window históricas do escopo, monta amostras multivariadas
// e executa prediction.DetectAnomalies (z-score). Devolve os pontos
// anômalos com metadata estatística.
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/prediction"
)

func (s *Server) handleProjectAnomalies() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "projectId"))
		if err != nil {
			http.Error(w, "invalid project id", http.StatusBadRequest)
			return
		}
		s.runAnomalies(w, r, "project", id)
	}
}

func (s *Server) handleTeamAnomalies() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "teamId"))
		if err != nil {
			http.Error(w, "invalid team id", http.StatusBadRequest)
			return
		}
		s.runAnomalies(w, r, "team", id)
	}
}

func (s *Server) runAnomalies(w http.ResponseWriter, r *http.Request, scopeKind string, scopeID uuid.UUID) {
	windowDays := windowDays(r.URL.Query().Get("window"))
	if windowDays == 0 {
		windowDays = 90
	}

	samples, analyzedFrom, analyzedTo, err := s.fetchMetricSamples(r.Context(), scopeKind, scopeID, windowDays)
	if err != nil {
		log.Error().Err(err).Str("scope_kind", scopeKind).Str("scope_id", scopeID.String()).
			Msg("fetch metric samples for anomaly detection")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	anomalies := prediction.DetectAnomalies(samples, prediction.DefaultAnomalyThreshold)

	// Garante array vazio em vez de null no JSON.
	if anomalies == nil {
		anomalies = []prediction.AnomalyPoint{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"scope_kind":    scopeKind,
		"scope_id":      scopeID.String(),
		"window_days":   windowDays,
		"anomalies":     anomalies,
		"analyzed_from": analyzedFrom.Format(time.RFC3339),
		"analyzed_to":   analyzedTo.Format(time.RFC3339),
	})
}

// fetchMetricSamples lê as metric_window históricas e monta []MetricSample.
// Usa SQL inline (sem sqlc) porque é leitura específica desta feature.
func (s *Server) fetchMetricSamples(
	ctx context.Context,
	scopeKind string,
	scopeID uuid.UUID,
	windowDays int,
) (samples []prediction.MetricSample, from, to time.Time, err error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -windowDays)

	rows, queryErr := s.db.Pool.Query(ctx, `
		SELECT
			computed_at,
			deployment_frequency,
			lead_time_median_s,
			change_failure_rate,
			mttr_mean_s
		FROM metrics.metric_window
		WHERE scope_kind = $1 AND scope_id = $2
		  AND computed_at >= $3
		ORDER BY computed_at ASC
		LIMIT 500
	`, scopeKind, scopeID, cutoff)
	if queryErr != nil {
		err = queryErr
		return
	}
	defer rows.Close()

	for rows.Next() {
		var computedAt time.Time
		var df pgtype.Numeric
		var ltS *int64
		var cfr pgtype.Numeric
		var mttrS *int64

		if scanErr := rows.Scan(&computedAt, &df, &ltS, &cfr, &mttrS); scanErr != nil {
			err = scanErr
			return
		}

		s := prediction.MetricSample{Date: computedAt}

		if df.Valid {
			if fv, fErr := df.Float64Value(); fErr == nil && fv.Valid {
				s.DeployFreq = fv.Float64
			}
		}
		if ltS != nil && *ltS > 0 {
			s.LeadTime = float64(*ltS) / 3600.0
		}
		if cfr.Valid {
			if fv, fErr := cfr.Float64Value(); fErr == nil && fv.Valid {
				s.CFR = fv.Float64
			}
		}
		if mttrS != nil && *mttrS > 0 {
			s.MTTR = float64(*mttrS) / 3600.0
		}

		samples = append(samples, s)
	}
	if rowErr := rows.Err(); rowErr != nil {
		err = rowErr
		return
	}

	to = time.Now().UTC()
	from = cutoff
	if len(samples) > 0 {
		from = samples[0].Date
		to = samples[len(samples)-1].Date
	}
	return
}
