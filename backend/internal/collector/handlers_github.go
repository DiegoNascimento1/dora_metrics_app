package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	githubcollector "github.com/dora-metrics-app/backend/internal/collector/github"
	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

// HandleCollectGitHubDeployments coleta deployments do GitHub para o projeto.
func (h *Handlers) HandleCollectGitHubDeployments(ctx context.Context, task *asynq.Task) error {
	var payload CollectGitHubPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w (%w)", err, asynq.SkipRetry)
	}

	q := queries.New(h.DB.Pool)

	project, err := q.GetProject(ctx, payload.ProjectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Warn().Str("project_id", payload.ProjectID.String()).Msg("project not found; skipping")
			return nil
		}
		return fmt.Errorf("load project: %w", err)
	}

	instance, err := q.GetSourceInstance(ctx, project.SourceInstanceID)
	if err != nil {
		return fmt.Errorf("load source instance: %w", err)
	}

	if instance.Kind != "github" {
		log.Warn().
			Str("project_id", project.ID.String()).
			Str("source_kind", instance.Kind).
			Msg("source is not github; skipping HandleCollectGitHubDeployments")
		return nil
	}

	token, err := resolveToken(ctx, h.Secret, instance)
	if err != nil {
		return fmt.Errorf("resolve github token: %w", err)
	}

	owner, repo, err := parseGitHubRepo(project.PathWithNamespace)
	if err != nil {
		return fmt.Errorf("parse path_with_namespace %q: %w (%w)", project.PathWithNamespace, err, asynq.SkipRetry)
	}

	client := githubcollector.NewClient(token)

	// Janela de coleta (mesma lógica que o coletor GitLab).
	since := time.Now().Add(-30 * 24 * time.Hour)
	if project.LastSyncedAt.Valid {
		since = project.LastSyncedAt.Time.Add(-1 * time.Hour)
	}
	if payload.BackfillDays > 0 {
		forced := time.Now().Add(-time.Duration(payload.BackfillDays) * 24 * time.Hour)
		if forced.Before(since) {
			since = forced
		}
	}

	deployments, err := client.ListDeployments(ctx, owner, repo, since)
	if err != nil {
		return fmt.Errorf("list github deployments: %w", err)
	}

	for _, d := range deployments {
		// Busca statuses para determinar o status final do deployment.
		statuses, statusErr := client.GetDeploymentStatuses(ctx, owner, repo, d.ID)
		finalStatus := "pending"
		if statusErr == nil && len(statuses) > 0 {
			finalStatus = statuses[0].State // mais recente primeiro
		}

		// Faz upsert do ambiente (environment) usando o nome do environment do GitHub.
		envRow, envErr := q.UpsertEnvironment(ctx, queries.UpsertEnvironmentParams{
			ProjectID:    project.ID,
			Name:         d.Environment,
			IsProduction: isProductionEnv(d.Environment),
			ExternalID:   fmt.Sprintf("github-env-%s", d.Environment),
			FirstSeenAt:  time.Now().UTC(),
		})
		if envErr != nil {
			return fmt.Errorf("upsert environment %q: %w", d.Environment, envErr)
		}

		raw, _ := json.Marshal(d)

		triggeredBy := &d.Creator.Login

		var finishedAt *time.Time
		if finalStatus == "success" || finalStatus == "failure" || finalStatus == "error" {
			if len(statuses) > 0 {
				t := statuses[0].UpdatedAt
				finishedAt = &t
			}
		}

		if _, err := q.UpsertDeployment(ctx, queries.UpsertDeploymentParams{
			ProjectID:     project.ID,
			EnvironmentID: envRow.ID,
			ExternalID:    fmt.Sprintf("%d", d.ID),
			Sha:           d.SHA,
			Ref:           strPtr(d.Ref),
			Status:        finalStatus,
			TriggeredBy:   triggeredBy,
			StartedAt:     pgTimePtr(&d.CreatedAt),
			FinishedAt:    pgTimePtr(finishedAt),
			IsRollback:    false,
			RawPayload:    raw,
		}); err != nil {
			return fmt.Errorf("upsert deployment %d: %w", d.ID, err)
		}
	}

	log.Info().
		Str("project_id", project.ID.String()).
		Str("repo", owner+"/"+repo).
		Int("deployments", len(deployments)).
		Msg("github deployments collect complete")

	for _, windowDays := range h.Windows {
		t, err := NewComputeMetricWindowTask(project.ID, windowDays)
		if err != nil {
			log.Error().Err(err).Int("window_days", windowDays).Msg("build compute task")
			continue
		}
		if _, err := h.Asynq.EnqueueContext(ctx, t); err != nil {
			log.Error().Err(err).Int("window_days", windowDays).Msg("enqueue compute task")
		}
	}
	return nil
}

// HandleCollectGitHubMRs coleta pull requests merged do GitHub para o projeto.
func (h *Handlers) HandleCollectGitHubMRs(ctx context.Context, task *asynq.Task) error {
	var payload CollectGitHubPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w (%w)", err, asynq.SkipRetry)
	}

	q := queries.New(h.DB.Pool)

	project, err := q.GetProject(ctx, payload.ProjectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load project: %w", err)
	}

	instance, err := q.GetSourceInstance(ctx, project.SourceInstanceID)
	if err != nil {
		return fmt.Errorf("load source instance: %w", err)
	}

	if instance.Kind != "github" {
		return nil
	}

	token, err := resolveToken(ctx, h.Secret, instance)
	if err != nil {
		return fmt.Errorf("resolve github token: %w", err)
	}

	owner, repo, err := parseGitHubRepo(project.PathWithNamespace)
	if err != nil {
		return fmt.Errorf("parse path_with_namespace %q: %w (%w)", project.PathWithNamespace, err, asynq.SkipRetry)
	}

	client := githubcollector.NewClient(token)

	since := time.Now().Add(-30 * 24 * time.Hour)
	if project.LastSyncedAt.Valid {
		since = project.LastSyncedAt.Time.Add(-1 * time.Hour)
	}
	if payload.BackfillDays > 0 {
		forced := time.Now().Add(-time.Duration(payload.BackfillDays) * 24 * time.Hour)
		if forced.Before(since) {
			since = forced
		}
	}

	prs, err := client.ListPullRequests(ctx, owner, repo, since)
	if err != nil {
		return fmt.Errorf("list github pull requests: %w", err)
	}

	for i, pr := range prs {
		if pr.MergedAt == nil {
			continue
		}

		// Tenta buscar o first_commit_at via GetCommit do HEAD SHA.
		var firstCommitAt *time.Time
		var firstCommitSHA string
		if pr.Head.SHA != "" {
			if commit, cErr := client.GetCommit(ctx, owner, repo, pr.Head.SHA); cErr == nil {
				t := commit.Commit.Author.Date
				firstCommitAt = &t
				firstCommitSHA = commit.SHA
			}
		}

		raw, _ := json.Marshal(pr)

		add := int32(pr.Additions)
		del := int32(pr.Deletions)

		if _, err := q.UpsertMergeRequest(ctx, queries.UpsertMergeRequestParams{
			ProjectID:       project.ID,
			ExternalID:      fmt.Sprintf("%d", pr.Number),
			Iid:             int32(pr.Number),
			Title:           pr.Title,
			AuthorUsername:  strPtr(pr.User.Login),
			AuthorIsBot:     false,
			TargetBranch:    pr.Base.Ref,
			SourceBranch:    strPtr(pr.Head.Ref),
			MergedAt:        pgTimePtr(pr.MergedAt),
			MergeCommitSha:  pr.MergeCommitSHA,
			SquashCommitSha: nil,
			FirstCommitAt:   pgTimePtr(firstCommitAt),
			FirstCommitSha:  strPtr(firstCommitSHA),
			Additions:       &add,
			Deletions:       &del,
			Labels:          []string{},
			WebUrl:          nil,
			RawPayload:      raw,
		}); err != nil {
			log.Error().Err(err).Int("pr_number", pr.Number).Msg("upsert github pull request")
		}
		_ = i
	}

	log.Info().
		Str("project_id", project.ID.String()).
		Str("repo", owner+"/"+repo).
		Int("pull_requests", len(prs)).
		Msg("github MRs collect complete")

	return nil
}

// parseGitHubRepo extrai owner e repo de path_with_namespace ("owner/repo").
func parseGitHubRepo(path string) (owner, repo string, err error) {
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("formato inválido %q (esperado 'owner/repo')", path)
	}
	return parts[0], parts[1], nil
}

// isProductionEnv classifica ambientes com nome "production" ou "prod" como produção.
func isProductionEnv(name string) bool {
	lower := strings.ToLower(name)
	return lower == "production" || lower == "prod" ||
		strings.HasPrefix(lower, "prod-") || strings.HasPrefix(lower, "production-")
}

// Garante que os handlers GitHub ficam registrados no ServeMux.
func (h *Handlers) registerGitHubHandlers(mux *asynq.ServeMux) {
	mux.HandleFunc(TaskCollectGitHubDeployments, h.HandleCollectGitHubDeployments)
	mux.HandleFunc(TaskCollectGitHubMRs, h.HandleCollectGitHubMRs)
}

// strPtrGH cria ponteiro para string não-vazia (reutiliza o strPtr do handlers.go).
func strPtrGH(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// uuidToStr converte uuid.UUID para string.
func uuidToStr(id uuid.UUID) string {
	return id.String()
}
