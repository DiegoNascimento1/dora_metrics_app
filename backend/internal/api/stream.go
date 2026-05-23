// Endpoint SSE para métricas em tempo real.
//
//	GET /api/v1/projects/{projectId}/metrics/stream
//
// Mantém conexão SSE aberta e envia o payload de métricas sempre que a
// metric_window for atualizada (poll de 30s contra o banco). Heartbeat a
// cada 30s para manter proxies vivos.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

func (s *Server) handleMetricsStream() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
		if err != nil {
			http.Error(w, "invalid project id", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		ctx := r.Context()

		// Envia métricas atuais imediatamente.
		if payload, fetchErr := s.latestMetricsPayload(ctx, projectID); fetchErr == nil {
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		var lastSent []byte

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				payload, fetchErr := s.latestMetricsPayload(ctx, projectID)
				if fetchErr != nil {
					log.Warn().Err(fetchErr).Str("project_id", projectID.String()).
						Msg("sse: erro ao buscar métricas")
					fmt.Fprintf(w, "event: heartbeat\ndata: {}\n\n")
					flusher.Flush()
					continue
				}

				if string(payload) != string(lastSent) {
					fmt.Fprintf(w, "data: %s\n\n", payload)
					flusher.Flush()
					lastSent = payload
				} else {
					fmt.Fprintf(w, "event: heartbeat\ndata: {}\n\n")
					flusher.Flush()
				}
			}
		}
	}
}

type metricsEvent struct {
	WindowDays        int     `json:"window_days"`
	DeploymentFreq    float64 `json:"deployment_frequency"`
	LeadTimeMedianSec *int64  `json:"lead_time_median_s"`
	ChangeFailureRate float64 `json:"change_failure_rate"`
	MTTRMeanSec       *int64  `json:"mttr_mean_s"`
	CombinedTier      string  `json:"combined_tier"`
	ComputedAt        string  `json:"computed_at"`
}

func (s *Server) latestMetricsPayload(ctx context.Context, projectID uuid.UUID) ([]byte, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT
			window_days,
			deployment_frequency,
			lead_time_median_s,
			change_failure_rate,
			mttr_mean_s,
			combined_tier,
			computed_at
		FROM metrics.metric_window
		WHERE scope_kind = 'project'
		  AND scope_id   = $1
		ORDER BY computed_at DESC
		LIMIT 1
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return json.Marshal(map[string]string{"status": "no_data"})
	}

	var ev metricsEvent
	if err := rows.Scan(
		&ev.WindowDays, &ev.DeploymentFreq,
		&ev.LeadTimeMedianSec, &ev.ChangeFailureRate,
		&ev.MTTRMeanSec, &ev.CombinedTier, &ev.ComputedAt,
	); err != nil {
		return nil, err
	}

	return json.Marshal(ev)
}
