package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPing_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/version" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("PRIVATE-TOKEN") != "test-token" {
			t.Errorf("missing or wrong PRIVATE-TOKEN")
		}
		_, _ = w.Write([]byte(`{"version":"17.x"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestPing_Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"403 Forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "bad")
	err := c.Ping(context.Background())
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != 403 {
		t.Errorf("expected 403, got %d", apiErr.StatusCode)
	}
}

func TestListDeployments_PaginatesViaNextPage(t *testing.T) {
	// Devolve duas páginas de 2 deployments cada via X-Next-Page.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		var body []Deployment
		switch page {
		case "1":
			body = []Deployment{
				{ID: 1, Status: "success"},
				{ID: 2, Status: "success"},
			}
			w.Header().Set("X-Next-Page", "2")
		case "2":
			body = []Deployment{
				{ID: 3, Status: "success"},
				{ID: 4, Status: "success"},
			}
			w.Header().Set("X-Next-Page", "")
		default:
			t.Errorf("unexpected page %s", page)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	got, err := c.ListDeployments(context.Background(), "42", ListDeploymentsOpts{
		Status:       "success",
		UpdatedAfter: time.Now().Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 deployments, got %d", len(got))
	}
	for i, d := range got {
		if d.ID != i+1 {
			t.Errorf("position %d: got ID=%d want %d", i, d.ID, i+1)
		}
	}
}

func TestListEnvironments_PassesProjectID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Project id "278964" precisa estar no path.
		if want := "/api/v4/projects/278964/environments"; r.URL.Path != want {
			t.Errorf("path = %s, want %s", r.URL.Path, want)
		}
		_ = json.NewEncoder(w).Encode([]Environment{
			{ID: 1, Name: "production", State: "available"},
			{ID: 2, Name: "staging", State: "available"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	envs, err := c.ListEnvironments(context.Background(), "278964")
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(envs) != 2 {
		t.Fatalf("expected 2 envs, got %d", len(envs))
	}
}

func TestAPIError_FormatsMessage(t *testing.T) {
	e := &APIError{StatusCode: http.StatusTooManyRequests, Message: "rate limited"}
	const want = "gitlab api: 429 rate limited"
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// ListProjectMembers usa o endpoint /members/all (com herdados do grupo).
func TestListProjectMembers_HitsMembersAllEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/api/v4/projects/42/members/all") {
			t.Errorf("path = %s, want contains /api/v4/projects/42/members/all", r.URL.Path)
		}
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			t.Errorf("missing PRIVATE-TOKEN header")
		}
		_, _ = w.Write([]byte(`[
			{"id": 10, "username": "alice_dev", "name": "Alice", "public_email": "alice@acme.com", "web_url": "https://gitlab.example/alice_dev"},
			{"id": 11, "username": "bob_dev", "name": "Bob", "public_email": "", "web_url": "https://gitlab.example/bob_dev"}
		]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	members, err := c.ListProjectMembers(context.Background(), "42")
	if err != nil {
		t.Fatalf("ListProjectMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("got %d members, want 2", len(members))
	}
	if members[0].Username != "alice_dev" || members[0].ID != 10 {
		t.Errorf("first member = %+v", members[0])
	}
	if members[1].PublicEmail != "" {
		t.Errorf("expected empty email for bob, got %q", members[1].PublicEmail)
	}
}

// Paginação: 2 páginas via X-Next-Page header.
func TestListProjectMembers_Paginated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		switch page {
		case "1", "":
			w.Header().Set("X-Next-Page", "2")
			_, _ = w.Write([]byte(`[{"id":1,"username":"a","name":"A"}]`))
		case "2":
			w.Header().Set("X-Next-Page", "")
			_, _ = w.Write([]byte(`[{"id":2,"username":"b","name":"B"}]`))
		default:
			t.Errorf("unexpected page=%s", page)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	members, err := c.ListProjectMembers(context.Background(), "1")
	if err != nil {
		t.Fatalf("ListProjectMembers: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members across 2 pages, got %d", len(members))
	}
}

// Garante que o `per_page` default fica em 100 quando opts.PerPage <= 0.
func TestListDeployments_DefaultPerPageIs100(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("per_page")
		if n, _ := strconv.Atoi(got); n != 100 {
			t.Errorf("per_page = %s, want 100", got)
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	if _, err := c.ListDeployments(context.Background(), "1", ListDeploymentsOpts{}); err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
}
