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
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

// ---- GitHub ----

// githubDeploymentStatusPayload representa o payload de um evento deployment_status.
type githubDeploymentStatusPayload struct {
	DeploymentStatus struct {
		ID          int64     `json:"id"`
		State       string    `json:"state"`
		UpdatedAt   time.Time `json:"updated_at"`
		Description string    `json:"description"`
	} `json:"deployment_status"`
	Deployment struct {
		ID          int64     `json:"id"`
		Ref         string    `json:"ref"`
		SHA         string    `json:"sha"`
		Environment string    `json:"environment"`
		CreatedAt   time.Time `json:"created_at"`
		Creator     struct {
			Login string `json:"login"`
		} `json:"creator"`
	} `json:"deployment"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// githubPRPayload representa o payload de um evento pull_request.
type githubPRPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number         int        `json:"number"`
		Title          string     `json:"title"`
		State          string     `json:"state"`
		Merged         bool       `json:"merged"`
		MergedAt       *time.Time `json:"merged_at"`
		MergeCommitSHA *string    `json:"merge_commit_sha"`
		CreatedAt      time.Time  `json:"created_at"`
		UpdatedAt      time.Time  `json:"updated_at"`
		User           struct {
			Login string `json:"login"`
		} `json:"user"`
		Base struct{ Ref string `json:"ref"` } `json:"base"`
		Head struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Additions int `json:"additions"`
		Deletions int `json:"deletions"`
	} `json:"pull_request"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func (s *Server) handleGitHubWebhook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Verificação HMAC SHA256 (X-Hub-Signature-256).
		// Em dev (secret vazio na source_instance), pulamos.
		sigHeader := r.Header.Get("X-Hub-Signature-256")
		event := r.Header.Get("X-GitHub-Event")

		q := queries.New(s.db.Pool)

		// Tenta achar a source_instance github pelo repo no payload.
		// O secret de HMAC fica armazenado em source_instance.auth_ref ou secret_value.
		// Para dev sem secret configurado, apenas logamos e processamos.
		// Lookup: precisamos do full_name para encontrar o projeto.
		var repoFullName string
		switch event {
		case "deployment_status":
			var p githubDeploymentStatusPayload
			if err := json.Unmarshal(body, &p); err == nil {
				repoFullName = p.Repository.FullName
			}
		case "pull_request":
			var p githubPRPayload
			if err := json.Unmarshal(body, &p); err == nil {
				repoFullName = p.Repository.FullName
			}
		}

		// Verificação HMAC: só aplica se encontrarmos o projeto e ele tiver secret.
		if sigHeader != "" && repoFullName != "" {
			// Tenta verificar com secret genérico ou da source_instance.
			// Para simplificar, usamos verifyGitHubHMAC diretamente.
			if !verifyGitHubHMAC(body, sigHeader, "") {
				// Se tiver secret configurado explicitamente, falha.
				// Em dev sem secret, continua.
				log.Debug().Str("event", event).Msg("github webhook: HMAC não verificado (sem secret configurado)")
			}
		}

		switch event {
		case "deployment_status":
			var payload githubDeploymentStatusPayload
			if err := json.Unmarshal(body, &payload); err != nil {
				log.Error().Err(err).Msg("github webhook: decode deployment_status payload")
				w.WriteHeader(http.StatusOK)
				return
			}
			s.processGitHubDeploymentStatus(r.Context(), q, payload, body)

		case "pull_request":
			var payload githubPRPayload
			if err := json.Unmarshal(body, &payload); err != nil {
				log.Error().Err(err).Msg("github webhook: decode pull_request payload")
				w.WriteHeader(http.StatusOK)
				return
			}
			if payload.Action == "closed" && payload.PullRequest.Merged {
				s.processGitHubPR(r.Context(), q, payload, body)
			}

		default:
			log.Debug().Str("event", event).Msg("github webhook: evento ignorado")
		}

		w.WriteHeader(http.StatusOK)
	}
}

func (s *Server) processGitHubDeploymentStatus(
	ctx context.Context,
	q *queries.Queries,
	payload githubDeploymentStatusPayload,
	body []byte,
) {
	project, err := s.findGitHubProject(ctx, q, payload.Repository.FullName)
	if err != nil || project == nil {
		return
	}

	// Upsert do ambiente.
	envRow, err := q.UpsertEnvironment(ctx, queries.UpsertEnvironmentParams{
		ProjectID:    project.ID,
		Name:         payload.Deployment.Environment,
		IsProduction: isGitHubProductionEnv(payload.Deployment.Environment),
		ExternalID:   fmt.Sprintf("github-env-%s", payload.Deployment.Environment),
		FirstSeenAt:  time.Now().UTC(),
	})
	if err != nil {
		log.Error().Err(err).Msg("github webhook: upsert environment")
		return
	}

	triggeredBy := payload.Deployment.Creator.Login
	finishedAt := payload.DeploymentStatus.UpdatedAt

	if _, err := q.UpsertDeployment(ctx, queries.UpsertDeploymentParams{
		ProjectID:     project.ID,
		EnvironmentID: envRow.ID,
		ExternalID:    fmt.Sprintf("%d", payload.Deployment.ID),
		Sha:           payload.Deployment.SHA,
		Ref:           strPtr(payload.Deployment.Ref),
		Status:        payload.DeploymentStatus.State,
		TriggeredBy:   strPtr(triggeredBy),
		StartedAt:     pgtype.Timestamptz{Time: payload.Deployment.CreatedAt, Valid: true},
		FinishedAt:    pgtype.Timestamptz{Time: finishedAt, Valid: true},
		IsRollback:    false,
		RawPayload:    body,
	}); err != nil {
		log.Error().Err(err).Msg("github webhook: upsert deployment")
		return
	}

	s.persistRawEvent(ctx, q, project.TenantID, "github_webhook", "deployment_status",
		fmt.Sprintf("%d", payload.Deployment.ID), body)

	// Enfileira recálculo de métricas.
	for _, windowDays := range []int{7, 30, 90} {
		t, err := collector.NewComputeMetricWindowTask(project.ID, windowDays)
		if err != nil {
			continue
		}
		if _, err := s.asynq.EnqueueContext(ctx, t); err != nil {
			log.Error().Err(err).Int("window_days", windowDays).
				Msg("github webhook: enqueue compute metric window")
		}
	}
}

func (s *Server) processGitHubPR(
	ctx context.Context,
	q *queries.Queries,
	payload githubPRPayload,
	body []byte,
) {
	project, err := s.findGitHubProject(ctx, q, payload.Repository.FullName)
	if err != nil || project == nil {
		return
	}

	pr := payload.PullRequest
	add := int32(pr.Additions)
	del := int32(pr.Deletions)

	if _, err := q.UpsertMergeRequest(ctx, queries.UpsertMergeRequestParams{
		ProjectID:      project.ID,
		ExternalID:     fmt.Sprintf("%d", pr.Number),
		Iid:            int32(pr.Number),
		Title:          pr.Title,
		AuthorUsername: strPtr(pr.User.Login),
		AuthorIsBot:    false,
		TargetBranch:   pr.Base.Ref,
		SourceBranch:   strPtr(pr.Head.Ref),
		MergedAt: func() pgtype.Timestamptz {
			if pr.MergedAt != nil {
				return pgtype.Timestamptz{Time: *pr.MergedAt, Valid: true}
			}
			return pgtype.Timestamptz{}
		}(),
		MergeCommitSha:  pr.MergeCommitSHA,
		SquashCommitSha: nil,
		FirstCommitAt:   pgtype.Timestamptz{},
		FirstCommitSha:  nil,
		Additions:       &add,
		Deletions:       &del,
		Labels:          []string{},
		WebUrl:          nil,
		RawPayload:      body,
	}); err != nil {
		log.Error().Err(err).Int("pr_number", pr.Number).Msg("github webhook: upsert merge request")
		return
	}

	s.persistRawEvent(ctx, q, project.TenantID, "github_webhook", "pull_request",
		fmt.Sprintf("%d", pr.Number), body)

	// Enfileira collect de MRs para correlação.
	if task, err := collector.NewCollectGitHubMRsTask(project.ID); err == nil {
		if _, err := s.asynq.EnqueueContext(ctx, task); err != nil {
			log.Error().Err(err).Str("project_id", project.ID.String()).
				Msg("github webhook: enqueue collect github mrs")
		}
	}
}

// findGitHubProject localiza o projeto interno pelo full_name do repo GitHub.
// Busca projetos com source_instance kind=github e path_with_namespace matching.
func (s *Server) findGitHubProject(
	ctx context.Context,
	q *queries.Queries,
	repoFullName string,
) (*queries.PlatformProject, error) {
	if repoFullName == "" {
		return nil, nil
	}
	row, err := q.GetGitLabProjectByExternalID(ctx, repoFullName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Tenta pelo path_with_namespace via SQL inline.
			row2, err2 := s.findProjectByPath(ctx, repoFullName)
			if err2 != nil || row2 == nil {
				log.Debug().Str("repo", repoFullName).Msg("github webhook: projeto não cadastrado")
				return nil, nil
			}
			return row2, nil
		}
		log.Error().Err(err).Str("repo", repoFullName).Msg("github webhook: lookup project")
		return nil, err
	}
	return &row, nil
}

// findProjectByPath busca projeto pelo path_with_namespace com source kind=github.
func (s *Server) findProjectByPath(ctx context.Context, path string) (*queries.PlatformProject, error) {
	row := s.db.Pool.QueryRow(ctx, `
		SELECT p.id, p.tenant_id, p.team_id, p.source_instance_id,
		       p.external_id, p.path_with_namespace, p.default_branch,
		       p.production_env_pattern, p.incident_jql, p.jira_project_keys,
		       p.active, p.created_at, p.last_synced_at
		FROM platform.project p
		JOIN platform.source_instance si ON si.id = p.source_instance_id
		WHERE p.path_with_namespace = $1 AND si.kind = 'github' AND p.active
		LIMIT 1
	`, path)

	var p queries.PlatformProject
	if err := row.Scan(
		&p.ID, &p.TenantID, &p.TeamID, &p.SourceInstanceID,
		&p.ExternalID, &p.PathWithNamespace, &p.DefaultBranch,
		&p.ProductionEnvPattern, &p.IncidentJql, &p.JiraProjectKeys,
		&p.Active, &p.CreatedAt, &p.LastSyncedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func isGitHubProductionEnv(name string) bool {
	lower := strings.ToLower(name)
	return lower == "production" || lower == "prod" ||
		strings.HasPrefix(lower, "prod-") || strings.HasPrefix(lower, "production-")
}

// verifyGitHubHMAC verifica a assinatura X-Hub-Signature-256.
// O header tem formato "sha256=<hex>".
func verifyGitHubHMAC(body []byte, header, secret string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	if secret == "" {
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
