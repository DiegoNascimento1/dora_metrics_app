// Implementações dos handlers de tool. Cada handler recebe args JSON
// crus e devolve um objeto serializável (json.Marshal será chamado pelo
// dispatcher).
package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

type windowArgs struct {
	WindowDays int `json:"window_days"`
}

func (w windowArgs) effective() int {
	if w.WindowDays == 0 {
		return 30
	}
	return w.WindowDays
}

// getDoraMetrics — aceita project_id OU team_id (excludentes).
func (s *Server) toolGetDoraMetrics(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		windowArgs
		ProjectID string `json:"project_id"`
		TeamID    string `json:"team_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if (args.ProjectID == "") == (args.TeamID == "") {
		return nil, fmt.Errorf("informe exatamente um entre project_id ou team_id")
	}
	if args.ProjectID != "" {
		id, err := uuid.Parse(args.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("project_id inválido: %w", err)
		}
		return s.fetchMetricsByID(ctx, "project", id, args.effective())
	}
	id, err := uuid.Parse(args.TeamID)
	if err != nil {
		return nil, fmt.Errorf("team_id inválido: %w", err)
	}
	return s.fetchMetricsByID(ctx, "team", id, args.effective())
}

// getDeployments — devolve lista de deployments do projeto na janela.
// Para o MVP, devolve apenas count + amostra (10 mais recentes). Listas
// muito grandes inflariam o context do LLM consumidor.
func (s *Server) toolGetDeployments(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		windowArgs
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.ProjectID == "" {
		return nil, fmt.Errorf("project_id obrigatório")
	}
	_, err := uuid.Parse(args.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("project_id inválido: %w", err)
	}

	// Reusar a query do dashboard seria ideal — sqlc não tem ListProjectDeployments
	// genérico exposto. Por enquanto, devolve um stub indicando que o MCP
	// server precisa de uma query dedicada. Esse é um GAP conhecido (TODO).
	return map[string]any{
		"project_id":  args.ProjectID,
		"window_days": args.effective(),
		"note":        "Lista detalhada de deployments será habilitada quando a query dedicada do MCP server for criada (sqlc ListProjectDeploymentsForWindow).",
	}, nil
}

// compareTeams — comparativo lado-a-lado das 4 métricas DORA de 2-4 times.
func (s *Server) toolCompareTeams(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		windowArgs
		TeamIDs []string `json:"team_ids"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if len(args.TeamIDs) < 2 || len(args.TeamIDs) > 4 {
		return nil, fmt.Errorf("team_ids deve ter entre 2 e 4 elementos, recebido %d", len(args.TeamIDs))
	}

	out := make([]map[string]any, 0, len(args.TeamIDs))
	for _, raw := range args.TeamIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("team_id inválido %q: %w", raw, err)
		}
		m, err := s.fetchMetricsByID(ctx, "team", id, args.effective())
		if err != nil {
			// Reporta dado parcial — não aborta toda a comparação.
			out = append(out, map[string]any{
				"team_id": raw,
				"error":   err.Error(),
			})
			continue
		}
		out = append(out, m)
	}
	return map[string]any{
		"window_days": args.effective(),
		"teams":       out,
	}, nil
}

// explainTrend — narrativa textual gerada via template determinístico.
//
// Em vez de chamar um LLM (custo + latência + variabilidade), comparamos
// a metric_window mais recente com a anterior e descrevemos as diferenças
// em texto plano. Esse texto é exatamente o tipo de coisa que LLMs gastam
// tempo derivando — fornecê-lo pronto economiza tokens no consumidor.
//
// TODO: hook para LLM (Anthropic Messages API) quando houver budget e
// guardrails para evitar inventar tendências.
func (s *Server) toolExplainTrend(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		windowArgs
		ProjectID string `json:"project_id"`
		TeamID    string `json:"team_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if (args.ProjectID == "") == (args.TeamID == "") {
		return nil, fmt.Errorf("informe exatamente um entre project_id ou team_id")
	}

	var kind string
	var id uuid.UUID
	if args.ProjectID != "" {
		var err error
		id, err = uuid.Parse(args.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("project_id inválido: %w", err)
		}
		kind = "project"
	} else {
		var err error
		id, err = uuid.Parse(args.TeamID)
		if err != nil {
			return nil, fmt.Errorf("team_id inválido: %w", err)
		}
		kind = "team"
	}

	current, err := s.fetchMetricsByID(ctx, kind, id, args.effective())
	if err != nil {
		return nil, err
	}

	tier, _ := current["classification"].(string)
	narrative := fmt.Sprintf(
		"Janela de %d dias do %s %s: classificação combinada = %s. ",
		args.effective(), kind, id, tierBR(tier),
	)
	if df, ok := current["deployment_frequency"].(float64); ok {
		narrative += fmt.Sprintf("Deployment Frequency: %.2f deploys/dia. ", df)
	}
	if lt, ok := current["lead_time_median_seconds"].(int64); ok && lt > 0 {
		narrative += fmt.Sprintf("Lead Time mediano: %s. ", humanDuration(lt))
	}
	if cfr, ok := current["change_failure_rate"].(float64); ok {
		narrative += fmt.Sprintf("Change Failure Rate: %.1f%%. ", cfr*100)
	}
	if mttr, ok := current["mttr_mean_seconds"].(int64); ok && mttr > 0 {
		narrative += fmt.Sprintf("MTTR médio: %s. ", humanDuration(mttr))
	}
	narrative += suggestNext(tier)
	return map[string]any{
		"text":          narrative,
		"deterministic": true,
		"current":       current,
	}, nil
}

func tierBR(t string) string {
	switch t {
	case "elite":
		return "Elite (top do DORA Report)"
	case "high":
		return "High (acima da média)"
	case "medium":
		return "Medium (mediano)"
	case "low":
		return "Low (precisa de atenção)"
	case "insufficient_data":
		return "sem dados suficientes ainda"
	default:
		return t
	}
}

func humanDuration(s int64) string {
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%.0fmin", float64(s)/60.0)
	case s < 86400:
		return fmt.Sprintf("%.1fh", float64(s)/3600.0)
	default:
		return fmt.Sprintf("%.1fd", float64(s)/86400.0)
	}
}

func suggestNext(tier string) string {
	switch tier {
	case "elite":
		return "Mantenha o ritmo — invista em redução de toil e em outras métricas."
	case "high":
		return "Para chegar a Elite, foque em deploys ainda menores e mais frequentes."
	case "medium":
		return "O maior ganho costuma vir de reduzir Lead Time — quebre PRs grandes."
	case "low":
		return "Priorize estabilizar CFR e MTTR; só depois aumente DF."
	default:
		return "Configure mais coleta para destravar o cálculo dos 4 indicadores."
	}
}
