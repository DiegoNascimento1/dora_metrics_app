package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestExtractTenantSlug_FromHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Tenant-Slug", "acme")
	if got := extractTenantSlug(r); got != "acme" {
		t.Errorf("got %q, want acme", got)
	}
}

func TestExtractTenantSlug_FromSubdomain(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "acme.dora.example.com"
	if got := extractTenantSlug(r); got != "acme" {
		t.Errorf("got %q", got)
	}
}

func TestExtractTenantSlug_FromSubdomain_StripsPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "acme.dora.example.com:8080"
	if got := extractTenantSlug(r); got != "acme" {
		t.Errorf("got %q", got)
	}
}

func TestExtractTenantSlug_IgnoresWwwAndApi(t *testing.T) {
	for _, host := range []string{"www.example.com", "api.example.com"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = host
		if got := extractTenantSlug(r); got != "" {
			t.Errorf("host %s deveria não devolver slug, got %q", host, got)
		}
	}
}

func TestExtractTenantSlug_FromQueryFallback(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?tenant=acme", nil)
	r.Host = "localhost"
	if got := extractTenantSlug(r); got != "acme" {
		t.Errorf("got %q", got)
	}
}

func TestExtractTenantSlug_HeaderWins(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?tenant=fromquery", nil)
	r.Host = "fromsubdomain.dora.example.com"
	r.Header.Set("X-Tenant-Slug", "fromheader")
	if got := extractTenantSlug(r); got != "fromheader" {
		t.Errorf("got %q, want header to win", got)
	}
}

func TestExtractTenantSlug_TwoLabelHostReturnsEmpty(t *testing.T) {
	// example.com tem 2 labels — não há subdomínio pra extrair.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "example.com"
	if got := extractTenantSlug(r); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestRequireTenant_RejectsWithoutContext(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { called = true })
	handler := RequireTenant(next)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if called {
		t.Error("next handler não deveria ter sido chamado")
	}
}

func TestRequireTenant_PassesWithContext(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { called = true })
	handler := RequireTenant(next)

	id := uuid.New()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(withTenant(r.Context(), TenantInfo{ID: id, Slug: "acme"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
	if !called {
		t.Error("next handler deveria ter sido chamado")
	}
}

func TestTenantFromContext_RoundTrip(t *testing.T) {
	id := uuid.New()
	ctx := withTenant(t.Context(), TenantInfo{ID: id, Slug: "acme"})

	got, ok := TenantFromContext(ctx)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.ID != id || got.Slug != "acme" {
		t.Errorf("got %+v", got)
	}
}

func TestTenantFromContext_AbsentReturnsFalse(t *testing.T) {
	_, ok := TenantFromContext(t.Context())
	if ok {
		t.Error("expected ok=false on empty context")
	}
}
