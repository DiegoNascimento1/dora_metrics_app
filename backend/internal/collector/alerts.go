package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

// alertDispatchTimeout limita o tempo total de uma tentativa de POST webhook.
// Erros transitórios (timeout/5xx) viram falha não-permanente para o asynq
// re-tentar com backoff; 4xx vira SkipRetry porque o destino claramente
// rejeitou o payload e re-enviar não vai mudar nada.
const alertDispatchTimeout = 10 * time.Second

// HandleDispatchAlert entrega um alert_event pendente via HTTP POST e atualiza
// o status (delivered/failed). Idempotente: chamadas repetidas com o mesmo
// event_id apenas sobrescrevem o status final, sem efeitos colaterais.
func (h *Handlers) HandleDispatchAlert(ctx context.Context, task *asynq.Task) error {
	var payload DispatchAlertPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w (%w)", err, asynq.SkipRetry)
	}

	q := queries.New(h.DB.Pool)

	event, err := q.GetAlertEvent(ctx, payload.EventID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Já foi removido (rule deletada, etc) — nada a fazer.
			return nil
		}
		return fmt.Errorf("load alert event: %w", err)
	}

	rule, err := q.GetAlertRule(ctx, event.RuleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Regra deletada entre detecção e dispatch: marca como failed
			// permanente e desiste.
			_ = q.MarkAlertEventFailed(ctx, queries.MarkAlertEventFailedParams{
				ID:        event.ID,
				LastError: strPtr("alert rule deleted before dispatch"),
			})
			return nil
		}
		return fmt.Errorf("load alert rule: %w", err)
	}

	body, err := buildWebhookBody(event, rule)
	if err != nil {
		_ = q.MarkAlertEventFailed(ctx, queries.MarkAlertEventFailedParams{
			ID:        event.ID,
			LastError: strPtr(err.Error()),
		})
		return fmt.Errorf("build webhook body: %w (%w)", err, asynq.SkipRetry)
	}

	reqCtx, cancel := context.WithTimeout(ctx, alertDispatchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, rule.WebhookUrl, bytes.NewReader(body))
	if err != nil {
		_ = q.MarkAlertEventFailed(ctx, queries.MarkAlertEventFailedParams{
			ID:        event.ID,
			LastError: strPtr(err.Error()),
		})
		return fmt.Errorf("build request: %w (%w)", err, asynq.SkipRetry)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "dora-metrics-app/alerts")

	// Adapta body/headers para PagerDuty Events v2 ou Opsgenie Alerts
	// quando a URL apontar pra esses destinos. Genérico (Slack/Teams)
	// preserva o body original.
	if err := adaptRequest(req, event, rule); err != nil {
		_ = q.MarkAlertEventFailed(ctx, queries.MarkAlertEventFailedParams{
			ID:        event.ID,
			LastError: strPtr(err.Error()),
		})
		return fmt.Errorf("adapt request: %w (%w)", err, asynq.SkipRetry)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		_ = q.MarkAlertEventFailed(ctx, queries.MarkAlertEventFailedParams{
			ID:        event.ID,
			LastError: strPtr(err.Error()),
		})
		return fmt.Errorf("post webhook: %w", err) // retryable
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	httpStatus := int32(resp.StatusCode)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := q.MarkAlertEventDelivered(ctx, queries.MarkAlertEventDeliveredParams{
			ID:         event.ID,
			HttpStatus: &httpStatus,
		}); err != nil {
			return fmt.Errorf("mark delivered: %w", err)
		}
		log.Info().
			Str("event_id", event.ID.String()).
			Int("status", resp.StatusCode).
			Msg("alerts: dispatch delivered")
		return nil
	}

	errMsg := fmt.Sprintf("webhook returned status %d", resp.StatusCode)
	if err := q.MarkAlertEventFailed(ctx, queries.MarkAlertEventFailedParams{
		ID:         event.ID,
		HttpStatus: &httpStatus,
		LastError:  &errMsg,
	}); err != nil {
		log.Error().Err(err).Msg("alerts: mark failed")
	}

	// 4xx (exceto 408/429) é erro permanente do cliente — SkipRetry.
	// 5xx e 408/429 são retryable.
	if resp.StatusCode >= 400 && resp.StatusCode < 500 &&
		resp.StatusCode != http.StatusRequestTimeout &&
		resp.StatusCode != http.StatusTooManyRequests {
		return fmt.Errorf("%s (%w)", errMsg, asynq.SkipRetry)
	}
	return errors.New(errMsg)
}

// buildWebhookBody monta o JSON de saída no formato Slack-compatible
// (campo "text") mais o "alert" detalhado pra consumidores que queiram parsear.
func buildWebhookBody(event queries.PlatformAlertEvent, rule queries.PlatformAlertRule) ([]byte, error) {
	previous := ""
	if event.PreviousTier != nil {
		previous = *event.PreviousTier
	}

	emoji := "📉"
	verb := "regrediu"
	if alertIsPromotion(previous, event.CurrentTier) {
		emoji = "📈"
		verb = "subiu"
	}

	text := fmt.Sprintf("%s *%s* — classificação DORA %s de `%s` para `%s` (scope=%s, window=%dd)",
		emoji, rule.Name, verb, previous, event.CurrentTier, event.ScopeKind, rule.WindowDays)

	body := map[string]any{
		"text": text,
		"alert": map[string]any{
			"event_id":      event.ID.String(),
			"rule_id":       rule.ID.String(),
			"rule_name":     rule.Name,
			"kind":          rule.Kind,
			"scope_kind":    event.ScopeKind,
			"scope_id":      event.ScopeID.String(),
			"previous_tier": previous,
			"current_tier":  event.CurrentTier,
			"window_days":   rule.WindowDays,
			"fired_at":      event.FiredAt.Format(time.RFC3339),
		},
	}
	return json.Marshal(body)
}

func alertIsPromotion(previous, current string) bool {
	order := func(t string) int {
		switch t {
		case "elite":
			return 4
		case "high":
			return 3
		case "medium":
			return 2
		case "low":
			return 1
		}
		return -1
	}
	p, c := order(previous), order(current)
	if p < 0 || c < 0 {
		return false
	}
	return c > p
}
