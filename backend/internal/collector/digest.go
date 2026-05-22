// Weekly digest — calcula, por projeto e por time ativos, a foto da
// semana anterior: deploys, incidents, tier atual vs anterior, top 3
// contributors (via person_id quando disponível). Resultado persiste em
// platform.digest_snapshot (migration 0011), idempotente por
// (tenant_id, scope_kind, scope_id, iso_week).
//
// Cron: "0 9 * * 1" (segunda 09:00 UTC). Sem retry agressivo — se uma
// execução falhar, a próxima cobre. asynq.MaxRetry(2).
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/rs/zerolog/log"
)

// digestRow é o shape que serializamos como JSON na coluna top_contributors.
type digestContributor struct {
	PersonID *uuid.UUID `json:"person_id,omitempty"`
	Name     string     `json:"name"`
	Deploys  int        `json:"deploys"`
}

// HandleDigestWeekly enfileira-se a si mesmo: lê projetos+times ativos e
// calcula o digest da semana ISO anterior.
//
// Estratégia minimalista: usa SQL puro via pgxpool (sem nova query sqlc)
// para evitar regenerar bindings no MVP. Quando estabilizar, migrar para
// sqlc.
func (h *Handlers) HandleDigestWeekly(ctx context.Context, _ *asynq.Task) error {
	now := time.Now().UTC()
	// "Semana anterior" = última segunda-feira completa até domingo.
	// time.ISOWeek(year, week) já considera ISO-8601.
	end := startOfISOWeek(now).Add(-time.Second) // domingo 23:59:59
	start := startOfISOWeek(end)                 // segunda da semana fechada
	year, week := start.ISOWeek()
	isoWeek := fmt.Sprintf("%04d-W%02d", year, week)

	rows, err := h.DB.Pool.Query(ctx, `
		SELECT id, tenant_id, 'project'::text AS scope_kind FROM platform.project
		WHERE is_active = true
		UNION ALL
		SELECT id, tenant_id, 'team'::text AS scope_kind FROM platform.team
	`)
	if err != nil {
		return fmt.Errorf("list active scopes: %w", err)
	}
	defer rows.Close()

	type scopeRef struct {
		id        uuid.UUID
		tenantID  uuid.UUID
		scopeKind string
	}
	var scopes []scopeRef
	for rows.Next() {
		var s scopeRef
		if err := rows.Scan(&s.id, &s.tenantID, &s.scopeKind); err != nil {
			return fmt.Errorf("scan scope: %w", err)
		}
		scopes = append(scopes, s)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate scopes: %w", err)
	}

	for _, s := range scopes {
		if err := h.computeDigestFor(ctx, s.tenantID, s.scopeKind, s.id, isoWeek, start, end); err != nil {
			log.Warn().Err(err).
				Str("scope", s.scopeKind).
				Str("id", s.id.String()).
				Msg("digest:weekly por escopo falhou — seguindo com próximos")
		}
	}
	log.Info().Str("iso_week", isoWeek).Int("scopes", len(scopes)).Msg("digest:weekly concluído")
	return nil
}

// computeDigestFor calcula UMA linha do digest_snapshot. Idempotente via
// ON CONFLICT (tenant_id, scope_kind, scope_id, iso_week).
//
// Para project: deployments + incidents do projeto. Para team: agregado
// de todos os projetos com team_id = scope_id.
func (h *Handlers) computeDigestFor(
	ctx context.Context,
	tenantID uuid.UUID,
	scopeKind string,
	scopeID uuid.UUID,
	isoWeek string,
	start, end time.Time,
) error {
	var depCount, incCount int
	var topRaw []byte

	if scopeKind == "project" {
		row := h.DB.Pool.QueryRow(ctx, `
			SELECT
				(SELECT COUNT(*) FROM metrics.deployment d
					WHERE d.project_id = $1 AND d.deployed_at >= $2 AND d.deployed_at < $3),
				(SELECT COUNT(*) FROM metrics.incident i
					WHERE i.project_id = $1 AND i.created_at >= $2 AND i.created_at < $3)
		`, scopeID, start, end)
		if err := row.Scan(&depCount, &incCount); err != nil {
			return fmt.Errorf("count project metrics: %w", err)
		}

		var err error
		topRaw, err = h.topContributorsForProject(ctx, scopeID, start, end)
		if err != nil {
			return err
		}
	} else {
		row := h.DB.Pool.QueryRow(ctx, `
			SELECT
				(SELECT COUNT(*) FROM metrics.deployment d
					JOIN platform.project p ON p.id = d.project_id
					WHERE p.team_id = $1 AND d.deployed_at >= $2 AND d.deployed_at < $3),
				(SELECT COUNT(*) FROM metrics.incident i
					JOIN platform.project p ON p.id = i.project_id
					WHERE p.team_id = $1 AND i.created_at >= $2 AND i.created_at < $3)
		`, scopeID, start, end)
		if err := row.Scan(&depCount, &incCount); err != nil {
			return fmt.Errorf("count team metrics: %w", err)
		}
		var err error
		topRaw, err = h.topContributorsForTeam(ctx, scopeID, start, end)
		if err != nil {
			return err
		}
	}

	// Tier atual e anterior: lookup do último metric_window 30d ao fim
	// da semana e ao fim da semana anterior.
	currentTier := h.tierAt(ctx, tenantID, scopeKind, scopeID, end)
	previousTier := h.tierAt(ctx, tenantID, scopeKind, scopeID, end.AddDate(0, 0, -7))
	tierDelta := tierRank(currentTier) - tierRank(previousTier)

	_, err := h.DB.Pool.Exec(ctx, `
		INSERT INTO platform.digest_snapshot (
			tenant_id, scope_kind, scope_id, iso_week,
			week_start, week_end,
			deployments_count, incidents_count,
			current_tier, previous_tier, tier_delta,
			top_contributors, computed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
		ON CONFLICT (tenant_id, scope_kind, scope_id, iso_week)
		DO UPDATE SET
			deployments_count = EXCLUDED.deployments_count,
			incidents_count = EXCLUDED.incidents_count,
			current_tier = EXCLUDED.current_tier,
			previous_tier = EXCLUDED.previous_tier,
			tier_delta = EXCLUDED.tier_delta,
			top_contributors = EXCLUDED.top_contributors,
			computed_at = now()
	`,
		tenantID, scopeKind, scopeID, isoWeek,
		start, end,
		depCount, incCount,
		nullIfEmpty(currentTier), nullIfEmpty(previousTier), tierDelta,
		topRaw,
	)
	return err
}

func (h *Handlers) topContributorsForProject(ctx context.Context, projectID uuid.UUID, start, end time.Time) ([]byte, error) {
	rows, err := h.DB.Pool.Query(ctx, `
		SELECT
			d.triggerer_person_id,
			COALESCE(p.display_name, d.triggered_by, 'desconhecido') AS name,
			COUNT(*)::int AS deploys
		FROM metrics.deployment d
		LEFT JOIN platform.person p ON p.id = d.triggerer_person_id
		WHERE d.project_id = $1
		  AND d.deployed_at >= $2 AND d.deployed_at < $3
		GROUP BY d.triggerer_person_id, p.display_name, d.triggered_by
		ORDER BY deploys DESC
		LIMIT 3
	`, projectID, start, end)
	if err != nil {
		return nil, fmt.Errorf("top contributors: %w", err)
	}
	defer rows.Close()
	return collectContributors(rows)
}

func (h *Handlers) topContributorsForTeam(ctx context.Context, teamID uuid.UUID, start, end time.Time) ([]byte, error) {
	rows, err := h.DB.Pool.Query(ctx, `
		SELECT
			d.triggerer_person_id,
			COALESCE(p.display_name, d.triggered_by, 'desconhecido') AS name,
			COUNT(*)::int AS deploys
		FROM metrics.deployment d
		JOIN platform.project pr ON pr.id = d.project_id
		LEFT JOIN platform.person p ON p.id = d.triggerer_person_id
		WHERE pr.team_id = $1
		  AND d.deployed_at >= $2 AND d.deployed_at < $3
		GROUP BY d.triggerer_person_id, p.display_name, d.triggered_by
		ORDER BY deploys DESC
		LIMIT 3
	`, teamID, start, end)
	if err != nil {
		return nil, fmt.Errorf("top contributors team: %w", err)
	}
	defer rows.Close()
	return collectContributors(rows)
}

func collectContributors(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]byte, error) {
	out := make([]digestContributor, 0, 3)
	for rows.Next() {
		var c digestContributor
		var pid *uuid.UUID
		if err := rows.Scan(&pid, &c.Name, &c.Deploys); err != nil {
			return nil, err
		}
		c.PersonID = pid
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// tierAt devolve o classification do último metric_window 30d cuja
// computed_at é ≤ moment. Vazio se sem dados.
func (h *Handlers) tierAt(ctx context.Context, tenantID uuid.UUID, scopeKind string, scopeID uuid.UUID, moment time.Time) string {
	var tier *string
	err := h.DB.Pool.QueryRow(ctx, `
		SELECT classification
		FROM metrics.metric_window
		WHERE tenant_id = $1 AND scope_kind = $2 AND scope_id = $3 AND window_days = 30
		  AND computed_at <= $4
		ORDER BY computed_at DESC
		LIMIT 1
	`, tenantID, scopeKind, scopeID, moment).Scan(&tier)
	if err != nil || tier == nil {
		return ""
	}
	return *tier
}

// startOfISOWeek devolve a segunda-feira 00:00 UTC da semana ISO de t.
func startOfISOWeek(t time.Time) time.Time {
	weekday := int(t.UTC().Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7 (ISO)
	}
	monday := t.UTC().AddDate(0, 0, -(weekday - 1))
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)
}

func tierRank(t string) int {
	switch t {
	case "elite":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
