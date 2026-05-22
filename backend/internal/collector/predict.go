// Task asynq `predict:weekly` — roda regressão linear sobre o histórico
// de metric_window 30d de cada projeto/time ativo. Se a tendência for
// degrading com confiança ≥ medium e haver alert_rule do kind
// `predicted_regression`, insere um alert_event para dispatch.
//
// Cron: "0 10 * * 1" (segunda 10:00 UTC — 1h depois do digest).
// Idempotência: a inserção do alert_event tem PK natural via
// (rule_id, fired_at_day) — re-execução no mesmo dia não duplica.
//
// Tipo de regra novo: `predicted_regression`. Disparado quando o
// Predict devolve direction=degrading + confidence>=medium para o
// escopo da regra. Reusa o pipeline atual de alert_event/dispatch:alert.
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/prediction"
)

const (
	predictMinSamples       = prediction.MinSamplesForPrediction
	predictLookbackDays     = 180
	predictMaxSamplesQuery  = 50
)

// HandlePredictWeekly itera todos os projetos+times ativos, calcula a
// predição para a janela 30d e, se houver regra alert_rule com
// kind="predicted_regression" para o escopo (ou globalmente), enfileira
// alert_event quando a predição é degrading com confidence>=medium.
func (h *Handlers) HandlePredictWeekly(ctx context.Context, _ *asynq.Task) error {
	rows, err := h.DB.Pool.Query(ctx, `
		SELECT id, tenant_id, 'project'::text AS scope_kind FROM platform.project
		WHERE is_active = true
		UNION ALL
		SELECT id, tenant_id, 'team'::text AS scope_kind FROM platform.team
	`)
	if err != nil {
		return fmt.Errorf("list active scopes: %w", err)
	}
	defer rows.Close()

	type scopeRef struct {
		id        uuid.UUID
		tenantID  uuid.UUID
		scopeKind string
	}
	var scopes []scopeRef
	for rows.Next() {
		var s scopeRef
		if err := rows.Scan(&s.id, &s.tenantID, &s.scopeKind); err != nil {
			return fmt.Errorf("scan scope: %w", err)
		}
		scopes = append(scopes, s)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate scopes: %w", err)
	}

	for _, s := range scopes {
		samples, err := h.loadPredictionSamples(ctx, s.scopeKind, s.id, 30)
		if err != nil {
			log.Warn().Err(err).Str("scope", s.scopeKind).Str("id", s.id.String()).Msg("predict: load samples")
			continue
		}
		if len(samples) < predictMinSamples {
			continue
		}
		pred := prediction.Predict(samples)
		if pred.Direction != "degrading" || pred.Confidence == "low" {
			continue
		}

		if err := h.firePredictionAlert(ctx, s.tenantID, s.scopeKind, s.id, pred); err != nil {
			log.Warn().Err(err).Str("scope", s.scopeKind).Str("id", s.id.String()).Msg("predict: fire alert")
		}
	}

	log.Info().Int("scopes", len(scopes)).Msg("predict:weekly concluído")
	return nil
}

func (h *Handlers) loadPredictionSamples(
	ctx context.Context, scopeKind string, scopeID uuid.UUID, windowDays int,
) ([]prediction.Sample, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -predictLookbackDays)
	rows, err := h.DB.Pool.Query(ctx, `
		SELECT computed_at, COALESCE(classification, 'insufficient_data')
		FROM metrics.metric_window
		WHERE scope_kind = $1 AND scope_id = $2 AND window_days = $3
		  AND computed_at >= $4
		ORDER BY computed_at ASC
		LIMIT $5
	`, scopeKind, scopeID, windowDays, cutoff, predictMaxSamplesQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []prediction.Sample
	for rows.Next() {
		var ts time.Time
		var tier string
		if err := rows.Scan(&ts, &tier); err != nil {
			return nil, err
		}
		out = append(out, prediction.Sample{T: ts, Tier: tier})
	}
	return out, rows.Err()
}

// firePredictionAlert busca alert_rules com kind=predicted_regression
// que casem com o escopo (rule.scope_id null = catch-all do tenant) e
// insere alert_event idempotente por (rule_id, fired_at_date).
func (h *Handlers) firePredictionAlert(
	ctx context.Context,
	tenantID uuid.UUID,
	scopeKind string,
	scopeID uuid.UUID,
	pred prediction.Prediction,
) error {
	rows, err := h.DB.Pool.Query(ctx, `
		SELECT id
		FROM platform.alert_rule
		WHERE tenant_id = $1 AND enabled = true AND kind = 'predicted_regression'
		  AND (scope_kind = $2 OR scope_kind IS NULL)
		  AND (scope_id = $3 OR scope_id IS NULL)
	`, tenantID, scopeKind, scopeID)
	if err != nil {
		return fmt.Errorf("list rules: %w", err)
	}
	defer rows.Close()

	var ruleIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ruleIDs = append(ruleIDs, id)
	}

	if len(ruleIDs) == 0 {
		return nil
	}

	predJSON, _ := json.Marshal(pred)

	for _, rid := range ruleIDs {
		// Idempotência manual: já tem alert_event hoje pra essa regra+escopo?
		var exists bool
		if err := h.DB.Pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM platform.alert_event
				WHERE rule_id = $1 AND scope_id = $2
				  AND fired_at >= date_trunc('day', now())
			)
		`, rid, scopeID).Scan(&exists); err != nil {
			log.Warn().Err(err).Msg("predict: check existing event")
			continue
		}
		if exists {
			continue
		}

		var eventID uuid.UUID
		if err := h.DB.Pool.QueryRow(ctx, `
			INSERT INTO platform.alert_event (
				id, tenant_id, rule_id, fired_at, scope_kind, scope_id,
				previous_tier, current_tier, delivery_status, last_error
			)
			VALUES (
				gen_random_uuid(), $1, $2, now(), $3, $4,
				NULL, $5, 'pending', $6
			)
			RETURNING id
		`, tenantID, rid, scopeKind, scopeID, pred.CurrentTier, string(predJSON)).Scan(&eventID); err != nil {
			log.Warn().Err(err).Msg("predict: insert event")
			continue
		}

		task, taskErr := NewDispatchAlertTask(eventID)
		if taskErr != nil {
			log.Warn().Err(taskErr).Msg("predict: build dispatch task")
			continue
		}
		if _, err := h.Asynq.EnqueueContext(ctx, task); err != nil {
			log.Warn().Err(err).Msg("predict: enqueue dispatch")
		}
	}
	return nil
}
