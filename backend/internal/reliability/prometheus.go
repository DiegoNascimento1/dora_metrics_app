// PrometheusProvider lê SLOs gerados por sloth (https://sloth.dev/) ou
// qualquer outra ferramenta que registre as métricas no padrão:
//
//   slo:sli_error:ratio_rate{period}{slo="...", service="..."}
//   slo:objective:ratio{slo="...", service="..."}
//   slo:current_burn_rate:ratio{slo="...", service="..."}
//
// O nome `slo:*` é a convenção do sloth, mas outros geradores (OpenSLO,
// Pyrra) seguem nomenclatura similar — basta ajustar via env
// PROMETHEUS_SLI_METRIC, PROMETHEUS_OBJECTIVE_METRIC, PROMETHEUS_BURN_METRIC.
//
// scopeRef = label filter em formato Prometheus (ex: `service="dora-api"`).
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

// PrometheusProvider lê SLOs via PromQL.
type PrometheusProvider struct {
	baseURL      string
	sliMetric    string // default "slo:sli_error:ratio_rate30d"
	objMetric    string // default "slo:objective:ratio"
	burnMetric   string // default "slo:current_burn_rate:ratio"
	periodDays   int    // janela usada nos rótulos das métricas (sloth: 30d default)
	http         *http.Client
}

// NewPrometheusProvider lê env PROMETHEUS_URL + overrides opcionais.
func NewPrometheusProvider() (*PrometheusProvider, error) {
	u := os.Getenv("PROMETHEUS_URL")
	if u == "" {
		return nil, fmt.Errorf("PROMETHEUS_URL: %w", ErrNotConfigured)
	}
	period := 30
	periodStr := os.Getenv("PROMETHEUS_SLO_PERIOD_DAYS")
	if periodStr != "" {
		// best-effort parse, default 30
		_, _ = fmt.Sscanf(periodStr, "%d", &period)
	}
	sli := envOr("PROMETHEUS_SLI_METRIC", fmt.Sprintf("slo:sli_error:ratio_rate%dd", period))
	obj := envOr("PROMETHEUS_OBJECTIVE_METRIC", "slo:objective:ratio")
	burn := envOr("PROMETHEUS_BURN_METRIC", "slo:current_burn_rate:ratio")
	return &PrometheusProvider{
		baseURL:    strings.TrimRight(u, "/"),
		sliMetric:  sli,
		objMetric:  obj,
		burnMetric: burn,
		periodDays: period,
		http:       &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (p *PrometheusProvider) Name() string { return "prometheus" }

type promInstantResp struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"` // "vector"
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"` // [timestamp, "stringValue"]
		} `json:"result"`
	} `json:"data"`
}

// ListSLOs faz 3 queries instant: SLI error rate, objective target,
// burn rate. Combina pelo label `slo` (id estável no sloth).
func (p *PrometheusProvider) ListSLOs(ctx context.Context, scopeRef string) ([]SLOStatus, error) {
	scopeFilter := ""
	if scopeRef != "" {
		scopeFilter = "{" + scopeRef + "}"
	}

	errResp, err := p.queryInstant(ctx, p.sliMetric+scopeFilter)
	if err != nil {
		return nil, err
	}
	objResp, err := p.queryInstant(ctx, p.objMetric+scopeFilter)
	if err != nil {
		return nil, err
	}

	// indexa objective por slo+service.
	type key struct{ slo, service string }
	objIdx := map[key]float64{}
	for _, r := range objResp.Data.Result {
		k := key{slo: r.Metric["slo"], service: r.Metric["service"]}
		objIdx[k] = parseFloat(r.Value[1])
	}

	out := make([]SLOStatus, 0, len(errResp.Data.Result))
	for _, r := range errResp.Data.Result {
		sliErr := parseFloat(r.Value[1]) // ex: 0.001 = 0.1% de erro
		actual := (1 - sliErr) * 100
		k := key{slo: r.Metric["slo"], service: r.Metric["service"]}
		target := objIdx[k] * 100 // sloth grava 0.999 → exibimos 99.9
		if target == 0 {
			target = 99.9 // fallback se a query objective falhou
		}

		var consumed float64
		denom := 100 - target
		if denom > 0 {
			consumed = (target - actual) / denom
		}
		if consumed < 0 {
			consumed = 0
		}
		if consumed > 1 {
			consumed = 1
		}

		name := r.Metric["slo"]
		if name == "" {
			name = r.Metric["service"]
		}
		out = append(out, SLOStatus{
			ID:          r.Metric["slo"] + "/" + r.Metric["service"],
			Name:        name,
			Target:      target,
			Actual:      actual,
			ErrorBudget: consumed,
			PeriodDays:  p.periodDays,
			Status:      classifyStatus(consumed),
			Source:      "prometheus",
		})
	}
	return out, nil
}

func (p *PrometheusProvider) queryInstant(ctx context.Context, query string) (*promInstantResp, error) {
	q := url.Values{}
	q.Set("query", query)
	uri := p.baseURL + "/api/v1/query?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("prometheus http %d: %s", resp.StatusCode, body)
	}
	var out promInstantResp
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if out.Status != "success" {
		return nil, fmt.Errorf("prometheus query returned status %s", out.Status)
	}
	return &out, nil
}

// parseFloat extrai o valor numérico de um instant vector. Prometheus
// devolve [timestamp_float, "value_as_string"].
func parseFloat(v any) float64 {
	s, ok := v.(string)
	if !ok {
		return 0
	}
	var f float64
	_, _ = fmt.Sscanf(s, "%f", &f)
	return f
}
