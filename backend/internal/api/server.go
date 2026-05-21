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
	"github.com/dora-metrics-app/backend/internal/observability"
	"github.com/dora-metrics-app/backend/internal/storage"
)

// Server agrega dependências de runtime da API.
type Server struct {
	cfg   config.Config
	db    *storage.Pool
	asynq *asynq.Client
	mux   *chi.Mux
}

// NewServer constrói o servidor com rotas registradas.
func NewServer(cfg config.Config, db *storage.Pool, asynqClient *asynq.Client) *Server {
	observability.Register()
	s := &Server{cfg: cfg, db: db, asynq: asynqClient, mux: chi.NewRouter()}
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
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/healthz", s.handleHealthz())
	r.Get("/readyz", s.handleReadyz())
	r.Handle("/metrics", observability.Handler())

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/projects", s.handleListProjects())
		r.Get("/projects/{projectId}/metrics", s.handleProjectMetrics())
		r.Get("/projects/{projectId}/timeseries", s.handleProjectTimeseries())
		r.Get("/projects/{projectId}/deployments", s.handleProjectDeployments())
		r.Get("/projects/{projectId}/achievements", s.handleProjectAchievements())

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
