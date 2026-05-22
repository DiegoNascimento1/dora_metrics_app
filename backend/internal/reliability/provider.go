// Package reliability abstrai o acesso a SLOs externos para a DORA
// Reliability v2 (Fase 6).
//
// Por que pluggable: SLO é território fragmentado — cada cliente já tem
// SEU provider (Datadog, Sentry, Prometheus+sloth, Google SRE YAML).
// Em vez de inventar mais um, lemos o que já existe e expomos no
// dashboard DORA junto das 4 métricas tradicionais.
//
// Cada provider mapeia "o que conta como SLO" para uma estrutura
// comum (SLOStatus) com:
//
//   - name           — nome humano do SLO
//   - target         — objetivo % (ex: 99.9)
//   - actual         — % atual no período
//   - errorBudget    — % consumido do error budget (0..1)
//   - period         — janela em dias (7, 30, 90)
//   - status         — "healthy" | "warning" | "breaching"
//
// O frontend exibe os SLOs como sub-cards na tela do projeto. O alert
// engine pode disparar quando errorBudget > 80%.
package reliability

import (
	"context"
	"errors"
	"fmt"
)

// SLOStatus é a projeção comum exposta pelo dashboard, independente
// do provider de origem.
type SLOStatus struct {
	ID          string  `json:"id"`           // ID nativo do provider
	Name        string  `json:"name"`         // nome humano
	Target      float64 `json:"target"`       // 0..100 (% de objetivo)
	Actual      float64 `json:"actual"`       // 0..100 (% medido)
	ErrorBudget float64 `json:"errorBudget"`  // 0..1 (1 = budget esgotado)
	PeriodDays  int     `json:"periodDays"`   // 7, 30, 90...
	Status      string  `json:"status"`       // healthy | warning | breaching
	Source      string  `json:"source"`       // "datadog" | "sentry" | "prometheus" | "yaml"
	URL         string  `json:"url,omitempty"` // link clicável pra UI do provider
}

// Provider é a interface implementada por cada backend de SLO.
type Provider interface {
	// ListSLOs devolve todos os SLOs vinculados ao escopo dado.
	// scopeRef é um identificador opaco que cada provider interpreta:
	//   - Datadog: service tag (ex: "service:dora-api")
	//   - Sentry: project slug
	//   - Prometheus: filtro de label (ex: "service=\"dora-api\"")
	//   - YAML: nome do arquivo dentro da pasta configurada
	ListSLOs(ctx context.Context, scopeRef string) ([]SLOStatus, error)

	// Name devolve o identificador do provider (telemetria, logs).
	Name() string
}

// classifyStatus computa healthy/warning/breaching a partir do error budget.
// Convenção:
//   < 50% consumido   → healthy
//   50%..80%          → warning
//   > 80%             → breaching (gera alerta)
func classifyStatus(errorBudget float64) string {
	switch {
	case errorBudget > 0.80:
		return "breaching"
	case errorBudget > 0.50:
		return "warning"
	default:
		return "healthy"
	}
}

// New constrói um Provider a partir do nome configurado. Os providers
// específicos têm seus próprios constructors (NewDatadogProvider etc)
// que leem env vars; aqui só o dispatch.
//
// Aceita:
//   - "datadog"
//   - "sentry"
//   - "prometheus" (cobre Prometheus + sloth + qualquer scraper de SLI)
//   - "yaml" (Google SRE-style YAML local — declarativo)
//   - ""    = no-op (todas as queries devolvem []SLOStatus{})
func New(kind string) (Provider, error) {
	switch kind {
	case "", "none":
		return NoopProvider{}, nil
	case "datadog":
		return NewDatadogProvider()
	case "sentry":
		return NewSentryProvider()
	case "prometheus":
		return NewPrometheusProvider()
	case "yaml":
		return NewYAMLProvider()
	default:
		return nil, fmt.Errorf("unknown reliability provider: %q", kind)
	}
}

// NoopProvider devolve lista vazia. Default seguro para deployments
// que ainda não escolheram um provider SLO.
type NoopProvider struct{}

func (NoopProvider) Name() string { return "noop" }
func (NoopProvider) ListSLOs(_ context.Context, _ string) ([]SLOStatus, error) {
	return []SLOStatus{}, nil
}

// ErrNotConfigured é retornado quando o provider precisa de env vars
// que não estão setadas.
var ErrNotConfigured = errors.New("reliability provider not configured")
