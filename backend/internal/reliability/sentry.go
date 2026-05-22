// SentryProvider lê Sentry "Performance Monitoring SLOs" via API REST
// v0 (`/api/0/organizations/{org}/`).
//
// Sentry expõe SLOs como "issue-alert thresholds" + Performance metrics.
// Para nosso modelo, usamos endpoint /performance-metrics/ que devolve
// taxa de erro e p95 latency — derivamos um SLO sintético (target
// vindo de env SENTRY_SLO_TARGET_PERCENT, default 99.9).
//
// scopeRef no Sentry = project slug (ex: "dora-api"). Vazio = todos os
// projects da org.
package reliability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// SentryProvider implementa Provider via Sentry API v0.
type SentryProvider struct {
	baseURL  string
	authTok  string
	orgSlug  string
	target   float64 // SLO target derivado de env
	period   int     // dias da janela
	http     *http.Client
}

// NewSentryProvider lê env SENTRY_AUTH_TOKEN + SENTRY_ORG_SLUG.
// Opcionais: SENTRY_BASE_URL (default oficial), SENTRY_SLO_TARGET_PERCENT,
// SENTRY_SLO_PERIOD_DAYS.
func NewSentryProvider() (*SentryProvider, error) {
	tok := os.Getenv("SENTRY_AUTH_TOKEN")
	org := os.Getenv("SENTRY_ORG_SLUG")
	if tok == "" || org == "" {
		return nil, fmt.Errorf("SENTRY_AUTH_TOKEN + SENTRY_ORG_SLUG: %w", ErrNotConfigured)
	}
	baseURL := os.Getenv("SENTRY_BASE_URL")
	if baseURL == "" {
		baseURL = "https://sentry.io"
	}
	target := 99.9
	if v := os.Getenv("SENTRY_SLO_TARGET_PERCENT"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			target = parsed
		}
	}
	period := 30
	if v := os.Getenv("SENTRY_SLO_PERIOD_DAYS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			period = parsed
		}
	}
	return &SentryProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		authTok: tok,
		orgSlug: org,
		target:  target,
		period:  period,
		http:    &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (s *SentryProvider) Name() string { return "sentry" }

// sentryProject é a shape de GET /api/0/organizations/{org}/projects/.
type sentryProject struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// sentryStats é a shape de GET /api/0/organizations/{org}/stats_v2/.
// Pedimos os campos "sum(quantity)" + grupo por outcome para derivar
// taxa de erro.
type sentryStats struct {
	Groups []struct {
		By struct {
			Outcome string `json:"outcome"`
		} `json:"by"`
		Totals struct {
			SumQuantity float64 `json:"sum(quantity)"`
		} `json:"totals"`
	} `json:"groups"`
}

// ListSLOs devolve 1 SLO sintético por project. SLI = % de eventos
// "accepted" (não-erro) / total. errorBudget = (target - actual) / (100 - target).
func (s *SentryProvider) ListSLOs(ctx context.Context, scopeRef string) ([]SLOStatus, error) {
	projects, err := s.listProjects(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]SLOStatus, 0, len(projects))
	for _, p := range projects {
		if scopeRef != "" && p.Slug != scopeRef {
			continue
		}
		actual, err := s.computeSLI(ctx, p.Slug)
		if err != nil {
			// projeto sem dados ainda — devolve placeholder healthy.
			out = append(out, SLOStatus{
				ID: p.ID, Name: p.Name + " (sem dados)",
				Target: s.target, PeriodDays: s.period,
				Status: "healthy", Source: "sentry",
				URL: fmt.Sprintf("%s/organizations/%s/projects/%s/", s.baseURL, s.orgSlug, p.Slug),
			})
			continue
		}

		// errorBudget consumido = (target - actual) / (100 - target).
		// Quando actual > target → consumed = negativo → clamp em 0.
		var consumed float64
		denom := 100 - s.target
		if denom > 0 {
			consumed = (s.target - actual) / denom
		}
		if consumed < 0 {
			consumed = 0
		}
		if consumed > 1 {
			consumed = 1
		}
		out = append(out, SLOStatus{
			ID:          p.ID,
			Name:        p.Name + " — availability SLO",
			Target:      s.target,
			Actual:      actual,
			ErrorBudget: consumed,
			PeriodDays:  s.period,
			Status:      classifyStatus(consumed),
			Source:      "sentry",
			URL:         fmt.Sprintf("%s/organizations/%s/projects/%s/", s.baseURL, s.orgSlug, p.Slug),
		})
	}
	return out, nil
}

func (s *SentryProvider) listProjects(ctx context.Context) ([]sentryProject, error) {
	var out []sentryProject
	uri := fmt.Sprintf("%s/api/0/organizations/%s/projects/", s.baseURL, s.orgSlug)
	if err := s.get(ctx, uri, &out); err != nil {
		return nil, fmt.Errorf("sentry list projects: %w", err)
	}
	return out, nil
}

func (s *SentryProvider) computeSLI(ctx context.Context, project string) (float64, error) {
	end := time.Now().UTC()
	start := end.AddDate(0, 0, -s.period)
	uri := fmt.Sprintf(
		"%s/api/0/organizations/%s/stats_v2/?project=%s&field=sum(quantity)&groupBy=outcome&start=%s&end=%s",
		s.baseURL, s.orgSlug, project,
		start.Format(time.RFC3339), end.Format(time.RFC3339),
	)
	var stats sentryStats
	if err := s.get(ctx, uri, &stats); err != nil {
		return 0, err
	}

	var accepted, total float64
	for _, g := range stats.Groups {
		total += g.Totals.SumQuantity
		if g.By.Outcome == "accepted" {
			accepted = g.Totals.SumQuantity
		}
	}
	if total == 0 {
		return 0, fmt.Errorf("no events")
	}
	return accepted / total * 100, nil
}

func (s *SentryProvider) get(ctx context.Context, uri string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.authTok)
	req.Header.Set("Accept", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sentry http %d: %s", resp.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}
