package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

type alertRuleDTO struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Enabled    bool    `json:"enabled"`
	Kind       string  `json:"kind"`
	ScopeKind  string  `json:"scopeKind"`
	ScopeID    *string `json:"scopeId"`
	WindowDays int32   `json:"windowDays"`
	WebhookURL string  `json:"webhookUrl"`
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
}

type createAlertRuleRequest struct {
	Tenant     string  `json:"tenant"`
	Name       string  `json:"name"`
	Enabled    *bool   `json:"enabled"`
	Kind       string  `json:"kind"`
	ScopeKind  string  `json:"scopeKind"`
	ScopeID    *string `json:"scopeId"`
	WindowDays int32   `json:"windowDays"`
	WebhookURL string  `json:"webhookUrl"`
}

type updateAlertRuleRequest struct {
	Name       *string `json:"name"`
	Enabled    *bool   `json:"enabled"`
	Kind       *string `json:"kind"`
	ScopeKind  *string `json:"scopeKind"`
	ScopeID    *string `json:"scopeId"`
	WindowDays *int32  `json:"windowDays"`
	WebhookURL *string `json:"webhookUrl"`
}

type alertEventDTO struct {
	ID             string  `json:"id"`
	RuleID         string  `json:"ruleId"`
	FiredAt        string  `json:"firedAt"`
	ScopeKind      string  `json:"scopeKind"`
	ScopeID        string  `json:"scopeId"`
	PreviousTier   *string `json:"previousTier"`
	CurrentTier    string  `json:"currentTier"`
	DeliveryStatus string  `json:"deliveryStatus"`
	HTTPStatus     *int32  `json:"httpStatus"`
	LastError      *string `json:"lastError"`
	DeliveredAt    *string `json:"deliveredAt"`
}

func (s *Server) handleListAlertRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, ok := s.requireTenant(w, r)
		if !ok {
			return
		}
		q := queries.New(s.db.Pool)
		rows, err := q.ListAlertRulesByTenant(r.Context(), tenant.ID)
		if err != nil {
			log.Error().Err(err).Msg("list alert rules")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		out := make([]alertRuleDTO, 0, len(rows))
		for _, row := range rows {
			out = append(out, toAlertRuleDTO(row))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Server) handleCreateAlertRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createAlertRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		req.Tenant = strings.TrimSpace(req.Tenant)
		req.Name = strings.TrimSpace(req.Name)
		req.WebhookURL = strings.TrimSpace(req.WebhookURL)
		if req.Tenant == "" || req.Name == "" || req.WebhookURL == "" {
			http.Error(w, "tenant, name and webhookUrl required", http.StatusBadRequest)
			return
		}
		if !validAlertKind(req.Kind) {
			http.Error(w, "kind must be tier_regression or tier_change", http.StatusBadRequest)
			return
		}
		if !validScopeKind(req.ScopeKind) {
			http.Error(w, "scopeKind must be project, team or tenant", http.StatusBadRequest)
			return
		}
		if !validWindowDays(req.WindowDays) {
			http.Error(w, "windowDays must be 7, 30 or 90", http.StatusBadRequest)
			return
		}

		scopeID := pgtype.UUID{Valid: false}
		if req.ScopeID != nil && *req.ScopeID != "" {
			id, err := uuid.Parse(*req.ScopeID)
			if err != nil {
				http.Error(w, "invalid scopeId", http.StatusBadRequest)
				return
			}
			scopeID = pgtype.UUID{Bytes: id, Valid: true}
		}

		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}

		q := queries.New(s.db.Pool)
		tenant, err := q.GetTenantBySlug(r.Context(), req.Tenant)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "tenant not found", http.StatusNotFound)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		row, err := q.CreateAlertRule(r.Context(), queries.CreateAlertRuleParams{
			TenantID:   tenant.ID,
			Name:       req.Name,
			Enabled:    enabled,
			Kind:       req.Kind,
			ScopeKind:  req.ScopeKind,
			ScopeID:    scopeID,
			WindowDays: req.WindowDays,
			WebhookUrl: req.WebhookURL,
		})
		if err != nil {
			log.Error().Err(err).Msg("create alert rule")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, toAlertRuleDTO(row))
	}
}

func (s *Server) handleUpdateAlertRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "ruleId"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		var req updateAlertRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if req.Kind != nil && !validAlertKind(*req.Kind) {
			http.Error(w, "kind must be tier_regression or tier_change", http.StatusBadRequest)
			return
		}
		if req.ScopeKind != nil && !validScopeKind(*req.ScopeKind) {
			http.Error(w, "scopeKind invalid", http.StatusBadRequest)
			return
		}
		if req.WindowDays != nil && !validWindowDays(*req.WindowDays) {
			http.Error(w, "windowDays must be 7, 30 or 90", http.StatusBadRequest)
			return
		}

		scopeID := pgtype.UUID{Valid: false}
		if req.ScopeID != nil && *req.ScopeID != "" {
			parsed, err := uuid.Parse(*req.ScopeID)
			if err != nil {
				http.Error(w, "invalid scopeId", http.StatusBadRequest)
				return
			}
			scopeID = pgtype.UUID{Bytes: parsed, Valid: true}
		}

		q := queries.New(s.db.Pool)
		row, err := q.UpdateAlertRule(r.Context(), queries.UpdateAlertRuleParams{
			ID:         id,
			Name:       req.Name,
			Enabled:    req.Enabled,
			Kind:       req.Kind,
			ScopeKind:  req.ScopeKind,
			ScopeID:    scopeID,
			WindowDays: req.WindowDays,
			WebhookUrl: req.WebhookURL,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "alert rule not found", http.StatusNotFound)
				return
			}
			log.Error().Err(err).Msg("update alert rule")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, toAlertRuleDTO(row))
	}
}

func (s *Server) handleDeleteAlertRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "ruleId"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		q := queries.New(s.db.Pool)
		if err := q.DeleteAlertRule(r.Context(), id); err != nil {
			log.Error().Err(err).Msg("delete alert rule")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleListAlertEvents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, ok := s.requireTenant(w, r)
		if !ok {
			return
		}
		limit := int32(50)
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
				limit = int32(n)
			}
		}
		q := queries.New(s.db.Pool)
		rows, err := q.ListRecentAlertEvents(r.Context(), queries.ListRecentAlertEventsParams{
			TenantID: tenant.ID,
			Limit:    limit,
		})
		if err != nil {
			log.Error().Err(err).Msg("list alert events")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		out := make([]alertEventDTO, 0, len(rows))
		for _, row := range rows {
			out = append(out, toAlertEventDTO(row))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func toAlertRuleDTO(r queries.PlatformAlertRule) alertRuleDTO {
	var scopeID *string
	if r.ScopeID.Valid {
		s := uuid.UUID(r.ScopeID.Bytes).String()
		scopeID = &s
	}
	return alertRuleDTO{
		ID:         r.ID.String(),
		Name:       r.Name,
		Enabled:    r.Enabled,
		Kind:       r.Kind,
		ScopeKind:  r.ScopeKind,
		ScopeID:    scopeID,
		WindowDays: r.WindowDays,
		WebhookURL: r.WebhookUrl,
		CreatedAt:  r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  r.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toAlertEventDTO(e queries.PlatformAlertEvent) alertEventDTO {
	var delivered *string
	if e.DeliveredAt.Valid {
		s := e.DeliveredAt.Time.UTC().Format(time.RFC3339)
		delivered = &s
	}
	return alertEventDTO{
		ID:             e.ID.String(),
		RuleID:         e.RuleID.String(),
		FiredAt:        e.FiredAt.UTC().Format(time.RFC3339),
		ScopeKind:      e.ScopeKind,
		ScopeID:        e.ScopeID.String(),
		PreviousTier:   e.PreviousTier,
		CurrentTier:    e.CurrentTier,
		DeliveryStatus: e.DeliveryStatus,
		HTTPStatus:     e.HttpStatus,
		LastError:      e.LastError,
		DeliveredAt:    delivered,
	}
}

func validAlertKind(k string) bool {
	return k == "tier_regression" || k == "tier_change"
}

func validScopeKind(k string) bool {
	return k == "project" || k == "team" || k == "tenant"
}

func validWindowDays(d int32) bool {
	return d == 7 || d == 30 || d == 90
}
