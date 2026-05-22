// Endpoints REST que orquestram o flow OAuth 3LO do Atlassian.
//
//   POST /api/v1/integrations/atlassian/authorize
//     body: { "returnTo": "/settings" }
//     resp: { "authorizeUrl": "https://auth.atlassian.com/authorize?..." }
//     → Frontend redireciona o usuário para a URL.
//
//   GET  /api/v1/integrations/atlassian/callback?code=...&state=...
//     → Atlassian redireciona pra cá. Validamos state, trocamos code,
//       persistimos. Redirect final pro returnTo (UI mostra "Conectado!").
//
//   GET  /api/v1/integrations/atlassian/status
//     resp: { "connected": bool, "connection": {...}? }
//
//   DELETE /api/v1/integrations/atlassian/connection
//     → Desconecta (apaga tokens locais).
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/integrations/atlassian"
)

func (s *Server) handleAtlassianAuthorize() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.atlassian == nil {
			http.Error(w, "atlassian OAuth não configurado (ATLASSIAN_OAUTH_CLIENT_ID/SECRET)", http.StatusServiceUnavailable)
			return
		}
		tenant, ok := TenantFromContext(r.Context())
		if !ok {
			http.Error(w, "tenant required (header X-Tenant-Slug)", http.StatusUnauthorized)
			return
		}
		var body struct {
			ReturnTo string `json:"returnTo"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		authURL, _, err := s.atlassian.StartConnect(r.Context(), tenant.ID, body.ReturnTo)
		if err != nil {
			log.Error().Err(err).Msg("atlassian: start connect")
			http.Error(w, "failed to start oauth", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"authorizeUrl": authURL})
	}
}

func (s *Server) handleAtlassianCallback() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.atlassian == nil {
			http.Error(w, "atlassian OAuth não configurado", http.StatusServiceUnavailable)
			return
		}
		state := r.URL.Query().Get("state")
		code := r.URL.Query().Get("code")
		oauthErr := r.URL.Query().Get("error")

		if oauthErr != "" {
			// Usuário negou ou Atlassian retornou erro — redireciona pra UI
			// com query ?atlassian_error=... pra exibir mensagem.
			http.Redirect(w, r, "/settings?atlassian_error="+oauthErr, http.StatusFound)
			return
		}
		if state == "" || code == "" {
			http.Error(w, "missing state/code", http.StatusBadRequest)
			return
		}

		actor := r.Header.Get("X-User-Id")
		if actor == "" {
			actor = "anonymous"
		}

		result, err := s.atlassian.CompleteConnect(r.Context(), state, code, actor)
		if err != nil {
			log.Warn().Err(err).Msg("atlassian: complete connect")
			http.Redirect(w, r, "/settings?atlassian_error=oauth_failed", http.StatusFound)
			return
		}
		// Sucesso — redireciona pra UI.
		http.Redirect(w, r, result.ReturnTo+"?atlassian=connected", http.StatusFound)
	}
}

func (s *Server) handleAtlassianStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.atlassian == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"connected": false,
				"available": false,
				"reason":    "atlassian OAuth not configured on backend",
			})
			return
		}
		tenant, ok := TenantFromContext(r.Context())
		if !ok {
			http.Error(w, "tenant required", http.StatusUnauthorized)
			return
		}

		conn, err := s.atlassian.GetConnection(r.Context(), tenant.ID)
		if errors.Is(err, atlassian.ErrNotConnected) {
			writeJSON(w, http.StatusOK, map[string]any{
				"connected": false,
				"available": true,
			})
			return
		}
		if err != nil {
			log.Error().Err(err).Msg("atlassian: get connection")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"connected":  true,
			"available":  true,
			"connection": conn,
		})
	}
}

func (s *Server) handleAtlassianDisconnect() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.atlassian == nil {
			http.Error(w, "atlassian OAuth not configured", http.StatusServiceUnavailable)
			return
		}
		tenant, ok := TenantFromContext(r.Context())
		if !ok {
			http.Error(w, "tenant required", http.StatusUnauthorized)
			return
		}
		if err := s.atlassian.Disconnect(r.Context(), tenant.ID); err != nil {
			log.Error().Err(err).Msg("atlassian: disconnect")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
