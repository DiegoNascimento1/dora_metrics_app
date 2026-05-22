package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// helper: monta um issue serializado no formato Jira Cloud REST v3.
func makeIssue(key, projectKey, summary, status, statusCat, issueType, priority string,
	labels []string, created, resolved string) json.RawMessage {
	payload := map[string]any{
		"key": key,
		"fields": map[string]any{
			"summary": summary,
			"project": map[string]any{"key": projectKey},
			"issuetype": map[string]any{"name": issueType},
			"status": map[string]any{
				"name": status,
				"statusCategory": map[string]any{"key": statusCat},
			},
			"priority": map[string]any{"name": priority},
			"labels":   labels,
			"created":  created,
		},
	}
	if resolved != "" {
		payload["fields"].(map[string]any)["resolutiondate"] = resolved
	}
	b, _ := json.Marshal(payload)
	return b
}

func TestRESTSource_Name(t *testing.T) {
	s := NewRESTSource("https://x", "e", "t")
	if got := s.Name(); got != "jira-rest" {
		t.Errorf("Name() = %q, want jira-rest", got)
	}
}

// Garante header Authorization Basic + Content-Type + path correto.
func TestSearchIssues_BasicAuthAndPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/rest/api/3/search/jql" {
			t.Errorf("path = %s, want /rest/api/3/search/jql", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
			t.Errorf("expected Basic auth, got %q", r.Header.Get("Authorization"))
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		_, _ = w.Write([]byte(`{"issues":[],"isLast":true}`))
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "alice@acme.com", "tok-1")
	_, err := s.SearchIssues(context.Background(), "issuetype = Incident", 0)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
}

// Paginação por nextPageToken: 3 páginas, segunda devolve token, última seta isLast=true.
func TestSearchIssues_PaginatesByNextPageToken(t *testing.T) {
	var page atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req searchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		n := page.Add(1)
		switch n {
		case 1:
			if req.NextPageToken != "" {
				t.Errorf("page 1 should have empty token, got %q", req.NextPageToken)
			}
			resp := searchResponse{
				NextPageToken: "tok-2",
				Issues: []json.RawMessage{
					makeIssue("ACME-1", "ACME", "first", "Open", "new", "Incident", "High",
						nil, "2026-05-01T10:00:00.000-0300", ""),
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case 2:
			if req.NextPageToken != "tok-2" {
				t.Errorf("page 2 token = %q, want tok-2", req.NextPageToken)
			}
			resp := searchResponse{
				NextPageToken: "tok-3",
				Issues: []json.RawMessage{
					makeIssue("ACME-2", "ACME", "second", "Open", "new", "Incident", "Medium",
						nil, "2026-05-02T10:00:00.000-0300", ""),
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case 3:
			if req.NextPageToken != "tok-3" {
				t.Errorf("page 3 token = %q, want tok-3", req.NextPageToken)
			}
			resp := searchResponse{
				IsLast: true,
				Issues: []json.RawMessage{
					makeIssue("ACME-3", "ACME", "third", "Done", "done", "Incident", "Low",
						nil, "2026-05-03T10:00:00.000-0300",
						"2026-05-04T12:00:00.000-0300"),
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			t.Errorf("unexpected extra request (page %d)", n)
		}
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	got, err := s.SearchIssues(context.Background(), "issuetype=Incident", 0)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d issues, want 3", len(got))
	}
	if got[0].Key != "ACME-1" || got[2].Key != "ACME-3" {
		t.Errorf("issues out of order: %+v", got)
	}
	if got[2].Resolved == nil {
		t.Errorf("expected resolved date on ACME-3")
	}
}

// limit > 0 corta a iteração antes de exaurir as páginas.
func TestSearchIssues_RespectsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// devolve sempre 100 issues + next token, simulando dataset enorme.
		issues := make([]json.RawMessage, 100)
		for i := range issues {
			issues[i] = makeIssue(fmt.Sprintf("ACME-%d", i+1), "ACME", "x", "Open", "new",
				"Incident", "High", nil, "2026-05-01T10:00:00.000-0300", "")
		}
		_ = json.NewEncoder(w).Encode(searchResponse{
			NextPageToken: "more",
			Issues:        issues,
		})
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	got, err := s.SearchIssues(context.Background(), "x", 5)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("got %d, want exactly 5 (limit)", len(got))
	}
}

// Página vazia: para imediatamente sem entrar em loop.
func TestSearchIssues_EmptyPageStopsLoop(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		// Devolve token mas nenhuma issue → loop deve parar.
		_, _ = w.Write([]byte(`{"nextPageToken":"x","issues":[]}`))
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	got, err := s.SearchIssues(context.Background(), "x", 0)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d issues, want 0", len(got))
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 HTTP call, got %d (loop não terminou)", calls.Load())
	}
}

// 401 vira APIError com StatusCode preservado pra retry logic upstream.
func TestSearchIssues_401Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errorMessages":["unauthorized"]}`))
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "bad-token")
	_, err := s.SearchIssues(context.Background(), "x", 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("status = %d, want 401", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Error(), "401") {
		t.Errorf("Error() = %q, want contains 401", apiErr.Error())
	}
}

// 429 com Retry-After: hoje o cliente não implementa retry interno, mas o
// APIError deve preservar o status code pra o caller (collector handler) decidir.
func TestSearchIssues_429RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"errorMessages":["rate limited"]}`))
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	_, err := s.SearchIssues(context.Background(), "x", 0)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", apiErr.StatusCode)
	}
}

// Parsing completo de campos: status, statusCategory, prioridade, labels,
// issuetype, created, resolved, project key, summary, raw preservado.
func TestSearchIssues_ParsesAllFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(searchResponse{
			IsLast: true,
			Issues: []json.RawMessage{
				makeIssue(
					"PROD-42",
					"PROD",
					"Page erroring 500",
					"Resolved",
					"done",
					"Incident",
					"Critical",
					[]string{"sev1", "p1"},
					"2026-05-19T08:30:00.000-0300",
					"2026-05-19T12:45:00.000-0300",
				),
			},
		})
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	got, err := s.SearchIssues(context.Background(), "x", 0)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d issues, want 1", len(got))
	}
	is := got[0]
	if is.Key != "PROD-42" {
		t.Errorf("Key = %q", is.Key)
	}
	if is.ProjectKey != "PROD" {
		t.Errorf("ProjectKey = %q", is.ProjectKey)
	}
	if is.Summary != "Page erroring 500" {
		t.Errorf("Summary = %q", is.Summary)
	}
	if is.IssueType != "Incident" {
		t.Errorf("IssueType = %q", is.IssueType)
	}
	if is.Status != "Resolved" {
		t.Errorf("Status = %q", is.Status)
	}
	if is.StatusCategory != "done" {
		t.Errorf("StatusCategory = %q", is.StatusCategory)
	}
	if is.Priority != "Critical" {
		t.Errorf("Priority = %q", is.Priority)
	}
	if len(is.Labels) != 2 || is.Labels[0] != "sev1" {
		t.Errorf("Labels = %+v", is.Labels)
	}
	if is.Created.IsZero() {
		t.Errorf("Created não parseou: %v", is.Created)
	}
	if is.Resolved == nil {
		t.Fatal("Resolved é nil, esperava ter valor")
	}
	wantRes := time.Date(2026, 5, 19, 12, 45, 0, 0, time.FixedZone("-0300", -3*3600))
	if !is.Resolved.Equal(wantRes) {
		t.Errorf("Resolved = %v, want %v", is.Resolved.UTC(), wantRes.UTC())
	}
	if len(is.Raw) == 0 {
		t.Errorf("Raw payload vazio")
	}
}

// Labels nil deve virar slice vazio (e não nil) — o caller espera len() válido.
func TestSearchIssues_LabelsNilBecomesEmptySlice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Sem labels no JSON.
		_, _ = w.Write([]byte(`{"isLast":true,"issues":[{
			"key":"X-1",
			"fields":{
				"summary":"no labels",
				"project":{"key":"X"},
				"issuetype":{"name":"Bug"},
				"status":{"name":"Open","statusCategory":{"key":"new"}},
				"priority":{"name":"Low"},
				"created":"2026-05-01T00:00:00.000-0300"
			}
		}]}`))
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	got, err := s.SearchIssues(context.Background(), "x", 0)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if got[0].Labels == nil {
		t.Error("Labels é nil; deveria ser slice vazio")
	}
	if len(got[0].Labels) != 0 {
		t.Errorf("Labels = %+v, want []", got[0].Labels)
	}
}

// Parsing de resolutiondate com formato RFC3339 (fallback) — Jira às vezes
// devolve com dois pontos no offset (`-03:00`) dependendo da versão.
func TestSearchIssues_ResolutionDateFallbackRFC3339(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"isLast":true,"issues":[{
			"key":"X-1",
			"fields":{
				"summary":"x",
				"project":{"key":"X"},
				"issuetype":{"name":"Bug"},
				"status":{"name":"Done","statusCategory":{"key":"done"}},
				"priority":{"name":"Low"},
				"labels":[],
				"created":"2026-05-01T00:00:00Z",
				"resolutiondate":"2026-05-02T10:00:00Z"
			}
		}]}`))
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	got, err := s.SearchIssues(context.Background(), "x", 0)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if got[0].Resolved == nil {
		t.Fatal("Resolved é nil; esperava parse RFC3339")
	}
	if got[0].Resolved.UTC() != time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC) {
		t.Errorf("Resolved = %v", got[0].Resolved.UTC())
	}
}

// resolutiondate vazia → Resolved continua nil.
func TestSearchIssues_EmptyResolutionDateIsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"isLast":true,"issues":[{
			"key":"X-1",
			"fields":{
				"summary":"x",
				"project":{"key":"X"},
				"issuetype":{"name":"Bug"},
				"status":{"name":"Open","statusCategory":{"key":"new"}},
				"priority":{"name":"Low"},
				"labels":[],
				"created":"2026-05-01T00:00:00Z",
				"resolutiondate":""
			}
		}]}`))
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	got, err := s.SearchIssues(context.Background(), "x", 0)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	if got[0].Resolved != nil {
		t.Errorf("Resolved = %v, want nil", got[0].Resolved)
	}
}

// Context com timeout muito curto causa erro de rede (não APIError).
func TestSearchIssues_ContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"isLast":true,"issues":[]}`))
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, err := s.SearchIssues(ctx, "x", 0)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("got APIError %v, expected network error", apiErr)
	}
}

// 500 → APIError com mensagem truncada do body.
func TestSearchIssues_5xxIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom internal"))
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	_, err := s.SearchIssues(context.Background(), "x", 0)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("status = %d, want 500", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Message, "boom") {
		t.Errorf("Message = %q, expected to contain body", apiErr.Message)
	}
}

// Decoder error: JSON inválido vira erro descritivo (não APIError).
func TestSearchIssues_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not-json`))
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	_, err := s.SearchIssues(context.Background(), "x", 0)
	if err == nil {
		t.Fatal("expected error on bad JSON, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v, want contains 'decode'", err)
	}
}

// SearchUsers — happy path: parsing + filtragem active/atlassian.
func TestSearchUsers_FiltersActiveAtlassianOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/users/search" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "alice" {
			t.Errorf("query = %s, want alice", r.URL.Query().Get("query"))
		}
		_, _ = w.Write([]byte(`[
			{"accountId":"557:abc","accountType":"atlassian","displayName":"Alice","emailAddress":"alice@acme.com","active":true},
			{"accountId":"557:bot","accountType":"app","displayName":"Bot","emailAddress":"bot@acme.com","active":true},
			{"accountId":"557:old","accountType":"atlassian","displayName":"Old","emailAddress":"old@acme.com","active":false}
		]`))
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	users, err := s.SearchUsers(context.Background(), "alice", 0)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("got %d users, want 1 (active atlassian)", len(users))
	}
	if users[0].DisplayName != "Alice" {
		t.Errorf("user = %+v", users[0])
	}
}

// SearchUsers — paginação por startAt/maxResults, para quando página
// vem com len < pageSize.
func TestSearchUsers_PaginatesByStartAt(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		startAt := r.URL.Query().Get("startAt")
		switch n {
		case 1:
			if startAt != "0" {
				t.Errorf("page 1 startAt = %s, want 0", startAt)
			}
			// devolve 100 → pede próxima.
			items := make([]string, 100)
			for i := range items {
				items[i] = fmt.Sprintf(`{"accountId":"a%d","accountType":"atlassian","displayName":"u%d","active":true}`, i, i)
			}
			_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
		case 2:
			if startAt != "100" {
				t.Errorf("page 2 startAt = %s, want 100", startAt)
			}
			_, _ = w.Write([]byte(`[{"accountId":"a101","accountType":"atlassian","displayName":"u101","active":true}]`))
		default:
			t.Errorf("unexpected page %d", n)
		}
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	users, err := s.SearchUsers(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) != 101 {
		t.Errorf("got %d, want 101", len(users))
	}
}

// SearchUsers — 401 vira APIError com status preservado.
func TestSearchUsers_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errorMessages":["bad token"]}`))
	}))
	defer srv.Close()
	s := NewRESTSource(srv.URL, "u", "bad")
	_, err := s.SearchUsers(context.Background(), "", 0)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("status = %d", apiErr.StatusCode)
	}
}

// SearchUsers — limit corta a iteração.
func TestSearchUsers_RespectsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		items := make([]string, 100)
		for i := range items {
			items[i] = fmt.Sprintf(`{"accountId":"a%d","accountType":"atlassian","displayName":"u%d","active":true}`, i, i)
		}
		_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
	}))
	defer srv.Close()
	s := NewRESTSource(srv.URL, "u", "t")
	users, err := s.SearchUsers(context.Background(), "", 5)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) != 5 {
		t.Errorf("got %d, want exactly 5", len(users))
	}
}

// MCPSource — confirma Name() correto e que falha cai pro fallback REST
// quando configurado.
func TestMCPSource_NameAndFallback(t *testing.T) {
	// endpoint inalcançável → erro de rede → deve cair no fallback.
	mcp := NewMCPSource("http://127.0.0.1:1", "x")
	if mcp.Name() != "atlassian-mcp" {
		t.Errorf("Name() = %q", mcp.Name())
	}

	// Sem fallback: erro propaga.
	_, err := mcp.SearchIssues(context.Background(), "x", 10)
	if err == nil {
		t.Fatal("expected error without fallback")
	}

	// Com fallback: usa REST mock.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"isLast":true,"issues":[{
			"key":"X-1",
			"fields":{
				"summary":"via fallback",
				"project":{"key":"X"},
				"issuetype":{"name":"Bug"},
				"status":{"name":"Open","statusCategory":{"key":"new"}},
				"priority":{"name":"Low"},
				"labels":[],
				"created":"2026-05-01T00:00:00Z"
			}
		}]}`))
	}))
	defer srv.Close()

	rest := NewRESTSource(srv.URL, "u", "t")
	mcpWithFallback := NewMCPSource("http://127.0.0.1:1", "x").WithFallback(rest)
	got, err := mcpWithFallback.SearchIssues(context.Background(), "x", 0)
	if err != nil {
		t.Fatalf("with fallback: %v", err)
	}
	if len(got) != 1 || got[0].Key != "X-1" {
		t.Errorf("got %+v", got)
	}
}

// Verifica que o body request inclui os campos default (status, summary, etc).
func TestSearchIssues_RequestIncludesFields(t *testing.T) {
	wantFields := map[string]bool{
		"summary": true, "status": true, "priority": true, "issuetype": true,
		"labels": true, "created": true, "resolutiondate": true, "project": true,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req searchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, f := range req.Fields {
			delete(wantFields, f)
		}
		if len(wantFields) != 0 {
			t.Errorf("missing fields in request: %+v", wantFields)
		}
		if req.JQL != "issuetype = Incident" {
			t.Errorf("JQL = %q", req.JQL)
		}
		if req.MaxResults != 100 {
			t.Errorf("MaxResults = %d, want 100", req.MaxResults)
		}
		_, _ = w.Write([]byte(`{"isLast":true,"issues":[]}`))
	}))
	defer srv.Close()

	s := NewRESTSource(srv.URL, "u", "t")
	_, _ = s.SearchIssues(context.Background(), "issuetype = Incident", 0)
}
