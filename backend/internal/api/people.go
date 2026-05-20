package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/identities"
	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

// ---- DTOs ----

type personDTO struct {
	ID           string  `json:"id"`
	DisplayName  string  `json:"displayName"`
	PrimaryEmail *string `json:"primaryEmail"`
	AvatarURL    *string `json:"avatarUrl"`
	CreatedAt    string  `json:"createdAt"`
}

type personWithIdentitiesDTO struct {
	personDTO
	Identities []identityDTO `json:"identities"`
}

type identityDTO struct {
	ID               string  `json:"id"`
	Kind             string  `json:"kind"`
	ExternalID       *string `json:"externalId"`
	ExternalUsername string  `json:"externalUsername"`
	ExternalEmail    *string `json:"externalEmail"`
	PersonID         *string `json:"personId"`
	LinkedAt         *string `json:"linkedAt"`
	LinkedBy         *string `json:"linkedBy"`
}

type suggestionDTO struct {
	A      identityDTO `json:"a"`
	B      identityDTO `json:"b"`
	Reason string      `json:"reason"`
	Score  float64     `json:"score"`
}

type createPersonRequest struct {
	Tenant       string  `json:"tenant"`
	DisplayName  string  `json:"displayName"`
	PrimaryEmail string  `json:"primaryEmail"`
	AvatarURL    string  `json:"avatarUrl"`
	IdentityIDs  []string `json:"identityIds"` // opcional: identities a vincular já na criação
}

type linkRequest struct {
	PersonID string `json:"personId"`
	LinkedBy string `json:"linkedBy"`
}

// ---- handlers ----

func (s *Server) handleListPeople() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, ok := s.requireTenant(w, r)
		if !ok {
			return
		}

		q := queries.New(s.db.Pool)
		people, err := q.ListPeople(r.Context(), tenant.ID)
		if err != nil {
			log.Error().Err(err).Msg("list people")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		out := make([]personWithIdentitiesDTO, 0, len(people))
		for _, p := range people {
			row := personWithIdentitiesDTO{personDTO: toPersonDTO(p)}
			idents, err := q.ListIdentitiesByPerson(r.Context(), pgtype.UUID{Bytes: p.ID, Valid: true})
			if err != nil {
				log.Error().Err(err).Msg("list identities by person")
				continue
			}
			for _, id := range idents {
				row.Identities = append(row.Identities, toIdentityDTO(id))
			}
			out = append(out, row)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Server) handleCreatePerson() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createPersonRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if req.Tenant == "" || req.DisplayName == "" {
			http.Error(w, "tenant and displayName required", http.StatusBadRequest)
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

		row, err := q.CreatePerson(r.Context(), queries.CreatePersonParams{
			TenantID:     tenant.ID,
			DisplayName:  req.DisplayName,
			PrimaryEmail: nilIfEmpty(req.PrimaryEmail),
			AvatarUrl:    nilIfEmpty(req.AvatarURL),
		})
		if err != nil {
			log.Error().Err(err).Msg("create person")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Vincula identities opcionalmente fornecidas (one-shot create+link).
		for _, idStr := range req.IdentityIDs {
			id, err := uuid.Parse(idStr)
			if err != nil {
				continue
			}
			if _, err := q.LinkIdentityToPerson(r.Context(), queries.LinkIdentityToPersonParams{
				IdentityID: id,
				PersonID:   pgtype.UUID{Bytes: row.ID, Valid: true},
				LinkedBy:   ptr("api"),
			}); err != nil {
				log.Error().Err(err).Str("identity_id", idStr).Msg("link identity on person create")
			}
		}

		writeJSON(w, http.StatusCreated, toPersonDTO(row))
	}
}

func (s *Server) handleListUnlinkedIdentities() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, ok := s.requireTenant(w, r)
		if !ok {
			return
		}
		q := queries.New(s.db.Pool)
		rows, err := q.ListUnlinkedIdentities(r.Context(), tenant.ID)
		if err != nil {
			log.Error().Err(err).Msg("list unlinked")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		out := make([]identityDTO, 0, len(rows))
		for _, id := range rows {
			out = append(out, toIdentityDTO(id))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Server) handleAutomatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, ok := s.requireTenant(w, r)
		if !ok {
			return
		}
		q := queries.New(s.db.Pool)
		rows, err := q.ListUnlinkedIdentities(r.Context(), tenant.ID)
		if err != nil {
			log.Error().Err(err).Msg("list unlinked")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		src := make([]identities.Identity, 0, len(rows))
		dtoByID := make(map[uuid.UUID]identityDTO, len(rows))
		for _, r := range rows {
			var email string
			if r.ExternalEmail != nil {
				email = *r.ExternalEmail
			}
			src = append(src, identities.Identity{
				ID:               r.ID,
				Kind:             r.Kind,
				ExternalUsername: r.ExternalUsername,
				ExternalEmail:    email,
			})
			dtoByID[r.ID] = toIdentityDTO(r)
		}

		matches := identities.Match(src)
		out := make([]suggestionDTO, 0, len(matches))
		for _, m := range matches {
			out = append(out, suggestionDTO{
				A:      dtoByID[m.A.ID],
				B:      dtoByID[m.B.ID],
				Reason: m.Reason,
				Score:  m.Score,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// personMetricsDTO traz métricas DORA-style atribuídas a uma pessoa.
//
// Caveat ético: ver docs/01-dora-metrics.md e docs/07-roadmap.md § Fase 3.5.
// Use para coaching/mentoria, não para ranking punitivo.
type personMetricsDTO struct {
	PersonID              string `json:"personId"`
	WindowDays            int    `json:"windowDays"`
	DeploymentsTriggered  int64  `json:"deploymentsTriggered"`
	LeadTimeMedianSeconds *int64 `json:"leadTimeMedianSeconds"`
	LeadTimeSampleSize    int64  `json:"leadTimeSampleSize"`
	IncidentsLinked       int64  `json:"incidentsLinked"`
}

func (s *Server) handlePersonMetrics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		personID, err := uuid.Parse(chi.URLParam(r, "personId"))
		if err != nil {
			http.Error(w, "invalid person id", http.StatusBadRequest)
			return
		}
		windowDays := windowDays(r.URL.Query().Get("window"))
		since := time.Now().UTC().AddDate(0, 0, -windowDays)

		q := queries.New(s.db.Pool)

		if _, err := q.GetPerson(r.Context(), personID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "person not found", http.StatusNotFound)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		pidParam := pgtype.UUID{Bytes: personID, Valid: true}
		sinceParam := pgtype.Timestamptz{Time: since, Valid: true}

		deploys, err := q.CountDeploymentsByPersonInWindow(r.Context(),
			queries.CountDeploymentsByPersonInWindowParams{
				PersonID:      pidParam,
				FinishedSince: sinceParam,
			})
		if err != nil {
			log.Error().Err(err).Msg("count deployments by person")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		ltRow, err := q.LeadTimeMedianByPersonInWindow(r.Context(),
			queries.LeadTimeMedianByPersonInWindowParams{
				PersonID:      pidParam,
				FinishedSince: sinceParam,
			})
		if err != nil {
			log.Error().Err(err).Msg("lead time by person")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		incidents, err := q.CountIncidentsLinkedToPersonInWindow(r.Context(),
			queries.CountIncidentsLinkedToPersonInWindowParams{
				PersonID:      pidParam,
				FinishedSince: sinceParam,
			})
		if err != nil {
			log.Error().Err(err).Msg("incidents by person")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		var ltMedian *int64
		if ltRow.SampleSize > 0 {
			v := int64(coerceLT(ltRow.MedianSeconds))
			ltMedian = &v
		}

		writeJSON(w, http.StatusOK, personMetricsDTO{
			PersonID:              personID.String(),
			WindowDays:            windowDays,
			DeploymentsTriggered:  deploys,
			LeadTimeMedianSeconds: ltMedian,
			LeadTimeSampleSize:    ltRow.SampleSize,
			IncidentsLinked:       incidents,
		})
	}
}

// coerceLT lida com o tipo interface{} que o sqlc gera para COALESCE(...) AS median_seconds.
func coerceLT(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	}
	return 0
}

func (s *Server) handleLinkIdentity() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identityID, err := uuid.Parse(chi.URLParam(r, "identityId"))
		if err != nil {
			http.Error(w, "invalid identity id", http.StatusBadRequest)
			return
		}

		var req linkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		personID, err := uuid.Parse(req.PersonID)
		if err != nil {
			http.Error(w, "invalid personId", http.StatusBadRequest)
			return
		}
		linkedBy := req.LinkedBy
		if linkedBy == "" {
			linkedBy = "api"
		}

		q := queries.New(s.db.Pool)
		row, err := q.LinkIdentityToPerson(r.Context(), queries.LinkIdentityToPersonParams{
			IdentityID: identityID,
			PersonID:   pgtype.UUID{Bytes: personID, Valid: true},
			LinkedBy:   &linkedBy,
		})
		if err != nil {
			log.Error().Err(err).Msg("link identity")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Propaga person_id para eventos já gravados.
		if _, err := q.PropagatePersonToMergeRequests(r.Context()); err != nil {
			log.Warn().Err(err).Msg("propagate to MRs")
		}
		if _, err := q.PropagatePersonToDeployments(r.Context()); err != nil {
			log.Warn().Err(err).Msg("propagate to deployments")
		}

		writeJSON(w, http.StatusOK, toIdentityDTO(row))
	}
}

// ---- helpers ----

// requireTenant lê ?tenant=slug do query string e devolve o tenant, ou
// responde 400/404 e devolve ok=false.
func (s *Server) requireTenant(w http.ResponseWriter, r *http.Request) (queries.PlatformTenant, bool) {
	slug := r.URL.Query().Get("tenant")
	if slug == "" {
		http.Error(w, "tenant query param required", http.StatusBadRequest)
		return queries.PlatformTenant{}, false
	}
	q := queries.New(s.db.Pool)
	tenant, err := q.GetTenantBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "tenant not found", http.StatusNotFound)
			return queries.PlatformTenant{}, false
		}
		log.Error().Err(err).Msg("get tenant by slug")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return queries.PlatformTenant{}, false
	}
	return tenant, true
}

func toPersonDTO(p queries.PlatformPerson) personDTO {
	return personDTO{
		ID:           p.ID.String(),
		DisplayName:  p.DisplayName,
		PrimaryEmail: p.PrimaryEmail,
		AvatarURL:    p.AvatarUrl,
		CreatedAt:    p.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func toIdentityDTO(i queries.PlatformPersonIdentity) identityDTO {
	out := identityDTO{
		ID:               i.ID.String(),
		Kind:             i.Kind,
		ExternalID:       i.ExternalID,
		ExternalUsername: i.ExternalUsername,
		ExternalEmail:    i.ExternalEmail,
		LinkedBy:         i.LinkedBy,
	}
	if i.PersonID.Valid {
		id, err := uuid.FromBytes(i.PersonID.Bytes[:])
		if err == nil {
			s := id.String()
			out.PersonID = &s
		}
	}
	if i.LinkedAt.Valid {
		s := i.LinkedAt.Time.UTC().Format(time.RFC3339)
		out.LinkedAt = &s
	}
	return out
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptr(s string) *string { return &s }
