package api

import (
	"crypto/subtle"
	"io"
	"net/http"

	"github.com/rs/zerolog/log"
)

const maxWebhookBody = 10 * 1024 * 1024 // 10 MiB

func (s *Server) handleGitLabWebhook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Gitlab-Token")
		expected := s.cfg.GitLab.WebhookToken
		if expected == "" || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		event := r.Header.Get("X-Gitlab-Event")
		log.Info().
			Str("event", event).
			Int("bytes", len(body)).
			Msg("gitlab webhook received")

		// TODO Fase 1: persistir em raw_event e enfileirar job.

		w.WriteHeader(http.StatusOK)
	}
}

func (s *Server) handleJiraWebhook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO Fase 2: validar HMAC-SHA256 com s.cfg.Jira.WebhookSecret.
		body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		log.Info().Int("bytes", len(body)).Msg("jira webhook received")

		// TODO Fase 2: persistir em raw_event e enfileirar job.

		w.WriteHeader(http.StatusOK)
	}
}
