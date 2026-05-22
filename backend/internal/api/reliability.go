// Endpoint REST que lista SLOs do provider configurado.
//
// GET /api/v1/reliability/slos?scope=service:dora-api
//   ?scope é opaco — cada provider interpreta de seu jeito:
//     datadog → tags_query
//     sentry → project slug
//     prometheus → label filter ('service="api"')
//     yaml → service
//   Vazio = todos os SLOs.
//
// GET /api/v1/reliability/info
//   Devolve { provider, configured } pro frontend saber se mostra UI.
package api

import (
	"net/http"

	"github.com/rs/zerolog/log"
)

func (s *Server) handleReliabilityInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"provider":   s.reliability.Name(),
			"configured": s.reliability.Name() != "noop",
		})
	}
}

func (s *Server) handleReliabilitySLOs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope := r.URL.Query().Get("scope")
		slos, err := s.reliability.ListSLOs(r.Context(), scope)
		if err != nil {
			log.Error().Err(err).
				Str("provider", s.reliability.Name()).
				Str("scope", scope).
				Msg("reliability list SLOs falhou")
			http.Error(w, "failed to list SLOs from provider", http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"provider": s.reliability.Name(),
			"scope":    scope,
			"slos":     slos,
		})
	}
}
