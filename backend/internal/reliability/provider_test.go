package reliability

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------- Common ----------------

func TestClassifyStatus(t *testing.T) {
	cases := map[float64]string{
		0.0:  "healthy",
		0.30: "healthy",
		0.51: "warning",
		0.80: "warning",
		0.81: "breaching",
		1.0:  "breaching",
	}
	for budget, want := range cases {
		if got := classifyStatus(budget); got != want {
			t.Errorf("classifyStatus(%.2f) = %s, want %s", budget, got, want)
		}
	}
}

func TestNew_Dispatch(t *testing.T) {
	p, err := New("")
	if err != nil || p.Name() != "noop" {
		t.Errorf("noop default falhou: %v / %v", p, err)
	}
	p, err = New("none")
	if err != nil || p.Name() != "noop" {
		t.Errorf("none = noop falhou: %v / %v", p, err)
	}
	if _, err := New("nonexistent"); err == nil {
		t.Error("provider desconhecido deveria erro")
	}
}

func TestNoopProvider_AlwaysEmpty(t *testing.T) {
	slos, err := NoopProvider{}.ListSLOs(t.Context(), "anything")
	if err != nil {
		t.Fatal(err)
	}
	if len(slos) != 0 {
		t.Errorf("noop devolveu %d, want 0", len(slos))
	}
}

// ---------------- Datadog ----------------

func TestDatadog_ListSLOs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("DD-API-KEY") != "api" || r.Header.Get("DD-APPLICATION-KEY") != "app" {
			http.Error(w, "no auth", http.StatusForbidden)
			return
		}
		if strings.Contains(r.URL.Path, "/history") {
			fmt.Fprintf(w, `{"data":{"overall":{"sli_value":99.85,"target":99.9,"error_budget":{"remaining":0.4}}}}`)
			return
		}
		fmt.Fprintf(w, `{"data":[{"id":"abc","name":"API uptime","type":"monitor","thresholds":[{"target":99.9,"timeframe":"30d"}]}]}`)
	}))
	defer srv.Close()

	d := &DatadogProvider{
		baseURL: srv.URL, apiKey: "api", appKey: "app",
		http: srv.Client(),
	}
	slos, err := d.ListSLOs(t.Context(), "service:dora-api")
	if err != nil {
		t.Fatalf("ListSLOs: %v", err)
	}
	if len(slos) != 1 {
		t.Fatalf("got %d, want 1", len(slos))
	}
	got := slos[0]
	if got.Source != "datadog" || got.Name != "API uptime" {
		t.Errorf("got %+v", got)
	}
	if got.Target != 99.9 || got.Actual != 99.85 {
		t.Errorf("target/actual = %v/%v", got.Target, got.Actual)
	}
	// remaining=0.4 → consumed=0.6 → warning
	if got.Status != "warning" {
		t.Errorf("status = %s, want warning (consumed 60%%)", got.Status)
	}
}

func TestDatadog_NoHistoryReturnsPlaceholder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/history") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `{"data":[{"id":"x","name":"Fresh SLO","thresholds":[{"target":99.5,"timeframe":"7d"}]}]}`)
	}))
	defer srv.Close()
	d := &DatadogProvider{baseURL: srv.URL, apiKey: "api", appKey: "app", http: srv.Client()}
	slos, err := d.ListSLOs(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(slos) != 1 || slos[0].Status != "healthy" || slos[0].PeriodDays != 7 {
		t.Errorf("placeholder errado: %+v", slos)
	}
}

func TestDatadog_RequiresEnv(t *testing.T) {
	t.Setenv("DD_API_KEY", "")
	t.Setenv("DD_APP_KEY", "")
	if _, err := NewDatadogProvider(); err == nil {
		t.Error("expected error sem env vars")
	}
}

// ---------------- Sentry ----------------

func TestSentry_ListSLOs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "no auth", http.StatusForbidden)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/projects/"):
			fmt.Fprintf(w, `[{"id":"1","slug":"dora-api","name":"DORA API"}]`)
		case strings.Contains(r.URL.Path, "/stats_v2/"):
			// 9970 accepted / 10000 total = 99.70%. target=99.9 →
			// consumed = (99.9-99.7)/0.1 = 2.0 → clamp 1.0 → breaching.
			// Valores escolhidos pra ficarem longe da fronteira 0.5.
			fmt.Fprintf(w, `{"groups":[{"by":{"outcome":"accepted"},"totals":{"sum(quantity)":9970}},{"by":{"outcome":"invalid"},"totals":{"sum(quantity)":30}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := &SentryProvider{
		baseURL: srv.URL, authTok: "tok", orgSlug: "acme",
		target: 99.9, period: 30, http: srv.Client(),
	}
	slos, err := s.ListSLOs(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(slos) != 1 {
		t.Fatalf("got %d", len(slos))
	}
	got := slos[0]
	if got.Actual < 99.69 || got.Actual > 99.71 {
		t.Errorf("actual = %v, want ≈99.70", got.Actual)
	}
	// target 99.9, actual 99.70 → consumed > 1 → clamp 1 → breaching.
	if got.Status != "breaching" {
		t.Errorf("status = %s, want breaching, budget=%v", got.Status, got.ErrorBudget)
	}
}

func TestSentry_ScopeRefFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/projects/") {
			fmt.Fprintf(w, `[{"id":"1","slug":"keep","name":"K"},{"id":"2","slug":"skip","name":"S"}]`)
			return
		}
		fmt.Fprintf(w, `{"groups":[{"by":{"outcome":"accepted"},"totals":{"sum(quantity)":100}}]}`)
	}))
	defer srv.Close()
	s := &SentryProvider{baseURL: srv.URL, authTok: "t", orgSlug: "a", target: 99.0, period: 30, http: srv.Client()}
	slos, _ := s.ListSLOs(t.Context(), "keep")
	if len(slos) != 1 || !strings.Contains(slos[0].Name, "K") {
		t.Errorf("filter falhou: %+v", slos)
	}
}

func TestSentry_RequiresEnv(t *testing.T) {
	t.Setenv("SENTRY_AUTH_TOKEN", "")
	if _, err := NewSentryProvider(); err == nil {
		t.Error("expected error sem env")
	}
}

// ---------------- Prometheus ----------------

func TestPrometheus_ListSLOs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		switch {
		case strings.Contains(q, "sli_error"):
			fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"slo":"availability","service":"api"},"value":[1700000000,"0.001"]}
			]}}`)
		case strings.Contains(q, "objective"):
			fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"slo":"availability","service":"api"},"value":[1700000000,"0.999"]}
			]}}`)
		default:
			fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
		}
	}))
	defer srv.Close()

	p := &PrometheusProvider{
		baseURL: srv.URL,
		sliMetric: "slo:sli_error:ratio_rate30d", objMetric: "slo:objective:ratio",
		periodDays: 30, http: srv.Client(),
	}
	slos, err := p.ListSLOs(t.Context(), `service="api"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(slos) != 1 {
		t.Fatalf("got %d", len(slos))
	}
	got := slos[0]
	// sli_error=0.001 → actual = 99.9%
	if got.Actual < 99.89 || got.Actual > 99.91 {
		t.Errorf("actual = %v", got.Actual)
	}
	if got.Target < 99.89 || got.Target > 99.91 {
		t.Errorf("target = %v", got.Target)
	}
}

func TestPrometheus_RequiresURL(t *testing.T) {
	t.Setenv("PROMETHEUS_URL", "")
	if _, err := NewPrometheusProvider(); err == nil {
		t.Error("expected error sem env")
	}
}

func TestParseFloat(t *testing.T) {
	if v := parseFloat("3.14"); v < 3.13 || v > 3.15 {
		t.Errorf("parseFloat(3.14) = %v", v)
	}
	if v := parseFloat(42); v != 0 {
		t.Errorf("parseFloat(non-string) deveria 0, got %v", v)
	}
}

// ---------------- YAML ----------------

func TestYAML_LoadsAndComputesStatus(t *testing.T) {
	dir := t.TempDir()
	content := `
slos:
  - id: api-avail
    name: "API availability"
    service: dora-api
    target: 99.9
    periodDays: 30
    indicators:
      - actual: 99.50
`
	if err := os.WriteFile(filepath.Join(dir, "api.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &YAMLProvider{dir: dir}
	slos, err := p.ListSLOs(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(slos) != 1 {
		t.Fatalf("got %d", len(slos))
	}
	// 99.5 vs target 99.9: (99.9-99.5)/(100-99.9) = 4 → clamp 1 → breaching
	if slos[0].Status != "breaching" {
		t.Errorf("status = %s, want breaching", slos[0].Status)
	}
	if slos[0].Source != "yaml" {
		t.Errorf("source = %s", slos[0].Source)
	}
}

func TestYAML_MissingDirReturnsEmpty(t *testing.T) {
	p := &YAMLProvider{dir: filepath.Join(t.TempDir(), "nope")}
	slos, err := p.ListSLOs(t.Context(), "")
	if err != nil {
		t.Errorf("dir ausente não deveria errar: %v", err)
	}
	if len(slos) != 0 {
		t.Errorf("got %d", len(slos))
	}
}

func TestYAML_ScopeRefFiltersService(t *testing.T) {
	dir := t.TempDir()
	content := `
slos:
  - id: keep
    service: target
    target: 99.0
    indicators: [{actual: 100}]
  - id: skip
    service: other
    target: 99.0
    indicators: [{actual: 100}]
`
	_ = os.WriteFile(filepath.Join(dir, "x.yml"), []byte(content), 0o600)
	p := &YAMLProvider{dir: dir}
	slos, _ := p.ListSLOs(t.Context(), "target")
	if len(slos) != 1 || slos[0].ID != "keep" {
		t.Errorf("filter falhou: %+v", slos)
	}
}

// Sanity check da serialização JSON (frontend consome via API).
func TestSLOStatus_JSONShape(t *testing.T) {
	s := SLOStatus{
		ID: "x", Name: "n", Target: 99, Actual: 98, ErrorBudget: 0.5,
		PeriodDays: 30, Status: "warning", Source: "datadog",
	}
	b, _ := json.Marshal(s)
	for _, want := range []string{`"target":99`, `"actual":98`, `"errorBudget":0.5`, `"periodDays":30`, `"status":"warning"`, `"source":"datadog"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("json não tem %s: %s", want, b)
		}
	}
}
