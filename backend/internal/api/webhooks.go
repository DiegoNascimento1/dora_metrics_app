package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/collector"
	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

const maxWebhookBody = 10 * 1024 * 1024 // 10 MiB

// ---- GitLab ----

type gitlabWebhookPayload struct {
	ObjectKind string `json:"object_kind"`
	Project    struct {
		ID                int    `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
}

// Eventos GitLab que disparam um collect imediato. Outros vão só pra raw_event.
var gitlabCollectableEvents = map[string]bool{
	"Merge Request Hook": true,
	"Deployment Hook":    true,
}

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

		var payload gitlabWebhookPayload
		// Best-effort parsing: se quebrar, ainda gravamos o raw e respondemos 200
		// pra evitar retry agressivo do GitLab.
		_ = json.Unmarshal(body, &payload)

		q := queries.New(s.db.Pool)

		var project *queries.PlatformProject
		if payload.Project.ID != 0 {
			row, err := q.GetGitLabProjectByExternalID(r.Context(), fmt.Sprintf("%d", payload.Project.ID))
			if err == nil {
				project = &row
			} else if !errors.Is(err, pgx.ErrNoRows) {
				log.Error().Err(err).Msg("lookup gitlab project")
			}
		}

		// Audit trail mesmo se projeto não cadastrado, desde que tenhamos
		// algum tenant para atribuir o evento. Sem projeto = sem tenant
		// inferível, então só logamos.
		if project != nil {
			s.persistRawEvent(r.Context(), q, project.TenantID, "gitlab_webhook", event,
				fmt.Sprintf("%d", payload.Project.ID), body)
		}

		if project != nil && gitlabCollectableEvents[event] {
			s.enqueueGitlabCollect(r.Context(), project, event, payload.Project.PathWithNamespace)
		} else {
			log.Debug().
				Str("event", event).
				Int("gitlab_project_id", payload.Project.ID).
				Bool("known_project", project != nil).
				Msg("gitlab webhook noop")
		}

		w.WriteHeader(http.StatusOK)
	}
}

func (s *Server) enqueueGitlabCollect(ctx context.Context, project *queries.PlatformProject, event, projectPath string) {
	task, err := collector.NewCollectGitlabTask(project.ID)
	if err != nil {
		log.Error().Err(err).Msg("build gitlab collect task from webhook")
		return
	}
	info, err := s.asynq.EnqueueContext(ctx, task)
	if err != nil {
		log.Error().Err(err).
			Str("project_id", project.ID.String()).
			Msg("enqueue collect from gitlab webhook")
		return
	}
	log.Info().
		Str("event", event).
		Str("project", projectPath).
		Str("task_id", info.ID).
		Msg("gitlab webhook -> collect:gitlab:deployments enqueued")
}

// ---- Jira ----

type jiraWebhookPayload struct {
	WebhookEvent string `json:"webhookEvent"`
	Issue        struct {
		Key    string `json:"key"`
		Fields struct {
			Project struct {
				Key string `json:"key"`
			} `json:"project"`
		} `json:"fields"`
	} `json:"issue"`
}

func (s *Server) handleJiraWebhook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// HMAC-SHA256 sobre o body com s.cfg.Jira.WebhookSecret.
		// Em dev (secret vazio), pulamos a verificação.
		if s.cfg.Jira.WebhookSecret != "" {
			sig := r.Header.Get("X-Hub-Signature")
			if !verifyJiraHMAC(body, sig, s.cfg.Jira.WebhookSecret) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		var payload jiraWebhookPayload
		_ = json.Unmarshal(body, &payload)

		jiraProjectKey := payload.Issue.Fields.Project.Key
		if jiraProjectKey == "" {
			log.Debug().Str("event", payload.WebhookEvent).
				Msg("jira webhook with no project key; ignored")
			w.WriteHeader(http.StatusOK)
			return
		}

		q := queries.New(s.db.Pool)
		projects, err := q.ListActiveProjectsByJiraProjectKey(r.Context(), jiraProjectKey)
		if err != nil {
			log.Error().Err(err).Msg("lookup projects by jira project key")
			w.WriteHeader(http.StatusOK)
			return
		}

		for _, p := range projects {
			s.persistRawEvent(r.Context(), q, p.TenantID, "jira_webhook",
				payload.WebhookEvent, payload.Issue.Key, body)

			task, err := collector.NewCollectJiraTask(p.ID)
			if err != nil {
				continue
			}
			if _, err := s.asynq.EnqueueContext(r.Context(), task); err != nil {
				log.Error().Err(err).
					Str("project_id", p.ID.String()).
					Msg("enqueue jira collect from webhook")
				continue
			}
			log.Info().
				Str("event", payload.WebhookEvent).
				Str("jira_project", jiraProjectKey).
				Str("project", p.PathWithNamespace).
				Msg("jira webhook -> collect:jira:incidents enqueued")
		}

		w.WriteHeader(http.StatusOK)
	}
}

// ---- helpers ----

func (s *Server) persistRawEvent(
	ctx context.Context, q *queries.Queries, tenantID uuid.UUID,
	source, kind, extID string, body []byte,
) {
	if _, err := q.InsertRawEvent(ctx, queries.InsertRawEventParams{
		TenantID:   tenantID,
		Source:     source,
		Kind:       kind,
		ExternalID: strPtr(extID),
		Payload:    body,
	}); err != nil {
		log.Error().Err(err).Str("source", source).Msg("persist raw_event")
	}
}

func verifyJiraHMAC(body []byte, header, secret string) bool {
	prefix := "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := mac.Sum(nil)
	return hmac.Equal(got, want)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
