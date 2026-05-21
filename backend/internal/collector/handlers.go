package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/alerts"
	"github.com/dora-metrics-app/backend/internal/calculator"
	"github.com/dora-metrics-app/backend/internal/collector/gitlab"
	"github.com/dora-metrics-app/backend/internal/collector/jira"
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
	mux.HandleFunc(TaskReconcileAllProjects, h.HandleReconcileAll)
	mux.HandleFunc(TaskCollectGitlab, h.HandleCollectGitlab)
	mux.HandleFunc(TaskCollectJira, h.HandleCollectJira)
	mux.HandleFunc(TaskComputeMetricWindow, h.HandleComputeMetricWindow)
	mux.HandleFunc(TaskSnapshotMonthly, h.HandleSnapshotMonthly)
	mux.HandleFunc(TaskDispatchAlert, h.HandleDispatchAlert)
}

// ReconcileBackfillDays é a profundidade da varredura que o job noturno força
// (cobre janelas de webhook perdidas e curtos períodos de indisponibilidade).
const ReconcileBackfillDays = 7

// HandleReconcileAll: tick noturno que enfileira collect:gitlab e collect:jira
// para todos os projetos ativos com BackfillDays=ReconcileBackfillDays.
// Garante captura de eventos que webhooks/scheduler regular tenham deixado escapar.
func (h *Handlers) HandleReconcileAll(ctx context.Context, _ *asynq.Task) error {
	q := queries.New(h.DB.Pool)

	projects, err := q.ListActiveProjects(ctx)
	if err != nil {
		return fmt.Errorf("list active projects: %w", err)
	}

	for _, p := range projects {
		if task, err := NewCollectGitlabTaskWithBackfill(p.ID, ReconcileBackfillDays); err == nil {
			if _, err := h.Asynq.EnqueueContext(ctx, task); err != nil {
				log.Error().Err(err).Str("project_id", p.ID.String()).
					Msg("reconcile: enqueue gitlab")
			}
		}
		if len(p.JiraProjectKeys) > 0 {
			if task, err := NewCollectJiraTaskWithBackfill(p.ID, ReconcileBackfillDays); err == nil {
				if _, err := h.Asynq.EnqueueContext(ctx, task); err != nil {
					log.Error().Err(err).Str("project_id", p.ID.String()).
						Msg("reconcile: enqueue jira")
				}
			}
		}
	}

	log.Info().Int("projects", len(projects)).
		Int("backfill_days", ReconcileBackfillDays).
		Msg("reconcile:projects fan-out complete")

	return nil
}

// IncidentLinkLookback é a janela usada para atribuir um incident ao deploy
// que o "causou". Padrão DORA: 24h. Configurável por projeto no futuro.
const IncidentLinkLookback = 24 * time.Hour

// HandleScanActiveProjects roda no tick periódico e enfileira uma task
// de coleta por projeto ativo.
func (h *Handlers) HandleScanActiveProjects(ctx context.Context, _ *asynq.Task) error {
	q := queries.New(h.DB.Pool)

	projects, err := q.ListActiveProjects(ctx)
	if err != nil {
		return fmt.Errorf("list active projects: %w", err)
	}

	for _, p := range projects {
		// GitLab: sempre enfileira (toda fonte de deployments).
		if task, err := NewCollectGitlabTask(p.ID); err != nil {
			log.Error().Err(err).Str("project_id", p.ID.String()).Msg("build gitlab collect task")
		} else if info, err := h.Asynq.EnqueueContext(ctx, task); err != nil {
			log.Error().Err(err).Str("project_id", p.ID.String()).Msg("enqueue gitlab collect")
		} else {
			log.Info().
				Str("project_id", p.ID.String()).
				Str("path", p.PathWithNamespace).
				Str("task_id", info.ID).
				Msg("enqueued collect:gitlab:deployments")
		}

		// Jira: enfileira só se o projeto tiver jira_project_keys configurado.
		if len(p.JiraProjectKeys) > 0 {
			if task, err := NewCollectJiraTask(p.ID); err != nil {
				log.Error().Err(err).Str("project_id", p.ID.String()).Msg("build jira collect task")
			} else if info, err := h.Asynq.EnqueueContext(ctx, task); err != nil {
				log.Error().Err(err).Str("project_id", p.ID.String()).Msg("enqueue jira collect")
			} else {
				log.Info().
					Str("project_id", p.ID.String()).
					Str("path", p.PathWithNamespace).
					Str("task_id", info.ID).
					Msg("enqueued collect:jira:incidents")
			}
		}
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

	token, err := resolveToken(ctx, h.Secret, instance)
	if err != nil {
		return fmt.Errorf("resolve gitlab token: %w", err)
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

	// Janela de coleta:
	//   - sem last_synced_at        -> backfill 30d (primeira vez)
	//   - com last_synced_at        -> incremental: last_synced_at - 1h (overlap)
	//   - BackfillDays > 0 (recon.) -> força N dias (cobre webhooks/janelas perdidas)
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

	// 3) Sincroniza merge requests (state=merged) na mesma janela.
	mrs, err := client.ListMergeRequests(ctx, project.ExternalID, gitlab.ListMergeRequestsOpts{
		State:        "merged",
		TargetBranch: project.DefaultBranch,
		UpdatedAfter: since,
		PerPage:      100,
	})
	if err != nil {
		return fmt.Errorf("list merge requests: %w", err)
	}

	storedMRs := make(map[string]queries.PlatformMergeRequest, len(mrs))
	for _, m := range mrs {
		firstCommitAt, firstCommitSHA := firstCommit(ctx, client, project.ExternalID, m)

		raw, _ := json.Marshal(m)

		row, err := q.UpsertMergeRequest(ctx, queries.UpsertMergeRequestParams{
			ProjectID:       project.ID,
			ExternalID:      fmt.Sprintf("%d", m.ID),
			Iid:             int32(m.IID),
			Title:           m.Title,
			AuthorUsername:  authorUsername(m),
			AuthorIsBot:     isBotAuthor(m),
			TargetBranch:    m.TargetBranch,
			SourceBranch:    strPtr(m.SourceBranch),
			MergedAt:        pgTimePtr(m.MergedAt),
			MergeCommitSha:  m.MergeCommitSHA,
			SquashCommitSha: m.SquashCommitSHA,
			FirstCommitAt:   pgTimePtr(firstCommitAt),
			FirstCommitSha:  strPtr(firstCommitSHA),
			Additions:       toInt32Ptr(m.Additions),
			Deletions:       toInt32Ptr(m.Deletions),
			Labels:          m.Labels,
			WebUrl:          strPtr(m.WebURL),
			RawPayload:      raw,
		})
		if err != nil {
			return fmt.Errorf("upsert merge request %d: %w", m.ID, err)
		}
		storedMRs[fmt.Sprintf("%d", m.ID)] = row
	}

	// 4) Correlaciona MRs ↔ deployments por janela temporal (estratégia
	//    descrita em docs/06-data-model.md): cada deployment de produção
	//    "fecha" o intervalo desde o deployment anterior do mesmo projeto;
	//    todos os MRs merged nesse intervalo são atribuídos a ele.
	if err := h.correlateDeploymentsAndMRs(ctx, q, project); err != nil {
		return fmt.Errorf("correlate deployments and MRs: %w", err)
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
		Int("merge_requests", len(mrs)).
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

	// Captura a classificação anterior ANTES de gravar a nova — usada pelo
	// detector de alertas. Não falhamos a task se a leitura der erro: o pior
	// caso é não disparar este alerta (idempotente na próxima compute).
	var previousTier string
	if prev, prevErr := q.GetLatestMetricWindow(ctx, queries.GetLatestMetricWindowParams{
		TenantID:   project.TenantID,
		ScopeKind:  "project",
		ScopeID:    project.ID,
		WindowDays: int32(payload.WindowDays),
	}); prevErr == nil {
		if prev.Classification != nil {
			previousTier = *prev.Classification
		}
	} else if !errors.Is(prevErr, pgx.ErrNoRows) {
		log.Warn().Err(prevErr).Msg("alerts: load previous metric_window")
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

	// Lead Time mediano via PERCENTILE_CONT na mesma janela.
	ltRow, err := q.LeadTimeMedianSecondsInWindow(ctx, queries.LeadTimeMedianSecondsInWindowParams{
		ProjectID:  project.ID,
		FinishedAt: pgTime(since),
	})
	if err != nil {
		return fmt.Errorf("compute lead time median: %w", err)
	}
	var leadTimeMedianS *int64
	if ltRow.SampleSize > 0 {
		v := int64(coerceFloat(ltRow.MedianSeconds))
		leadTimeMedianS = &v
	}

	// CFR = (deploys com >= 1 incident vinculado) / total
	cfrRow, err := q.ChangeFailureRateInWindow(ctx, queries.ChangeFailureRateInWindowParams{
		ProjectID:     project.ID,
		FinishedSince: pgTime(since),
	})
	if err != nil {
		return fmt.Errorf("compute cfr: %w", err)
	}
	var cfrNumeric pgtype.Numeric
	var cfrFloat *float64
	if cfrRow.SampleSize > 0 {
		v := coerceFloat(cfrRow.Cfr)
		cfrFloat = &v
		cfrNumeric, _ = numericFromFloat(v)
	}

	// MTTR = média (segundos) de (resolved_at - created_at) na janela.
	mttrRow, err := q.MTTRMeanSecondsInWindow(ctx, queries.MTTRMeanSecondsInWindowParams{
		ProjectID:     project.ID,
		ResolvedSince: pgTime(since),
	})
	if err != nil {
		return fmt.Errorf("compute mttr: %w", err)
	}
	var mttrMeanS *int64
	if mttrRow.SampleSize > 0 {
		v := int64(coerceFloat(mttrRow.MeanSeconds))
		mttrMeanS = &v
	}

	thresholds := calculator.DefaultThresholds()
	if row, err := q.GetClassificationThreshold(ctx, project.TenantID); err == nil {
		if loaded, err := calculator.FromJSON(row.Config); err == nil {
			thresholds = loaded
		} else {
			log.Warn().Err(err).Str("tenant_id", project.TenantID.String()).
				Msg("invalid classification_threshold config; using defaults")
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		log.Warn().Err(err).Msg("load thresholds; using defaults")
	}

	classification := calculator.WorstOf(
		calculator.ClassifyDeploymentFrequency(df, thresholds),
		calculator.ClassifyLeadTime(leadTimeMedianS, thresholds),
		calculator.ClassifyChangeFailureRate(cfrFloat, thresholds),
		calculator.ClassifyMTTR(mttrMeanS, thresholds),
	)

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
		LeadTimeMedianS:     leadTimeMedianS,
		ChangeFailureRate:   cfrNumeric,
		MttrMeanS:           mttrMeanS,
		Classification:      strPtr(classification),
		SampleSize:          int32(count),
	})
	if err != nil {
		return fmt.Errorf("upsert metric window: %w", err)
	}

	log.Info().
		Str("project_id", project.ID.String()).
		Int("window_days", payload.WindowDays).
		Int64("deploys", count).
		Float64("df_per_day", df).
		Int64("lt_sample", ltRow.SampleSize).
		Interface("lt_median_s", leadTimeMedianS).
		Int64("cfr_sample", cfrRow.SampleSize).
		Interface("cfr", cfrFloat).
		Int64("mttr_sample", mttrRow.SampleSize).
		Interface("mttr_mean_s", mttrMeanS).
		Str("class", classification).
		Msg("metric window updated")

	h.fanOutAlertsForTierChange(ctx, q, project, payload.WindowDays, previousTier, classification)

	return nil
}

// fanOutAlertsForTierChange compara a classificação anterior com a nova e,
// se houve transição relevante, persiste alert_events em "pending" e enfileira
// uma task dispatch:alert por evento. Erros são logados mas não propagam —
// nenhum dos passos é crítico pro recálculo principal.
func (h *Handlers) fanOutAlertsForTierChange(
	ctx context.Context,
	q *queries.Queries,
	project queries.PlatformProject,
	windowDays int,
	previousTier, currentTier string,
) {
	if !alerts.IsChange(previousTier, currentTier) {
		return
	}

	rules, err := q.FindMatchingAlertRules(ctx, queries.FindMatchingAlertRulesParams{
		TenantID:   project.TenantID,
		ScopeKind:  "project",
		WindowDays: int32(windowDays),
		ScopeID:    pgtype.UUID{Bytes: project.ID, Valid: true},
	})
	if err != nil {
		log.Error().Err(err).Msg("alerts: find matching rules")
		return
	}

	for _, rule := range rules {
		if !alerts.RuleMatchesChange(rule.Kind, previousTier, currentTier) {
			continue
		}

		payload := map[string]any{
			"rule_id":       rule.ID.String(),
			"rule_name":     rule.Name,
			"kind":          rule.Kind,
			"scope_kind":    "project",
			"scope_id":      project.ID.String(),
			"project_path":  project.PathWithNamespace,
			"window_days":   windowDays,
			"previous_tier": previousTier,
			"current_tier":  currentTier,
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			log.Error().Err(err).Msg("alerts: marshal payload")
			continue
		}

		event, err := q.InsertAlertEvent(ctx, queries.InsertAlertEventParams{
			RuleID:       rule.ID,
			TenantID:     project.TenantID,
			ScopeKind:    "project",
			ScopeID:      project.ID,
			PreviousTier: strPtr(previousTier),
			CurrentTier:  currentTier,
			Payload:      payloadBytes,
		})
		if err != nil {
			log.Error().Err(err).Str("rule_id", rule.ID.String()).
				Msg("alerts: insert event")
			continue
		}

		task, err := NewDispatchAlertTask(event.ID)
		if err != nil {
			log.Error().Err(err).Msg("alerts: build dispatch task")
			continue
		}
		if _, err := h.Asynq.EnqueueContext(ctx, task); err != nil {
			log.Error().Err(err).Str("event_id", event.ID.String()).
				Msg("alerts: enqueue dispatch")
			continue
		}

		log.Info().
			Str("rule_id", rule.ID.String()).
			Str("event_id", event.ID.String()).
			Str("previous_tier", previousTier).
			Str("current_tier", currentTier).
			Msg("alerts: tier change detected, dispatch enqueued")
	}
}

// coerceFloat converte um interface{} vindo do sqlc (que pode ser float64,
// int64 ou pgtype.Numeric) em float64. NULL/erro = 0 (caller já protegeu
// com sample_size).
func coerceFloat(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case pgtype.Numeric:
		if !x.Valid {
			return 0
		}
		f, err := x.Float64Value()
		if err != nil || !f.Valid {
			return 0
		}
		return f.Float64
	case []byte:
		// Numeric pode chegar como []byte em alguns paths do pgx.
		var n pgtype.Numeric
		if err := n.Scan(string(x)); err == nil {
			f, _ := n.Float64Value()
			if f.Valid {
				return f.Float64
			}
		}
		return 0
	case string:
		var n pgtype.Numeric
		if err := n.Scan(x); err == nil {
			f, _ := n.Float64Value()
			if f.Valid {
				return f.Float64
			}
		}
		return 0
	}
	return 0
}

// HandleCollectJira coleta incidents Jira para o projeto, faz upsert e
// linka cada incident novo ao deployment de produção mais recente cujo
// finished_at cai em (incident.created_at - IncidentLinkLookback, incident.created_at].
func (h *Handlers) HandleCollectJira(ctx context.Context, task *asynq.Task) error {
	var payload CollectJiraPayload
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

	if len(project.JiraProjectKeys) == 0 {
		log.Warn().
			Str("project_id", project.ID.String()).
			Msg("project has no jira_project_keys; skipping jira collect")
		return nil
	}

	jiraInstance, err := q.GetFirstSourceInstanceForTenantKind(ctx,
		queries.GetFirstSourceInstanceForTenantKindParams{
			TenantID: project.TenantID,
			Kind:     "jira",
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Warn().
				Str("project_id", project.ID.String()).
				Msg("no jira source_instance for tenant; skipping")
			return nil
		}
		return fmt.Errorf("load jira source: %w", err)
	}

	apiToken, err := resolveToken(ctx, h.Secret, jiraInstance)
	if err != nil {
		return fmt.Errorf("resolve jira token: %w", err)
	}

	email, err := resolveJiraEmail(ctx, h.Secret, jiraInstance)
	if err != nil {
		return fmt.Errorf("resolve jira email: %w", err)
	}

	source := jira.NewRESTSource(jiraInstance.BaseUrl, email, apiToken)

	// Janela JQL:
	//   - default 30d (cobre MTTR/CFR window que reportamos)
	//   - BackfillDays > 0 sobrescreve
	windowDays := 30
	if payload.BackfillDays > windowDays {
		windowDays = payload.BackfillDays
	}
	since := time.Now().UTC().AddDate(0, 0, -windowDays).Format("2006-01-02")

	keys := strings.Join(quoteEach(project.JiraProjectKeys), ", ")
	jql := fmt.Sprintf(`(%s) AND project in (%s) AND created >= "%s" ORDER BY created ASC`,
		project.IncidentJql, keys, since)

	issues, err := source.SearchIssues(ctx, jql, 0)
	if err != nil {
		return fmt.Errorf("jira search: %w", err)
	}

	for _, issue := range issues {
		row, err := q.UpsertIncident(ctx, queries.UpsertIncidentParams{
			TenantID:         project.TenantID,
			SourceInstanceID: jiraInstance.ID,
			ExternalID:       issue.Key,
			JiraProjectKey:   issue.ProjectKey,
			Summary:          issue.Summary,
			Status:           issue.Status,
			StatusCategory:   issue.StatusCategory,
			Priority:         strPtr(issue.Priority),
			Issuetype:        strPtr(issue.IssueType),
			Labels:           issue.Labels,
			CreatedAt:        issue.Created,
			ResolvedAt:       pgTimePtr(issue.Resolved),
			RawPayload:       issue.Raw,
		})
		if err != nil {
			return fmt.Errorf("upsert incident %s: %w", issue.Key, err)
		}

		// Linking: acha o deploy de produção mais recente em
		// (created - IncidentLinkLookback, created]; se houver, vincula.
		floor := issue.Created.Add(-IncidentLinkLookback)
		deployID, err := q.FindDeploymentForIncident(ctx,
			queries.FindDeploymentForIncidentParams{
				ProjectID:         project.ID,
				IncidentCreatedAt: pgTime(issue.Created),
				LookbackFloor:     pgTime(floor),
			},
		)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // nenhum deploy candidato — não é falha
		}
		if err != nil {
			return fmt.Errorf("find deploy for incident %s: %w", issue.Key, err)
		}
		if err := q.UpsertDeploymentIncidentLink(ctx,
			queries.UpsertDeploymentIncidentLinkParams{
				DeploymentID: deployID,
				IncidentID:   row.ID,
				LinkReason:   "time_window",
			},
		); err != nil {
			return fmt.Errorf("link incident %s ↔ deploy %s: %w",
				issue.Key, deployID, err)
		}
	}

	log.Info().
		Str("project_id", project.ID.String()).
		Str("path", project.PathWithNamespace).
		Int("incidents", len(issues)).
		Msg("jira collect complete")

	return nil
}

// quoteEach envolve cada elemento em aspas duplas (formato JQL).
func quoteEach(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = `"` + k + `"`
	}
	return out
}

// correlateDeploymentsAndMRs atribui cada MR mergeado ao primeiro deployment
// de produção bem-sucedido cujo finished_at é >= merged_at do MR.
//
// Estratégia time-window (ver docs/06-data-model.md). Para cada par
// (deployment_i, deployment_{i-1}) ordenado ASC, todos os MRs merged em
// (deployment_{i-1}.finished_at, deployment_i.finished_at] entram em
// deployment_i. Para o primeiro deployment, o limite inferior é 30 dias
// antes do seu finished_at (cobre o backfill).
//
// É idempotente: a função sempre varre todos os deploys do projeto e faz
// upsert no link table; reordenar/recalcular não duplica registros.
func (h *Handlers) correlateDeploymentsAndMRs(ctx context.Context, q *queries.Queries, project queries.PlatformProject) error {
	deploys, err := q.ListProductionDeploymentsForProject(ctx, project.ID)
	if err != nil {
		return fmt.Errorf("list production deployments: %w", err)
	}

	branches := []string{project.DefaultBranch}

	for i, d := range deploys {
		until := d.FinishedAt
		if !until.Valid {
			continue
		}

		var from pgtype.Timestamptz
		if i == 0 {
			from = pgTime(until.Time.Add(-30 * 24 * time.Hour))
		} else if prev := deploys[i-1].FinishedAt; prev.Valid {
			from = prev
		} else {
			from = pgTime(until.Time.Add(-30 * 24 * time.Hour))
		}

		mrs, err := q.ListMergedMRsBetween(ctx, queries.ListMergedMRsBetweenParams{
			ProjectID:      project.ID,
			TargetBranches: branches,
			MergedAfter:    from,
			MergedUntil:    until,
		})
		if err != nil {
			return fmt.Errorf("list merged MRs for deploy %s: %w", d.ID, err)
		}

		for _, m := range mrs {
			if err := q.UpsertDeploymentMRLink(ctx, queries.UpsertDeploymentMRLinkParams{
				DeploymentID:   d.ID,
				MergeRequestID: m.ID,
			}); err != nil {
				return fmt.Errorf("link deploy %s ↔ MR %s: %w", d.ID, m.ID, err)
			}
		}
	}
	return nil
}

// firstCommit busca os commits do MR e devolve o mais antigo (authored_date)
// com seu SHA. Retorna (nil, "") em caso de erro — a atribuição em si não falha
// porque Lead Time daquele MR fica indisponível até o próximo ciclo.
func firstCommit(ctx context.Context, client *gitlab.Client, projectID string, m gitlab.MergeRequest) (*time.Time, string) {
	commits, err := client.ListMRCommits(ctx, projectID, m.IID)
	if err != nil {
		log.Warn().
			Err(err).
			Int("mr_iid", m.IID).
			Msg("list MR commits failed; lead_time will be unavailable for this MR")
		return nil, ""
	}
	if len(commits) == 0 {
		return nil, ""
	}
	earliest := commits[0]
	for _, c := range commits[1:] {
		if c.AuthoredDate.Before(earliest.AuthoredDate) {
			earliest = c
		}
	}
	return &earliest.AuthoredDate, earliest.ID
}

func authorUsername(m gitlab.MergeRequest) *string {
	if m.Author == nil {
		return nil
	}
	return strPtr(m.Author.Username)
}

// isBotAuthor classifica autores conhecidos como bots para que MRs deles não
// afetem o cálculo de Lead Time (sinal técnico recomendado pelo DORA).
// Lista extensível por projeto no futuro (coluna em platform.project).
func isBotAuthor(m gitlab.MergeRequest) bool {
	if m.Author == nil {
		return false
	}
	switch m.Author.Username {
	case "dependabot", "dependabot[bot]",
		"renovate", "renovate[bot]", "renovate-bot",
		"gitlab-bot", "ghost-user":
		return true
	}
	return false
}

func toInt32Ptr(p *int) *int32 {
	if p == nil {
		return nil
	}
	v := int32(*p)
	return &v
}

// HandleSnapshotMonthly congela a foto da janela 30d de CADA projeto ativo
// em metrics.metric_monthly_snapshot. Idempotente — re-rodar no mesmo mês
// sobrescreve. Cron previsto: `0 0 1 * *` (1º dia do mês 00:00 UTC,
// capturando o mês que acabou).
func (h *Handlers) HandleSnapshotMonthly(ctx context.Context, _ *asynq.Task) error {
	q := queries.New(h.DB.Pool)

	projects, err := q.ListActiveProjects(ctx)
	if err != nil {
		return fmt.Errorf("list active projects: %w", err)
	}

	// Mês a congelar = mês anterior ao corrente. Cron roda dia 1, então
	// (hoje - 1 mês) é seguro pra ancorar no mês completo recém-encerrado.
	month := time.Now().UTC().AddDate(0, -1, 0)
	monthDate := pgtype.Date{
		Time:  time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC),
		Valid: true,
	}

	processed := 0
	for _, p := range projects {
		row, err := q.GetLatestMetricWindow(ctx, queries.GetLatestMetricWindowParams{
			TenantID:   p.TenantID,
			ScopeKind:  "project",
			ScopeID:    p.ID,
			WindowDays: 30,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			log.Error().Err(err).Str("project_id", p.ID.String()).
				Msg("snapshot: load metric_window")
			continue
		}

		_, err = q.UpsertMonthlySnapshot(ctx, queries.UpsertMonthlySnapshotParams{
			TenantID:            p.TenantID,
			ScopeKind:           "project",
			ScopeID:             p.ID,
			Month:               monthDate,
			DeploymentFrequency: row.DeploymentFrequency,
			LeadTimeMedianS:     row.LeadTimeMedianS,
			ChangeFailureRate:   row.ChangeFailureRate,
			MttrMeanS:           row.MttrMeanS,
			Classification:      row.Classification,
		})
		if err != nil {
			log.Error().Err(err).Str("project_id", p.ID.String()).
				Msg("snapshot: upsert")
			continue
		}
		processed++
	}

	log.Info().
		Int("projects", processed).
		Str("month", monthDate.Time.Format("2006-01")).
		Msg("monthly snapshot complete")
	return nil
}

// resolveToken devolve a credencial efetiva para uma source_instance.
//
// Precedência:
//   1. instance.SecretValue (token gravado via UI / REST)
//   2. secret.Provider.Get(instance.AuthRef) (env var / vault — legado)
//
// Permite migrar gradualmente do env-driven para o DB-driven sem quebrar
// instalações que já dependem do .env.
func resolveToken(ctx context.Context, provider secret.Provider, instance queries.PlatformSourceInstance) (string, error) {
	if instance.SecretValue != nil && *instance.SecretValue != "" {
		return *instance.SecretValue, nil
	}
	if instance.AuthRef == "" {
		return "", fmt.Errorf("source instance %s has neither secret_value nor auth_ref", instance.ID)
	}
	return provider.Get(ctx, instance.AuthRef)
}

// resolveJiraEmail extrai o email de auth do Jira:
//   1. instance.AuthEmail (configurado via UI)
//   2. secret.Provider.Get("JIRA_EMAIL") (legado)
func resolveJiraEmail(ctx context.Context, provider secret.Provider, instance queries.PlatformSourceInstance) (string, error) {
	if instance.AuthEmail != nil && *instance.AuthEmail != "" {
		return *instance.AuthEmail, nil
	}
	return provider.Get(ctx, "JIRA_EMAIL")
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
