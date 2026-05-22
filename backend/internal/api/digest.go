// Endpoint REST do weekly digest. Lê de platform.digest_snapshot.
// Se `week` não vier no query, devolve o último snapshot disponível.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
)

type digestDTO struct {
	IsoWeek          string          `json:"isoWeek"`
	WeekStart        string          `json:"weekStart"`
	WeekEnd          string          `json:"weekEnd"`
	DeploymentsCount int             `json:"deploymentsCount"`
	IncidentsCount   int             `json:"incidentsCount"`
	CurrentTier      *string         `json:"currentTier"`
	PreviousTier     *string         `json:"previousTier"`
	TierDelta        int             `json:"tierDelta"`
	TopContributors  json.RawMessage `json:"topContributors"`
	ComputedAt       string          `json:"computedAt"`
}

func (s *Server) handleProjectDigest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "projectId"))
		if err != nil {
			http.Error(w, "invalid project id", http.StatusBadRequest)
			return
		}
		s.fetchAndWriteDigest(w, r, "project", id)
	}
}

func (s *Server) handleTeamDigest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "teamId"))
		if err != nil {
			http.Error(w, "invalid team id", http.StatusBadRequest)
			return
		}
		s.fetchAndWriteDigest(w, r, "team", id)
	}
}

func (s *Server) fetchAndWriteDigest(w http.ResponseWriter, r *http.Request, scopeKind string, scopeID uuid.UUID) {
	week := r.URL.Query().Get("week")

	var (
		dto       digestDTO
		weekStart time.Time
		weekEnd   time.Time
		computed  time.Time
	)

	if week == "" {
		err := s.db.Pool.QueryRow(r.Context(), `
			SELECT iso_week, week_start, week_end, deployments_count, incidents_count,
			       current_tier, previous_tier, tier_delta, top_contributors, computed_at
			FROM platform.digest_snapshot
			WHERE scope_kind = $1 AND scope_id = $2
			ORDER BY iso_week DESC
			LIMIT 1
		`, scopeKind, scopeID).Scan(
			&dto.IsoWeek, &weekStart, &weekEnd, &dto.DeploymentsCount, &dto.IncidentsCount,
			&dto.CurrentTier, &dto.PreviousTier, &dto.TierDelta, &dto.TopContributors, &computed,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "no digest snapshot yet", http.StatusNotFound)
				return
			}
			log.Error().Err(err).Msg("fetch digest")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	} else {
		err := s.db.Pool.QueryRow(r.Context(), `
			SELECT iso_week, week_start, week_end, deployments_count, incidents_count,
			       current_tier, previous_tier, tier_delta, top_contributors, computed_at
			FROM platform.digest_snapshot
			WHERE scope_kind = $1 AND scope_id = $2 AND iso_week = $3
		`, scopeKind, scopeID, week).Scan(
			&dto.IsoWeek, &weekStart, &weekEnd, &dto.DeploymentsCount, &dto.IncidentsCount,
			&dto.CurrentTier, &dto.PreviousTier, &dto.TierDelta, &dto.TopContributors, &computed,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "digest snapshot not found", http.StatusNotFound)
				return
			}
			log.Error().Err(err).Msg("fetch digest by week")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	dto.WeekStart = weekStart.UTC().Format("2006-01-02")
	dto.WeekEnd = weekEnd.UTC().Format("2006-01-02")
	dto.ComputedAt = computed.UTC().Format(time.RFC3339)

	writeJSON(w, http.StatusOK, dto)
}
