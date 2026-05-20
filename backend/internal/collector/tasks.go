// Package collector define os tipos de tasks asynq compartilhados entre
// o scheduler periódico e os handlers.
package collector

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// Tipos de tasks. Stringly-typed para fácil debug no asynqmon.
const (
	TaskScanActiveProjects   = "scan:active_projects"
	TaskReconcileAllProjects = "reconcile:projects"
	TaskCollectGitlab        = "collect:gitlab:deployments"
	TaskCollectJira          = "collect:jira:incidents"
	TaskComputeMetricWindow  = "compute:metric_window"
)

// Filas (declaradas no asynq.Config).
const (
	QueueCollect = "collect"
	QueueCompute = "compute"
	QueueDefault = "default"
)

// CollectGitlabPayload é o payload da task collect:gitlab:deployments.
//
// BackfillDays > 0 força o coletor a buscar uma janela maior, ignorando
// last_synced_at. Usado pelo job de reconciliação noturna.
type CollectGitlabPayload struct {
	ProjectID    uuid.UUID `json:"project_id"`
	BackfillDays int       `json:"backfill_days,omitempty"`
}

// NewCollectGitlabTask constrói a task para enfileirar.
func NewCollectGitlabTask(projectID uuid.UUID) (*asynq.Task, error) {
	return newCollectGitlabTask(projectID, 0)
}

// NewCollectGitlabTaskWithBackfill cria a task forçando backfill de N dias.
func NewCollectGitlabTaskWithBackfill(projectID uuid.UUID, days int) (*asynq.Task, error) {
	return newCollectGitlabTask(projectID, days)
}

func newCollectGitlabTask(projectID uuid.UUID, backfillDays int) (*asynq.Task, error) {
	payload, err := json.Marshal(CollectGitlabPayload{
		ProjectID:    projectID,
		BackfillDays: backfillDays,
	})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(
		TaskCollectGitlab, payload,
		asynq.Queue(QueueCollect),
		asynq.MaxRetry(3),
	), nil
}

// CollectJiraPayload é o payload da task collect:jira:incidents.
type CollectJiraPayload struct {
	ProjectID    uuid.UUID `json:"project_id"`
	BackfillDays int       `json:"backfill_days,omitempty"`
}

// NewCollectJiraTask constrói a task para enfileirar.
func NewCollectJiraTask(projectID uuid.UUID) (*asynq.Task, error) {
	return newCollectJiraTask(projectID, 0)
}

// NewCollectJiraTaskWithBackfill cria a task forçando backfill de N dias.
func NewCollectJiraTaskWithBackfill(projectID uuid.UUID, days int) (*asynq.Task, error) {
	return newCollectJiraTask(projectID, days)
}

func newCollectJiraTask(projectID uuid.UUID, backfillDays int) (*asynq.Task, error) {
	payload, err := json.Marshal(CollectJiraPayload{
		ProjectID:    projectID,
		BackfillDays: backfillDays,
	})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(
		TaskCollectJira, payload,
		asynq.Queue(QueueCollect),
		asynq.MaxRetry(3),
	), nil
}

// ComputeMetricWindowPayload é o payload da task compute:metric_window.
type ComputeMetricWindowPayload struct {
	ProjectID  uuid.UUID `json:"project_id"`
	WindowDays int       `json:"window_days"`
}

// NewComputeMetricWindowTask constrói a task para enfileirar.
func NewComputeMetricWindowTask(projectID uuid.UUID, windowDays int) (*asynq.Task, error) {
	payload, err := json.Marshal(ComputeMetricWindowPayload{
		ProjectID:  projectID,
		WindowDays: windowDays,
	})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(
		TaskComputeMetricWindow, payload,
		asynq.Queue(QueueCompute),
		asynq.MaxRetry(2),
	), nil
}
