// Endpoint SSE para métricas em tempo real.
//
//	GET /api/v1/projects/{projectId}/metrics/stream
//
// Quando Redis estiver disponível, usa pub/sub no canal "metrics:{project_id}"
// para receber eventos push em tempo real assim que a metric_window for
// recalculada. O worker publica nesse canal após cada UpsertMetricWindow.
//
// Fallback: quando Redis não estiver disponível, usa polling de 30s contra
// o banco (comportamento anterior).
//
// Heartbeat a cada 30s mantém a conexão viva através de proxies.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
)

const sseHeartbeatInterval = 30 * time.Second

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

		// Modo Redis pub/sub (preferido) ou polling (fallback).
		if s.rdb != nil {
			s.streamViaRedisPubSub(ctx, w, flusher, projectID)
		} else {
			s.streamViaPolling(ctx, w, flusher, projectID)
		}
	}
}

// streamViaRedisPubSub escuta o canal Redis "metrics:{project_id}" e
// retransmite os payloads como eventos SSE.
func (s *Server) streamViaRedisPubSub(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	projectID uuid.UUID,
) {
	channel := fmt.Sprintf("metrics:%s", projectID.String())
	pubsub := s.rdb.Subscribe(ctx, channel)
	defer pubsub.Close()

	// Aguarda confirmação da subscription.
	if _, err := pubsub.Receive(ctx); err != nil {
		log.Warn().Err(err).Str("channel", channel).Msg("sse: redis subscribe — fallback para polling")
		s.streamViaPolling(ctx, w, flusher, projectID)
		return
	}

	log.Debug().Str("channel", channel).Msg("sse: subscribed via redis pub/sub")

	msgCh := pubsub.Channel()
	ticker := time.NewTicker(sseHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Debug().Str("project_id", projectID.String()).Msg("sse: client disconnected")
			return

		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			if !json.Valid([]byte(msg.Payload)) {
				log.Warn().Str("channel", channel).Msg("sse: payload inválido, ignorando")
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", msg.Payload)
			flusher.Flush()

		case t := <-ticker.C:
			fmt.Fprintf(w, "event: heartbeat\ndata: {\"ts\":%q}\n\n", t.UTC().Format(time.RFC3339))
			flusher.Flush()
		}
	}
}

// streamViaPolling é o fallback quando Redis não está disponível.
// Faz poll de 30s contra o banco e envia eventos apenas quando há mudança.
func (s *Server) streamViaPolling(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	projectID uuid.UUID,
) {
	ticker := time.NewTicker(sseHeartbeatInterval)
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

func (s *Server) latestMetricsPayload(ctx context.Context, projectID uuid.UUID) ([]byte, error) {
	row := s.db.Pool.QueryRow(ctx, `
		SELECT
			window_days,
			deployment_frequency,
			lead_time_median_s,
			change_failure_rate,
			mttr_mean_s,
			COALESCE(classification, 'insufficient_data'),
			computed_at
		FROM metrics.metric_window
		WHERE scope_kind = 'project'
		  AND scope_id   = $1
		ORDER BY computed_at DESC
		LIMIT 1
	`, projectID)

	var windowDays int32
	var df pgtype.Numeric
	var ltS *int64
	var cfr pgtype.Numeric
	var mttrS *int64
	var classification string
	var computedAt time.Time

	if err := row.Scan(&windowDays, &df, &ltS, &cfr, &mttrS, &classification, &computedAt); err != nil {
		return json.Marshal(map[string]string{"status": "no_data"})
	}

	out := map[string]any{
		"project_id":     projectID.String(),
		"window_days":    windowDays,
		"classification": classification,
		"computed_at":    computedAt.UTC().Format(time.RFC3339),
	}
	if df.Valid {
		if fv, err := df.Float64Value(); err == nil && fv.Valid {
			out["deployment_frequency"] = fv.Float64
		}
	}
	if ltS != nil {
		out["lead_time_median_s"] = *ltS
	}
	if cfr.Valid {
		if fv, err := cfr.Float64Value(); err == nil && fv.Valid {
			out["change_failure_rate"] = fv.Float64
		}
	}
	if mttrS != nil {
		out["mttr_mean_s"] = *mttrS
	}

	return json.Marshal(out)
}
