// Package api expõe o servidor HTTP da plataforma.
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/hibiken/asynq"

	"github.com/dora-metrics-app/backend/internal/config"
	"github.com/dora-metrics-app/backend/internal/integrations/atlassian"
	"github.com/dora-metrics-app/backend/internal/observability"
	"github.com/dora-metrics-app/backend/internal/reliability"
	"github.com/dora-metrics-app/backend/internal/storage"
	"github.com/rs/zerolog/log"
)

// Server agrega dependências de runtime da API.
type Server struct {
	cfg         config.Config
	db          *storage.Pool
	asynq       *asynq.Client
	reliability reliability.Provider
	atlassian   *atlassian.Service
	mux         *chi.Mux
}

// NewServer constrói o servidor com rotas registradas.
func NewServer(cfg config.Config, db *storage.Pool, asynqClient *asynq.Client) *Server {
	observability.Register()

	// Reliability provider — falha de init não derruba o servidor;
	// cai pro Noop e a UI mostra "nenhum SLO configurado".
	relProvider, err := reliability.New(cfg.ReliabilityProvider)
	if err != nil {
		log.Warn().Err(err).Str("kind", cfg.ReliabilityProvider).
			Msg("reliability provider falhou ao inicializar — usando noop")
		relProvider = reliability.NoopProvider{}
	}

	// Atlassian OAuth — opcional. Sem CLIENT_ID/SECRET, o feature fica
	// desativado (status endpoint devolve available=false; UI esconde
	// o botão Connect).
	var atlSvc *atlassian.Service
	if cfg.AtlassianOAuth.ClientID != "" && cfg.AtlassianOAuth.ClientSecret != "" {
		cipher, cipherErr := atlassian.NewCipherFromEnv()
		oauthCfg := &atlassian.OAuthConfig{
			ClientID:     cfg.AtlassianOAuth.ClientID,
			ClientSecret: cfg.AtlassianOAuth.ClientSecret,
			RedirectURI:  cfg.AtlassianOAuth.RedirectURI,
			Scopes: []string{
				"read:jira-work",
				"read:jira-user",
				"offline_access",
			},
		}
		if oauthCfg.RedirectURI == "" {
			oauthCfg.RedirectURI = "http://localhost:8080/api/v1/integrations/atlassian/callback"
		}
		if cipherErr != nil {
			log.Warn().Err(cipherErr).Msg("atlassian OAuth: OAUTH_ENCRYPTION_KEY ausente — feature desligado")
		} else {
			atlSvc = atlassian.NewService(db, cipher, oauthCfg)
		}
	}

	s := &Server{
		cfg: cfg, db: db, asynq: asynqClient,
		reliability: relProvider,
		atlassian:   atlSvc,
		mux:         chi.NewRouter(),
	}
	s.routes()
	return s
}

// ServeHTTP delega ao chi.Mux para satisfazer http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	r := s.mux

	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(observability.HTTPMiddleware)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:4200"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Tenant-Slug"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	// Tenant resolution: injeta TenantInfo no context quando o request
	// carrega X-Tenant-Slug, subdomínio ou ?tenant=. Não bloqueia se
	// ausente — handlers existentes derivam tenant via project_id.
	r.Use(TenantMiddleware(s.db))

	r.Get("/healthz", s.handleHealthz())
	r.Get("/readyz", s.handleReadyz())
	r.Handle("/metrics", observability.Handler())

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/projects", s.handleListProjects())
		r.Get("/projects/{projectId}/metrics", s.handleProjectMetrics())
		r.Get("/projects/{projectId}/timeseries", s.handleProjectTimeseries())
		r.Get("/projects/{projectId}/deployments", s.handleProjectDeployments())
		r.Get("/projects/{projectId}/achievements", s.handleProjectAchievements())
		r.Get("/projects/{projectId}/export", s.handleProjectExport())

		r.Get("/people", s.handleListPeople())
		r.Post("/people", s.handleCreatePerson())
		r.Get("/people/{personId}/metrics", s.handlePersonMetrics())
		r.Get("/identities/unlinked", s.handleListUnlinkedIdentities())
		r.Get("/identities/automatch", s.handleAutomatch())
		r.Post("/identities/{identityId}/link", s.handleLinkIdentity())

		r.Get("/source-instances", s.handleListSourceInstances())
		r.Post("/source-instances", s.handleCreateSourceInstance())
		r.Delete("/source-instances/{sourceInstanceId}", s.handleDeleteSourceInstance())
		r.Post("/source-instances/test", s.handleTestConnection())

		r.Get("/teams", s.handleListTeams())
		r.Post("/teams", s.handleCreateTeam())
		r.Patch("/teams/{teamId}", s.handleUpdateTeam())
		r.Delete("/teams/{teamId}", s.handleDeleteTeam())
		r.Post("/teams/{teamId}/projects", s.handleAssignProjectToTeam())
		r.Get("/teams/{teamId}/metrics", s.handleTeamMetrics())
		r.Get("/teams/{teamId}/timeseries", s.handleTeamTimeseries())
		r.Get("/teams/{teamId}/digest", s.handleTeamDigest())
		r.Get("/projects/{projectId}/digest", s.handleProjectDigest())

		r.Get("/benchmarks", s.handleBenchmarks())
		r.Get("/reliability/info", s.handleReliabilityInfo())
		r.Get("/reliability/slos", s.handleReliabilitySLOs())
		r.Get("/projects/{projectId}/predict", s.handleProjectPredict())
		r.Get("/teams/{teamId}/predict", s.handleTeamPredict())

		// Atlassian OAuth 3LO — admin conecta a conta Jira via UI.
		r.Post("/integrations/atlassian/authorize", s.handleAtlassianAuthorize())
		r.Get("/integrations/atlassian/callback", s.handleAtlassianCallback())
		r.Get("/integrations/atlassian/status", s.handleAtlassianStatus())
		r.Delete("/integrations/atlassian/connection", s.handleAtlassianDisconnect())

		r.Post("/projects/{projectId}/unassign-team", s.handleUnassignProjectFromTeam())

		r.Get("/alert-rules", s.handleListAlertRules())
		r.Post("/alert-rules", s.handleCreateAlertRule())
		r.Patch("/alert-rules/{ruleId}", s.handleUpdateAlertRule())
		r.Delete("/alert-rules/{ruleId}", s.handleDeleteAlertRule())
		r.Get("/alert-events", s.handleListAlertEvents())
	})

	r.Route("/webhooks", func(r chi.Router) {
		r.Post("/gitlab", s.handleGitLabWebhook())
		r.Post("/jira", s.handleJiraWebhook())
	})
}
