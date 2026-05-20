package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/calculator"
	"github.com/dora-metrics-app/backend/internal/collector/gitlab"
	"github.com/dora-metrics-app/backend/internal/secret"
	"github.com/dora-metrics-app/backend/internal/storage"
	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

// Handlers agrega as dependências usadas pelos handlers de task asynq.
type Handlers struct {
	DB      *storage.Pool
	Secret  secret.Provider
	Asynq   *asynq.Client
	Windows []int // janelas em dias que recalculamos (ex: [7, 30, 90])
}

// Register associa os handlers a um asynq.ServeMux.
func (h *Handlers) Register(mux *asynq.ServeMux) {
	mux.HandleFunc(TaskScanActiveProjects, h.HandleScanActiveProjects)
	mux.HandleFunc(TaskCollectGitlab, h.HandleCollectGitlab)
	mux.HandleFunc(TaskComputeMetricWindow, h.HandleComputeMetricWindow)
}

// HandleScanActiveProjects roda no tick periódico e enfileira uma task
// de coleta por projeto ativo.
func (h *Handlers) HandleScanActiveProjects(ctx context.Context, _ *asynq.Task) error {
	q := queries.New(h.DB.Pool)

	projects, err := q.ListActiveProjects(ctx)
	if err != nil {
		return fmt.Errorf("list active projects: %w", err)
	}

	for _, p := range projects {
		task, err := NewCollectGitlabTask(p.ID)
		if err != nil {
			log.Error().Err(err).Str("project_id", p.ID.String()).Msg("build collect task")
			continue
		}
		info, err := h.Asynq.EnqueueContext(ctx, task)
		if err != nil {
			log.Error().Err(err).Str("project_id", p.ID.String()).Msg("enqueue collect task")
			continue
		}
		log.Info().
			Str("project_id", p.ID.String()).
			Str("path", p.PathWithNamespace).
			Str("task_id", info.ID).
			Msg("enqueued collect:gitlab:deployments")
	}
	return nil
}

// HandleCollectGitlab coleta environments e deployments do GitLab
// para um projeto.
func (h *Handlers) HandleCollectGitlab(ctx context.Context, task *asynq.Task) error {
	var payload CollectGitlabPayload
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

	if instance.Kind != "gitlab" {
		log.Warn().
			Str("project_id", project.ID.String()).
			Str("source_kind", instance.Kind).
			Msg("source is not gitlab; skipping")
		return nil
	}

	token, err := h.Secret.Get(ctx, instance.AuthRef)
	if err != nil {
		return fmt.Errorf("resolve token (auth_ref=%s): %w", instance.AuthRef, err)
	}

	client := gitlab.NewClient(instance.BaseUrl, token)

	prodRE, err := regexp.Compile(project.ProductionEnvPattern)
	if err != nil {
		return fmt.Errorf("invalid production_env_pattern %q: %w (%w)",
			project.ProductionEnvPattern, err, asynq.SkipRetry)
	}

	envs, err := client.ListEnvironments(ctx, project.ExternalID)
	if err != nil {
		return fmt.Errorf("list environments: %w", err)
	}

	envIDByExternal := make(map[int]uuid.UUID)
	for _, e := range envs {
		isProd := prodRE.MatchString(e.Name)
		row, upsertErr := q.UpsertEnvironment(ctx, queries.UpsertEnvironmentParams{
			ProjectID:    project.ID,
			Name:         e.Name,
			IsProduction: isProd,
			ExternalID:   fmt.Sprintf("%d", e.ID),
			FirstSeenAt:  time.Now().UTC(),
		})
		if upsertErr != nil {
			return fmt.Errorf("upsert env %s: %w", e.Name, upsertErr)
		}
		envIDByExternal[e.ID] = row.ID
	}

	// Backfill inicial = 30d. Depois incremental a partir de last_synced_at - 1h
	// (overlap defensivo para cobrir eventos no exato segundo da última sync).
	since := time.Now().Add(-30 * 24 * time.Hour)
	if project.LastSyncedAt.Valid {
		since = project.LastSyncedAt.Time.Add(-1 * time.Hour)
	}

	deployments, err := client.ListDeployments(ctx, project.ExternalID, gitlab.ListDeploymentsOpts{
		UpdatedAfter: since,
		PerPage:      100,
	})
	if err != nil {
		return fmt.Errorf("list deployments: %w", err)
	}

	for _, d := range deployments {
		envID, ok := envIDByExternal[d.Environment.ID]
		if !ok {
			isProd := prodRE.MatchString(d.Environment.Name)
			row, upsertErr := q.UpsertEnvironment(ctx, queries.UpsertEnvironmentParams{
				ProjectID:    project.ID,
				Name:         d.Environment.Name,
				IsProduction: isProd,
				ExternalID:   fmt.Sprintf("%d", d.Environment.ID),
				FirstSeenAt:  time.Now().UTC(),
			})
			if upsertErr != nil {
				return fmt.Errorf("upsert env (lazy) %s: %w", d.Environment.Name, upsertErr)
			}
			envID = row.ID
			envIDByExternal[d.Environment.ID] = envID
		}

		raw, _ := json.Marshal(d)

		_, err := q.UpsertDeployment(ctx, queries.UpsertDeploymentParams{
			ProjectID:     project.ID,
			EnvironmentID: envID,
			ExternalID:    fmt.Sprintf("%d", d.ID),
			Sha:           d.SHA,
			Ref:           strPtr(d.Ref),
			Status:        d.Status,
			TriggeredBy:   triggeredBy(d),
			StartedAt:     pgTimePtr(deployableStarted(d)),
			FinishedAt:    pgTimePtr(d.FinishedAt),
			IsRollback:    false,
			RawPayload:    raw,
		})
		if err != nil {
			return fmt.Errorf("upsert deployment %d: %w", d.ID, err)
		}
	}

	if err := q.UpdateProjectLastSynced(ctx, queries.UpdateProjectLastSyncedParams{
		ID:           project.ID,
		LastSyncedAt: pgTime(time.Now().UTC()),
	}); err != nil {
		return fmt.Errorf("update last_synced_at: %w", err)
	}

	log.Info().
		Str("project_id", project.ID.String()).
		Str("path", project.PathWithNamespace).
		Int("environments", len(envs)).
		Int("deployments", len(deployments)).
		Msg("gitlab collect complete")

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

// HandleComputeMetricWindow recalcula a janela rolante (DF na Fase 1)
// e grava em metrics.metric_window.
func (h *Handlers) HandleComputeMetricWindow(ctx context.Context, task *asynq.Task) error {
	var payload ComputeMetricWindowPayload
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

	since := time.Now().Add(-time.Duration(payload.WindowDays) * 24 * time.Hour)

	count, err := q.CountSuccessfulProductionDeploymentsInWindow(ctx,
		queries.CountSuccessfulProductionDeploymentsInWindowParams{
			ProjectID:  project.ID,
			FinishedAt: pgTime(since),
		},
	)
	if err != nil {
		return fmt.Errorf("count deploys: %w", err)
	}

	df := float64(count) / float64(payload.WindowDays)
	classification := calculator.ClassifyDeploymentFrequency(df)

	dfNumeric, err := numericFromFloat(df)
	if err != nil {
		return fmt.Errorf("convert df: %w", err)
	}

	_, err = q.UpsertMetricWindow(ctx, queries.UpsertMetricWindowParams{
		TenantID:            project.TenantID,
		ScopeKind:           "project",
		ScopeID:             project.ID,
		WindowDays:          int32(payload.WindowDays),
		ComputedAt:          time.Now().UTC(),
		DeploymentFrequency: dfNumeric,
		LeadTimeMedianS:     nil,
		ChangeFailureRate:   pgtype.Numeric{}, // NULL na Fase 1
		MttrMeanS:           nil,
		Classification:      strPtr(classification),
		SampleSize:          int32(count),
	})
	if err != nil {
		return fmt.Errorf("upsert metric window: %w", err)
	}

	log.Info().
		Str("project_id", project.ID.String()).
		Int("window_days", payload.WindowDays).
		Int64("sample", count).
		Float64("df_per_day", df).
		Str("class", classification).
		Msg("metric window updated")

	return nil
}

// ---- helpers ----

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func pgTimePtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func triggeredBy(d gitlab.Deployment) *string {
	if d.Deployable != nil && d.Deployable.User != nil {
		return strPtr(d.Deployable.User.Username)
	}
	if d.User != nil {
		return strPtr(d.User.Username)
	}
	return nil
}

func deployableStarted(d gitlab.Deployment) *time.Time {
	if d.Deployable != nil && d.Deployable.StartedAt != nil {
		return d.Deployable.StartedAt
	}
	return nil
}

func numericFromFloat(f float64) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if err := n.Scan(fmt.Sprintf("%.6f", f)); err != nil {
		return pgtype.Numeric{}, err
	}
	return n, nil
}
