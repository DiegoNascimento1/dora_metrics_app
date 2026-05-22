package collector

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

func TestDetectDestination(t *testing.T) {
	cases := []struct {
		url  string
		want destinationKind
	}{
		{"https://events.pagerduty.com/v2/enqueue?routing_key=KEY", destPagerDuty},
		{"https://api.opsgenie.com/v2/alerts?api_key=KEY", destOpsgenie},
		{"https://hooks.slack.com/services/T/B/X", destGeneric},
		{"https://acme.webhook.office.com/...", destGeneric},
		{"not-a-url", destGeneric},
	}
	for _, c := range cases {
		if got := detectDestination(c.url); got != c.want {
			t.Errorf("detectDestination(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func newAlertFixtures(t *testing.T, webhookURL string, current string) (queries.PlatformAlertEvent, queries.PlatformAlertRule) {
	t.Helper()
	prev := "high"
	return queries.PlatformAlertEvent{
			ID:           uuid.New(),
			RuleID:       uuid.New(),
			ScopeKind:    "project",
			ScopeID:      uuid.New(),
			PreviousTier: &prev,
			CurrentTier:  current,
			FiredAt:      time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
		},
		queries.PlatformAlertRule{
			ID:         uuid.New(),
			Name:       "DF dropped",
			Kind:       "tier_regression",
			WindowDays: 30,
			WebhookUrl: webhookURL,
		}
}

func TestAdaptRequest_PagerDutyBuildsEventEnvelope(t *testing.T) {
	event, rule := newAlertFixtures(t, "https://events.pagerduty.com/v2/enqueue?routing_key=ROUTE", "low")

	req, _ := http.NewRequest(http.MethodPost, rule.WebhookUrl, strings.NewReader(`{}`))
	if err := adaptRequest(req, event, rule); err != nil {
		t.Fatalf("adaptRequest: %v", err)
	}
	body, _ := io.ReadAll(req.Body)

	var pd map[string]any
	if err := json.Unmarshal(body, &pd); err != nil {
		t.Fatalf("decode pd body: %v (raw %s)", err, body)
	}
	if pd["routing_key"] != "ROUTE" {
		t.Errorf("routing_key = %v", pd["routing_key"])
	}
	if pd["event_action"] != "trigger" {
		t.Errorf("event_action = %v", pd["event_action"])
	}
	if pd["dedup_key"] != event.ID.String() {
		t.Errorf("dedup_key = %v", pd["dedup_key"])
	}
	payload, _ := pd["payload"].(map[string]any)
	if payload["severity"] != "error" {
		t.Errorf("severity = %v, want error (current=low)", payload["severity"])
	}
}

func TestAdaptRequest_PagerDutyPromotionResolves(t *testing.T) {
	// high → elite = promoção: event_action="resolve"
	event, rule := newAlertFixtures(t, "https://events.pagerduty.com/v2/enqueue?routing_key=R", "elite")
	req, _ := http.NewRequest(http.MethodPost, rule.WebhookUrl, strings.NewReader(`{}`))
	if err := adaptRequest(req, event, rule); err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(req.Body)
	var pd map[string]any
	_ = json.Unmarshal(body, &pd)
	if pd["event_action"] != "resolve" {
		t.Errorf("event_action = %v, want resolve para promoção", pd["event_action"])
	}
}

func TestAdaptRequest_PagerDutyRequiresRoutingKey(t *testing.T) {
	event, rule := newAlertFixtures(t, "https://events.pagerduty.com/v2/enqueue", "low")
	req, _ := http.NewRequest(http.MethodPost, rule.WebhookUrl, strings.NewReader(`{}`))
	err := adaptRequest(req, event, rule)
	if err == nil {
		t.Fatal("expected error sem routing_key")
	}
	if !strings.Contains(err.Error(), "routing_key") {
		t.Errorf("err = %v", err)
	}
}

func TestAdaptRequest_OpsgenieBuildsAlertEnvelope(t *testing.T) {
	event, rule := newAlertFixtures(t, "https://api.opsgenie.com/v2/alerts?api_key=KEY", "low")
	req, _ := http.NewRequest(http.MethodPost, rule.WebhookUrl, strings.NewReader(`{}`))
	if err := adaptRequest(req, event, rule); err != nil {
		t.Fatalf("adaptRequest: %v", err)
	}
	if h := req.Header.Get("Authorization"); h != "GenieKey KEY" {
		t.Errorf("Authorization = %q", h)
	}
	if q := req.URL.RawQuery; strings.Contains(q, "api_key") {
		t.Errorf("api_key não foi removido da URL: %q", q)
	}
	body, _ := io.ReadAll(req.Body)
	var og map[string]any
	if err := json.Unmarshal(body, &og); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if og["alias"] != event.ID.String() {
		t.Errorf("alias = %v", og["alias"])
	}
	if og["priority"] != "P2" {
		t.Errorf("priority = %v, want P2 (low tier)", og["priority"])
	}
	tags, _ := og["tags"].([]any)
	if len(tags) == 0 {
		t.Error("tags vazias")
	}
}

func TestAdaptRequest_GenericIsNoOp(t *testing.T) {
	event, rule := newAlertFixtures(t, "https://hooks.slack.com/services/T/B/X", "low")
	originalBody := `{"text":"original"}`
	req, _ := http.NewRequest(http.MethodPost, rule.WebhookUrl, strings.NewReader(originalBody))
	if err := adaptRequest(req, event, rule); err != nil {
		t.Fatalf("adaptRequest: %v", err)
	}
	body, _ := io.ReadAll(req.Body)
	if string(body) != originalBody {
		t.Errorf("body alterado: %s", body)
	}
	if req.Header.Get("Authorization") != "" {
		t.Errorf("Authorization adicionado em destino genérico")
	}
}
