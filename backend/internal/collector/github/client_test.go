package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient cria um Client apontando para o httptest.Server.
func newTestClient(srv *httptest.Server) *Client {
	return NewClientWithBaseURL(srv.URL, "test-token")
}

// ---- ListDeployments ----

func TestListDeployments_HappyPath_SemPaginacao(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		deps := []Deployment{
			{ID: 1, Ref: "main", SHA: "abc123", Environment: "production",
				CreatedAt: time.Now(), UpdatedAt: time.Now()},
			{ID: 2, Ref: "main", SHA: "def456", Environment: "production",
				CreatedAt: time.Now(), UpdatedAt: time.Now()},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(deps)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	deps, err := client.ListDeployments(t.Context(), "owner", "repo", time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 2 {
		t.Errorf("esperava 2 deployments, obteve %d", len(deps))
	}
}

func TestListDeployments_Paginacao(t *testing.T) {
	// Servidor que retorna 2 páginas com Link: rel="next".
	page1 := []Deployment{{ID: 1, Ref: "main", SHA: "abc", CreatedAt: time.Now(), UpdatedAt: time.Now()}}
	page2 := []Deployment{{ID: 2, Ref: "main", SHA: "def", CreatedAt: time.Now(), UpdatedAt: time.Now()}}

	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			// Última página — sem Link header.
			json.NewEncoder(w).Encode(page2)
			return
		}
		// Primeira página — com Link next.
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/owner/repo/deployments?per_page=100&page=2>; rel="next"`, srvURL))
		json.NewEncoder(w).Encode(page1)
	}))
	defer srv.Close()
	srvURL = srv.URL

	client := newTestClient(srv)
	deps, err := client.ListDeployments(t.Context(), "owner", "repo", time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 2 {
		t.Errorf("esperava 2 deployments com paginação, obteve %d", len(deps))
	}
}

// ---- ListPullRequests ----

func TestListPullRequests_HappyPath(t *testing.T) {
	mergedAt := time.Now().Add(-1 * time.Hour)
	prs := []PullRequest{
		{
			Number:   1,
			Title:    "feat: add feature",
			State:    "closed",
			MergedAt: &mergedAt,
			UpdatedAt: time.Now(),
			MergeCommitSHA: strPtr("abc123"),
		},
		{
			// PR fechado mas não mergeado — deve ser ignorado.
			Number:   2,
			Title:    "fix: close without merge",
			State:    "closed",
			MergedAt: nil,
			UpdatedAt: time.Now(),
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(prs)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	result, err := client.ListPullRequests(t.Context(), "owner", "repo", time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("esperava 1 PR merged, obteve %d", len(result))
	}
	if result[0].Number != 1 {
		t.Errorf("esperava PR #1, obteve #%d", result[0].Number)
	}
}

// ---- Erros ----

func TestListDeployments_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.ListDeployments(t.Context(), "owner", "repo", time.Time{})
	if err == nil {
		t.Fatal("esperava erro 401, obteve nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("esperava *APIError, obteve %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("esperava status 401, obteve %d", apiErr.StatusCode)
	}
}

func TestListDeployments_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.ListDeployments(t.Context(), "owner", "repo", time.Time{})
	if err == nil {
		t.Fatal("esperava erro 429, obteve nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("esperava *APIError, obteve %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("esperava status 429, obteve %d", apiErr.StatusCode)
	}
}

// ---- GetDeploymentStatuses ----

func TestGetDeploymentStatuses_HappyPath(t *testing.T) {
	statuses := []DeploymentStatus{
		{ID: 1, State: "success", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: 2, State: "in_progress", CreatedAt: time.Now().Add(-1 * time.Minute), UpdatedAt: time.Now()},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(statuses)
	}))
	defer srv.Close()

	client := newTestClient(srv)
	result, err := client.GetDeploymentStatuses(t.Context(), "owner", "repo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("esperava 2 statuses, obteve %d", len(result))
	}
}

// ---- parseLinkNext ----

func TestParseLinkNext_ComNext(t *testing.T) {
	header := `<https://api.github.com/repos/owner/repo/deployments?per_page=100&page=2>; rel="next", <https://api.github.com/repos/owner/repo/deployments?per_page=100&page=5>; rel="last"`
	got := parseLinkNext(header)
	want := "https://api.github.com/repos/owner/repo/deployments?per_page=100&page=2"
	if got != want {
		t.Errorf("parseLinkNext: queria %q, obteve %q", want, got)
	}
}

func TestParseLinkNext_SemNext(t *testing.T) {
	header := `<https://api.github.com/repos/owner/repo/deployments?per_page=100&page=1>; rel="first"`
	got := parseLinkNext(header)
	if got != "" {
		t.Errorf("esperava vazio para link sem next, obteve %q", got)
	}
}

func TestParseLinkNext_Vazio(t *testing.T) {
	got := parseLinkNext("")
	if got != "" {
		t.Errorf("esperava vazio para header vazio, obteve %q", got)
	}
}

// ---- helpers ----

func strPtr(s string) *string { return &s }
