// DatadogProvider lê SLOs do Datadog via API REST direta — Datadog v1
// SLO endpoint (`/api/v1/slo`).
//
// Auth: DD-API-KEY + DD-APPLICATION-KEY headers (env vars).
// Endpoint base configurável (us, eu, ddog-gov...).
//
// Convenção do scopeRef: tag filter no padrão Datadog
//   "service:dora-api"          → filtra SLOs por essa tag
//   "team:platform"             → filtra por outra tag
//   ""                          → sem filtro (devolve todos)
//
// O endpoint /api/v1/slo aceita `?tags_query=...` que faz exatamente isso.
package reliability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DatadogProvider implementa Provider via Datadog API v1.
type DatadogProvider struct {
	baseURL string
	apiKey  string
	appKey  string
	http    *http.Client
}

// NewDatadogProvider lê env DD_API_KEY, DD_APP_KEY, DD_SITE (default us).
func NewDatadogProvider() (*DatadogProvider, error) {
	api := os.Getenv("DD_API_KEY")
	app := os.Getenv("DD_APP_KEY")
	if api == "" || app == "" {
		return nil, fmt.Errorf("DD_API_KEY + DD_APP_KEY: %w", ErrNotConfigured)
	}
	site := os.Getenv("DD_SITE")
	if site == "" {
		site = "datadoghq.com"
	}
	return &DatadogProvider{
		baseURL: "https://api." + site,
		apiKey:  api,
		appKey:  app,
		http:    &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// Name implementa Provider.
func (d *DatadogProvider) Name() string { return "datadog" }

// datadogSLO é a shape mínima que extraímos do envelope da API v1.
//   https://docs.datadoghq.com/api/latest/service-level-objectives/#get-all-slos
type datadogSLO struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Type       string  `json:"type"` // "metric", "monitor", "time_slice"
	TargetSLO  float64 `json:"-"`    // preenchido manualmente
	Thresholds []struct {
		Target    float64 `json:"target"`
		Timeframe string  `json:"timeframe"` // "7d", "30d", "90d"
	} `json:"thresholds"`
}

// datadogSLOHistory devolve `actual` (sli_value) e error budget.
type datadogSLOHistory struct {
	Data struct {
		Overall struct {
			SLI        float64 `json:"sli_value"`     // 0..100
			Target     float64 `json:"target"`        // 0..100
			ErrorBudget struct {
				Remaining float64 `json:"remaining"` // 0..1 (1 = nada gasto)
			} `json:"error_budget"`
		} `json:"overall"`
	} `json:"data"`
}

// ListSLOs faz dois passos: lista SLOs e, para cada um, busca history
// (SLI atual + error budget). Limita concorrência fazendo serial — em
// produção, paralelizar se virar gargalo.
func (d *DatadogProvider) ListSLOs(ctx context.Context, scopeRef string) ([]SLOStatus, error) {
	q := url.Values{}
	if scopeRef != "" {
		q.Set("tags_query", scopeRef)
	}
	q.Set("limit", "100")

	listURL := d.baseURL + "/api/v1/slo?" + q.Encode()
	var listResp struct {
		Data []datadogSLO `json:"data"`
	}
	if err := d.get(ctx, listURL, &listResp); err != nil {
		return nil, fmt.Errorf("datadog list slos: %w", err)
	}

	out := make([]SLOStatus, 0, len(listResp.Data))
	now := time.Now().Unix()
	for _, s := range listResp.Data {
		periodDays := 30
		var target float64
		if len(s.Thresholds) > 0 {
			target = s.Thresholds[0].Target
			periodDays = parseTimeframeDays(s.Thresholds[0].Timeframe)
		}

		fromTs := now - int64(periodDays)*86400
		histURL := fmt.Sprintf(
			"%s/api/v1/slo/%s/history?from_ts=%d&to_ts=%d",
			d.baseURL, url.PathEscape(s.ID), fromTs, now,
		)
		var hist datadogSLOHistory
		if err := d.get(ctx, histURL, &hist); err != nil {
			// SLO sem histórico (ainda zero queries) — devolve placeholder.
			out = append(out, SLOStatus{
				ID: s.ID, Name: s.Name, Target: target,
				PeriodDays: periodDays, Source: "datadog",
				Status: "healthy",
				URL:    fmt.Sprintf("%s/slo?slo_id=%s", strings.Replace(d.baseURL, "https://api.", "https://app.", 1), s.ID),
			})
			continue
		}

		// Datadog devolve error_budget.remaining em [0, 1] onde 1 = intacto.
		// Convertemos para "consumido" = 1 - remaining.
		consumed := 1 - hist.Data.Overall.ErrorBudget.Remaining
		if consumed < 0 {
			consumed = 0
		}
		out = append(out, SLOStatus{
			ID:          s.ID,
			Name:        s.Name,
			Target:      hist.Data.Overall.Target,
			Actual:      hist.Data.Overall.SLI,
			ErrorBudget: consumed,
			PeriodDays:  periodDays,
			Status:      classifyStatus(consumed),
			Source:      "datadog",
			URL:         fmt.Sprintf("%s/slo?slo_id=%s", strings.Replace(d.baseURL, "https://api.", "https://app.", 1), s.ID),
		})
	}
	return out, nil
}

func (d *DatadogProvider) get(ctx context.Context, uri string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("DD-API-KEY", d.apiKey)
	req.Header.Set("DD-APPLICATION-KEY", d.appKey)
	req.Header.Set("Accept", "application/json")

	resp, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("datadog request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("datadog http %d: %s", resp.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}

// parseTimeframeDays mapeia "7d"/"30d"/"90d" pra int. Default 30.
func parseTimeframeDays(tf string) int {
	switch tf {
	case "7d":
		return 7
	case "30d":
		return 30
	case "90d":
		return 90
	}
	return 30
}
