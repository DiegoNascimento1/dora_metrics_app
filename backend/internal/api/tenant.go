// Multi-tenant context resolution.
//
// Estratégia: o request carrega o tenant em UM dos lugares abaixo
// (avaliados nesta ordem):
//
//  1. Header HTTP X-Tenant-Slug  (preferido para apps server-to-server)
//  2. Subdomínio do Host          (ex: acme.dora.example.com → "acme")
//  3. Query string ?tenant=slug   (fallback dev/debug)
//
// O middleware resolve o slug → uuid via platform.tenant.slug e injeta
// `TenantID` + `TenantSlug` no context.Context para que handlers tenham
// acesso sem reabrir a query.
//
// Handlers que precisam de isolamento por tenant chamam `TenantFromContext(r.Context())`.
// Se ausente, deve retornar 401/403 — nunca processar como "tenant default".
//
// Compatibilidade: requests SEM cabeçalho/subdomínio/query passam pelo
// middleware sem TenantID no context. Handlers existentes que ainda
// derivam tenant via project_id continuam funcionando — migração é
// incremental. Em produção, considerar habilitar `RequireTenant` que
// recusa requests sem identidade explícita.
package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/storage"
	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

type tenantContextKey struct{}

// TenantInfo é o que vai no context.Context.
type TenantInfo struct {
	ID   uuid.UUID
	Slug string
}

// withTenant devolve um novo context.Context com o tenant injetado.
func withTenant(ctx context.Context, t TenantInfo) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, t)
}

// TenantFromContext devolve o tenant presente no context, ou (zero, false).
// Handlers chamam isto para filtrar queries por tenant_id.
func TenantFromContext(ctx context.Context) (TenantInfo, bool) {
	t, ok := ctx.Value(tenantContextKey{}).(TenantInfo)
	return t, ok
}

// TenantMiddleware retorna um middleware chi que tenta resolver o tenant
// e injetá-lo no context. Não bloqueia o request se não encontrar — só
// loga em debug. Use RequireTenant em rotas que exigem isolamento.
//
// Para evitar uma round-trip ao DB por request em produção, considere
// cachear (slug → TenantInfo) em memory com TTL pequeno (5-30s). MVP
// faz lookup direto.
func TenantMiddleware(db *storage.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			slug := extractTenantSlug(r)
			if slug == "" {
				next.ServeHTTP(w, r)
				return
			}

			q := queries.New(db.Pool)
			t, err := q.GetTenantBySlug(r.Context(), slug)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					log.Debug().Str("slug", slug).Msg("tenant slug not found")
					next.ServeHTTP(w, r)
					return
				}
				log.Error().Err(err).Str("slug", slug).Msg("tenant lookup failed")
				http.Error(w, "tenant lookup failed", http.StatusInternalServerError)
				return
			}

			ctx := withTenant(r.Context(), TenantInfo{ID: t.ID, Slug: t.Slug})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireTenant é um middleware adicional que recusa requests sem
// tenant no context (HTTP 401). Use em rotas que MANIPULAM dados
// específicos do tenant (POST/PATCH/DELETE).
func RequireTenant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := TenantFromContext(r.Context()); !ok {
			http.Error(w, "tenant required (header X-Tenant-Slug or subdomain)", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// extractTenantSlug aplica a estratégia documentada no header do arquivo.
func extractTenantSlug(r *http.Request) string {
	if h := strings.TrimSpace(r.Header.Get("X-Tenant-Slug")); h != "" {
		return h
	}
	// Subdomínio: split do Host (sem porta). Considera só o primeiro
	// label como slug se houver ≥ 3 labels (acme.dora.example.com).
	host := r.Host
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if labels := strings.Split(host, "."); len(labels) >= 3 {
		first := strings.ToLower(labels[0])
		if first != "www" && first != "api" && first != "localhost" {
			return first
		}
	}
	if q := strings.TrimSpace(r.URL.Query().Get("tenant")); q != "" {
		return q
	}
	return ""
}
