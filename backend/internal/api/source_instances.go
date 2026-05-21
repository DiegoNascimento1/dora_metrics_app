package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/collector/gitlab"
	"github.com/dora-metrics-app/backend/internal/collector/jira"
	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

// sourceInstanceDTO é a projeção REST. NUNCA inclui secret_value — só
// hasSecret booleano pra UI saber se já tem credencial gravada.
type sourceInstanceDTO struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	BaseURL     string `json:"baseUrl"`
	DisplayName string `json:"displayName"`
	AuthRef     string `json:"authRef"`
	AuthEmail   string `json:"authEmail,omitempty"`
	HasSecret   bool   `json:"hasSecret"`
	CreatedAt   string `json:"createdAt"`
}

type createSourceInstanceRequest struct {
	Tenant      string `json:"tenant"`
	Kind        string `json:"kind"` // "gitlab" | "jira"
	BaseURL     string `json:"baseUrl"`
	DisplayName string `json:"displayName"`
	Secret      string `json:"secret"`    // token; NUNCA persistido em log
	AuthEmail   string `json:"authEmail"` // só Jira
}

type testConnectionRequest struct {
	Kind      string `json:"kind"`
	BaseURL   string `json:"baseUrl"`
	Secret    string `json:"secret"`
	AuthEmail string `json:"authEmail"`
}

type testConnectionResponse struct {
	OK      bool   `json:"ok"`
	Status  int    `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

// ---- handlers ----

func (s *Server) handleListSourceInstances() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, ok := s.requireTenant(w, r)
		if !ok {
			return
		}
		q := queries.New(s.db.Pool)
		rows, err := q.ListSourceInstancesByTenant(r.Context(), tenant.ID)
		if err != nil {
			log.Error().Err(err).Msg("list source instances")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		out := make([]sourceInstanceDTO, 0, len(rows))
		for _, si := range rows {
			out = append(out, toSourceInstanceDTO(si))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Server) handleCreateSourceInstance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createSourceInstanceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if req.Tenant == "" || req.Kind == "" || req.BaseURL == "" || req.DisplayName == "" {
			http.Error(w, "tenant, kind, baseUrl and displayName required", http.StatusBadRequest)
			return
		}
		if req.Kind != "gitlab" && req.Kind != "jira" {
			http.Error(w, "kind must be gitlab or jira", http.StatusBadRequest)
			return
		}
		if req.Kind == "jira" && req.AuthEmail == "" {
			http.Error(w, "authEmail required for jira", http.StatusBadRequest)
			return
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

		// auth_ref vira um pointer simbólico que diz "leia do banco". O
		// collector verifica secret_value primeiro, então o conteúdo do
		// auth_ref para integrações criadas pela UI é só decorativo.
		authRef := fmt.Sprintf("db:source-instance/%s", strings.ToLower(req.Kind))

		row, err := q.CreateSourceInstance(r.Context(), queries.CreateSourceInstanceParams{
			TenantID:    tenant.ID,
			Kind:        req.Kind,
			BaseUrl:     strings.TrimRight(req.BaseURL, "/"),
			DisplayName: req.DisplayName,
			AuthRef:     authRef,
			SecretValue: nilIfEmpty(req.Secret),
			AuthEmail:   nilIfEmpty(req.AuthEmail),
		})
		if err != nil {
			log.Error().Err(err).Msg("create source instance")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, toSourceInstanceDTO(row))
	}
}

func (s *Server) handleDeleteSourceInstance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "sourceInstanceId"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		q := queries.New(s.db.Pool)
		if err := q.DeleteSourceInstance(r.Context(), id); err != nil {
			log.Error().Err(err).Msg("delete source instance")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleTestConnection valida credenciais ANTES de salvar.
// Não persiste nada — só faz uma chamada read-only no fornecedor.
func (s *Server) handleTestConnection() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req testConnectionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusOK, testConnectionResponse{OK: false, Message: "invalid body"})
			return
		}
		if req.Kind == "" || req.BaseURL == "" || req.Secret == "" {
			writeJSON(w, http.StatusOK, testConnectionResponse{OK: false, Message: "kind, baseUrl, secret obrigatórios"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		switch req.Kind {
		case "gitlab":
			client := gitlab.NewClient(strings.TrimRight(req.BaseURL, "/"), req.Secret)
			if err := client.Ping(ctx); err != nil {
				writeJSON(w, http.StatusOK, testConnectionResponse{
					OK: false, Message: err.Error(),
				})
				return
			}
			writeJSON(w, http.StatusOK, testConnectionResponse{OK: true, Status: 200, Message: "GitLab respondeu /api/v4/version"})

		case "jira":
			if req.AuthEmail == "" {
				writeJSON(w, http.StatusOK, testConnectionResponse{OK: false, Message: "authEmail obrigatório para Jira"})
				return
			}
			src := jira.NewRESTSource(strings.TrimRight(req.BaseURL, "/"), req.AuthEmail, req.Secret)
			// Pequeno JQL no-op só pra testar auth: project = "PROBABLY_NOT_REAL"
			// retorna 400 (parse) se auth OK, 401 se auth ruim.
			_, err := src.SearchIssues(ctx, `project = "__dora_metrics_probe__"`, 1)
			if err == nil {
				writeJSON(w, http.StatusOK, testConnectionResponse{OK: true, Message: "Jira aceitou JQL (auth OK)"})
				return
			}
			// 400 com parse error é OK do ponto de vista de auth.
			if apiErr, ok := err.(*jira.APIError); ok {
				if apiErr.StatusCode == http.StatusBadRequest {
					writeJSON(w, http.StatusOK, testConnectionResponse{OK: true, Status: 400, Message: "Auth OK (Jira recusou JQL de teste, esperado)"})
					return
				}
				writeJSON(w, http.StatusOK, testConnectionResponse{OK: false, Status: apiErr.StatusCode, Message: apiErr.Message})
				return
			}
			writeJSON(w, http.StatusOK, testConnectionResponse{OK: false, Message: err.Error()})

		default:
			writeJSON(w, http.StatusOK, testConnectionResponse{OK: false, Message: "kind must be gitlab or jira"})
		}
	}
}

// ---- helpers ----

func toSourceInstanceDTO(si queries.PlatformSourceInstance) sourceInstanceDTO {
	dto := sourceInstanceDTO{
		ID:          si.ID.String(),
		Kind:        si.Kind,
		BaseURL:     si.BaseUrl,
		DisplayName: si.DisplayName,
		AuthRef:     si.AuthRef,
		HasSecret:   si.SecretValue != nil && *si.SecretValue != "",
		CreatedAt:   si.CreatedAt.UTC().Format(time.RFC3339),
	}
	if si.AuthEmail != nil {
		dto.AuthEmail = *si.AuthEmail
	}
	return dto
}
