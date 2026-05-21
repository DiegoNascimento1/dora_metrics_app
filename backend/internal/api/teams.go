package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

type teamDTO struct {
	ID        string  `json:"id"`
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	Color     *string `json:"color"`
	Emoji     *string `json:"emoji"`
	CreatedAt string  `json:"createdAt"`
}

type createTeamRequest struct {
	Tenant string `json:"tenant"`
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Color  string `json:"color"`
	Emoji  string `json:"emoji"`
}

type updateTeamRequest struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
	Emoji *string `json:"emoji"`
}

type assignProjectRequest struct {
	ProjectID string `json:"projectId"`
}

// ---- handlers ----

func (s *Server) handleListTeams() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, ok := s.requireTenant(w, r)
		if !ok {
			return
		}
		q := queries.New(s.db.Pool)
		rows, err := q.ListTeamsByTenant(r.Context(), tenant.ID)
		if err != nil {
			log.Error().Err(err).Msg("list teams")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		out := make([]teamDTO, 0, len(rows))
		for _, t := range rows {
			out = append(out, toTeamDTO(t))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Server) handleCreateTeam() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createTeamRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		req.Slug = strings.TrimSpace(req.Slug)
		req.Name = strings.TrimSpace(req.Name)
		if req.Tenant == "" || req.Slug == "" || req.Name == "" {
			http.Error(w, "tenant, slug and name required", http.StatusBadRequest)
			return
		}

		q := queries.New(s.db.Pool)
		tenant, err := q.GetTenantBySlug(r.Context(), req.Tenant)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "tenant not found", http.StatusNotFound)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		row, err := q.CreateTeam(r.Context(), queries.CreateTeamParams{
			TenantID: tenant.ID,
			Slug:     req.Slug,
			Name:     req.Name,
			Color:    nilIfEmpty(req.Color),
			Emoji:    nilIfEmpty(req.Emoji),
		})
		if err != nil {
			log.Error().Err(err).Msg("create team")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, toTeamDTO(row))
	}
}

func (s *Server) handleUpdateTeam() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "teamId"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		var req updateTeamRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		q := queries.New(s.db.Pool)
		row, err := q.UpdateTeam(r.Context(), queries.UpdateTeamParams{
			ID:    id,
			Name:  req.Name,
			Color: req.Color,
			Emoji: req.Emoji,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "team not found", http.StatusNotFound)
				return
			}
			log.Error().Err(err).Msg("update team")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, toTeamDTO(row))
	}
}

func (s *Server) handleDeleteTeam() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "teamId"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		q := queries.New(s.db.Pool)
		if err := q.DeleteTeam(r.Context(), id); err != nil {
			log.Error().Err(err).Msg("delete team")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleAssignProjectToTeam() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID, err := uuid.Parse(chi.URLParam(r, "teamId"))
		if err != nil {
			http.Error(w, "invalid team id", http.StatusBadRequest)
			return
		}
		var req assignProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			http.Error(w, "invalid projectId", http.StatusBadRequest)
			return
		}

		q := queries.New(s.db.Pool)
		project, err := q.AssignProjectToTeam(r.Context(), queries.AssignProjectToTeamParams{
			ProjectID: projectID,
			TeamID:    pgtype.UUID{Bytes: teamID, Valid: true},
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "project not found", http.StatusNotFound)
				return
			}
			log.Error().Err(err).Msg("assign project to team")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, toProjectDTO(project))
	}
}

func (s *Server) handleUnassignProjectFromTeam() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
		if err != nil {
			http.Error(w, "invalid project id", http.StatusBadRequest)
			return
		}
		q := queries.New(s.db.Pool)
		project, err := q.AssignProjectToTeam(r.Context(), queries.AssignProjectToTeamParams{
			ProjectID: projectID,
			TeamID:    pgtype.UUID{Valid: false},
		})
		if err != nil {
			log.Error().Err(err).Msg("unassign project from team")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, toProjectDTO(project))
	}
}

// ---- helpers ----

func toTeamDTO(t queries.PlatformTeam) teamDTO {
	return teamDTO{
		ID:        t.ID.String(),
		Slug:      t.Slug,
		Name:      t.Name,
		Color:     t.Color,
		Emoji:     t.Emoji,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
	}
}
