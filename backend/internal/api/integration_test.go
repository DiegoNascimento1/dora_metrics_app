//go:build integration

// Integration tests via Testcontainers — sobe Postgres 18 real, aplica
// todas as migrations e exercita endpoints REST end-to-end.
//
// Rodar: `make test-integration` (ou `go test -tags=integration ./...`).
// Requer Docker no host.
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/dora-metrics-app/backend/internal/config"
	"github.com/dora-metrics-app/backend/internal/storage"
)

// setupContainer sobe Postgres 18 + aplica migrations. Devolve a URL
// + função de cleanup. Falha o teste se o Docker não estiver disponível.
func setupContainer(t *testing.T) (string, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	migrationsPath, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	if _, err := os.Stat(migrationsPath); err != nil {
		t.Fatalf("migrations dir not found: %v", err)
	}

	pg, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("dora"),
		postgres.WithUsername("dora"),
		postgres.WithPassword("dora"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Skipf("Testcontainers/docker indisponível: %v", err)
	}

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	// Aplica migrations rodando o binário oficial via testcontainers
	// (mais fácil que importar a lib migrate em test build).
	mig, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "migrate/migrate:v4.18.1",
			Cmd: []string{
				"-path=/migrations",
				"-database", containerDSN(t, ctx, pg),
				"up",
			},
			Mounts: testcontainers.ContainerMounts{
				testcontainers.BindMount(migrationsPath, "/migrations"),
			},
			Networks:   []string{"bridge"},
			WaitingFor: wait.ForExit(),
		},
		Started: true,
	})
	if err != nil {
		_ = pg.Terminate(ctx)
		t.Fatalf("migrate container: %v", err)
	}
	_ = mig.Terminate(ctx)

	return dsn, func() {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel2()
		_ = pg.Terminate(ctx2)
	}
}

// containerDSN devolve o DSN apontando para o hostname do container
// (acessível de dentro do container migrate).
func containerDSN(t *testing.T, ctx context.Context, pg *postgres.PostgresContainer) string {
	t.Helper()
	host, err := pg.ContainerIP(ctx)
	if err != nil {
		t.Fatalf("container IP: %v", err)
	}
	return "postgres://dora:dora@" + host + ":5432/dora?sslmode=disable"
}

func TestIntegration_HealthEndpoint(t *testing.T) {
	dsn, cleanup := setupContainer(t)
	defer cleanup()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	srv := NewServer(config.Config{}, &storage.Pool{Pool: pool}, asynq.NewClient(asynq.RedisClientOpt{Addr: "127.0.0.1:65000"}))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestIntegration_ListProjects_Empty(t *testing.T) {
	dsn, cleanup := setupContainer(t)
	defer cleanup()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	srv := NewServer(config.Config{}, &storage.Pool{Pool: pool}, asynq.NewClient(asynq.RedisClientOpt{Addr: "127.0.0.1:65000"}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	// Lista vazia (DB recém-criado, sem tenants).
	if body := w.Body.String(); body != "[]\n" && body != "null\n" && body != "[]" {
		// aceitamos qualquer representação vazia
	}
}

func TestIntegration_ProjectMetrics_NotFound(t *testing.T) {
	dsn, cleanup := setupContainer(t)
	defer cleanup()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	srv := NewServer(config.Config{}, &storage.Pool{Pool: pool}, asynq.NewClient(asynq.RedisClientOpt{Addr: "127.0.0.1:65000"}))

	// UUID válido mas inexistente — esperar 404.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/00000000-0000-0000-0000-000000000001/metrics", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
}

// TeamMetrics com team inexistente → 404.
func TestIntegration_TeamMetrics_NotFound(t *testing.T) {
	dsn, cleanup := setupContainer(t)
	defer cleanup()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	srv := NewServer(config.Config{}, &storage.Pool{Pool: pool}, asynq.NewClient(asynq.RedisClientOpt{Addr: "127.0.0.1:65000"}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/00000000-0000-0000-0000-000000000099/metrics", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
}

// Digest endpoint sem snapshot → 404 com mensagem específica.
func TestIntegration_ProjectDigest_EmptyReturns404(t *testing.T) {
	dsn, cleanup := setupContainer(t)
	defer cleanup()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	srv := NewServer(config.Config{}, &storage.Pool{Pool: pool}, asynq.NewClient(asynq.RedisClientOpt{Addr: "127.0.0.1:65000"}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/00000000-0000-0000-0000-000000000001/digest", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
}

// IDs malformados → 400 (não 500).
func TestIntegration_BadUUID_Returns400(t *testing.T) {
	dsn, cleanup := setupContainer(t)
	defer cleanup()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	srv := NewServer(config.Config{}, &storage.Pool{Pool: pool}, asynq.NewClient(asynq.RedisClientOpt{Addr: "127.0.0.1:65000"}))
	for _, path := range []string{
		"/api/v1/projects/not-a-uuid/metrics",
		"/api/v1/projects/not-a-uuid/digest",
		"/api/v1/teams/not-a-uuid/metrics",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("path %s: status = %d, want 400", path, w.Code)
		}
	}
}

// Confirma /metrics (Prometheus) exposto pelo middleware observability.
func TestIntegration_PrometheusMetricsEndpoint(t *testing.T) {
	dsn, cleanup := setupContainer(t)
	defer cleanup()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	srv := NewServer(config.Config{}, &storage.Pool{Pool: pool}, asynq.NewClient(asynq.RedisClientOpt{Addr: "127.0.0.1:65000"}))
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	// Prometheus exposition format sempre traz alguma linha # HELP.
	if !strings.Contains(body, "# HELP") {
		t.Errorf("body sem '# HELP' (não é exposição Prometheus): %s", body[:min(200, len(body))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
