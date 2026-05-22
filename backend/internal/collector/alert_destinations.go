// Destinos de alerta — PagerDuty Events v2 + Opsgenie Alerts API +
// genérico Slack-compatible.
//
// Detecção pelo host da URL configurada na alert_rule.webhook_url:
//
//   events.pagerduty.com   → formato PagerDuty Events API v2.
//                            routing_key vai como ?routing_key=... na URL.
//   api.opsgenie.com       → formato Opsgenie Alerts API. Auth via
//                            ?api_key=... na URL.
//   *                      → formato genérico (Slack/Teams-compatible).
//
// Em PagerDuty/Opsgenie, autenticação fica embedded na URL para evitar
// adicionar uma coluna nova em platform.alert_rule só pra carry auth.
// Em produção real, mover para um secret separado (alert_rule.auth_ref).
package collector

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

// destinationKind escolhe o formato com base no host da URL.
type destinationKind string

const (
	destPagerDuty destinationKind = "pagerduty"
	destOpsgenie  destinationKind = "opsgenie"
	destGeneric   destinationKind = "generic"
)

func detectDestination(webhookURL string) destinationKind {
	u, err := url.Parse(webhookURL)
	if err != nil {
		return destGeneric
	}
	switch {
	case strings.HasSuffix(u.Host, "pagerduty.com"):
		return destPagerDuty
	case strings.HasSuffix(u.Host, "opsgenie.com"):
		return destOpsgenie
	default:
		return destGeneric
	}
}

// adaptRequest mexe no request para o formato esperado pelo destino:
//   - PagerDuty: substitui o body pelo envelope Events v2.
//   - Opsgenie: substitui o body pelo envelope Alerts v2 + header
//     Authorization GenieKey ${api_key}.
//   - Generic: deixa o request original (formato Slack/Teams).
//
// Devolve potencialmente nil error mesmo quando alterou request — o
// caller continua o dispatch normalmente.
func adaptRequest(
	req *http.Request,
	event queries.PlatformAlertEvent,
	rule queries.PlatformAlertRule,
) error {
	kind := detectDestination(rule.WebhookUrl)
	if kind == destGeneric {
		return nil
	}

	switch kind {
	case destPagerDuty:
		body, err := buildPagerDutyBody(event, rule, req.URL)
		if err != nil {
			return err
		}
		setBodyJSON(req, body)
		// PagerDuty Events v2 não exige auth header — routing_key vai no body.
		return nil

	case destOpsgenie:
		body, err := buildOpsgenieBody(event, rule)
		if err != nil {
			return err
		}
		setBodyJSON(req, body)
		// Opsgenie autentica via header GenieKey (mais seguro do que query).
		apiKey := req.URL.Query().Get("api_key")
		if apiKey != "" {
			req.Header.Set("Authorization", "GenieKey "+apiKey)
			// Limpa o api_key da URL para não vazar nos logs do destino.
			q := req.URL.Query()
			q.Del("api_key")
			req.URL.RawQuery = q.Encode()
		}
		return nil
	}
	return nil
}

func setBodyJSON(req *http.Request, body []byte) {
	req.Body = readCloserFromBytes(body)
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
}

// buildPagerDutyBody monta um Event de PagerDuty Events API v2.
//   dedup_key = event.ID → re-dispatch do mesmo event_id atualiza o incident
//                          em vez de criar duplicado.
//   severity: regression → "warning"/"error"; promotion → "info".
//   routing_key extraído do query param da webhook URL.
func buildPagerDutyBody(
	event queries.PlatformAlertEvent,
	rule queries.PlatformAlertRule,
	webhookURL *url.URL,
) ([]byte, error) {
	routingKey := webhookURL.Query().Get("routing_key")
	if routingKey == "" {
		return nil, fmt.Errorf("pagerduty: webhook URL precisa ?routing_key=...")
	}

	previous := ""
	if event.PreviousTier != nil {
		previous = *event.PreviousTier
	}

	severity := "warning"
	eventAction := "trigger"
	if alertIsPromotion(previous, event.CurrentTier) {
		severity = "info"
		// Promoções não geram incident — mandamos resolve do dedup_key.
		eventAction = "resolve"
	} else if event.CurrentTier == "low" {
		severity = "error"
	}

	body := map[string]any{
		"routing_key":  routingKey,
		"event_action": eventAction,
		"dedup_key":    event.ID.String(),
		"payload": map[string]any{
			"summary":   fmt.Sprintf("[%s] DORA %s → %s", rule.Name, previous, event.CurrentTier),
			"source":    "dora-metrics",
			"severity":  severity,
			"component": "dora",
			"group":     event.ScopeKind,
			"class":     rule.Kind,
			"custom_details": map[string]any{
				"rule_name":     rule.Name,
				"scope_kind":    event.ScopeKind,
				"scope_id":      event.ScopeID.String(),
				"previous_tier": previous,
				"current_tier":  event.CurrentTier,
				"window_days":   rule.WindowDays,
				"fired_at":      event.FiredAt.Format(time.RFC3339),
			},
		},
	}
	return json.Marshal(body)
}

// buildOpsgenieBody monta um Alert no formato POST /v2/alerts.
func buildOpsgenieBody(
	event queries.PlatformAlertEvent,
	rule queries.PlatformAlertRule,
) ([]byte, error) {
	previous := ""
	if event.PreviousTier != nil {
		previous = *event.PreviousTier
	}

	priority := "P3"
	if event.CurrentTier == "low" {
		priority = "P2"
	}

	body := map[string]any{
		"message": fmt.Sprintf("DORA %s: %s → %s", rule.Name, previous, event.CurrentTier),
		"alias":   event.ID.String(), // dedup_key Opsgenie
		"description": fmt.Sprintf(
			"Classificação DORA do %s regrediu de %s para %s.\nJanela: %dd\nScope ID: %s",
			event.ScopeKind, previous, event.CurrentTier, rule.WindowDays, event.ScopeID,
		),
		"priority": priority,
		"source":   "dora-metrics",
		"tags":     []string{"dora", event.ScopeKind, event.CurrentTier},
		"details": map[string]any{
			"rule_id":     rule.ID.String(),
			"window_days": fmt.Sprintf("%d", rule.WindowDays),
			"scope_id":    event.ScopeID.String(),
			"fired_at":    event.FiredAt.Format(time.RFC3339),
		},
	}
	return json.Marshal(body)
}

// readCloserFromBytes wraps bytes em um io.ReadCloser sem dep extra.
type bytesReadCloser struct {
	data []byte
	off  int
}

func readCloserFromBytes(b []byte) *bytesReadCloser {
	return &bytesReadCloser{data: b}
}

func (r *bytesReadCloser) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

func (r *bytesReadCloser) Close() error { return nil }
