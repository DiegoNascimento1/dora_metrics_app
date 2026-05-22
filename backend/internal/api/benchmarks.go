// Endpoint de benchmarks DORA da indústria.
//
// Os números expostos são os percentis do DORA Report 2024 (publicado pelo
// Google DORA). Servem como referência ANÔNIMA para comparação — não
// agregamos dados de outros clientes; isso fere multi-tenancy.
//
// Quando o cliente tem dataset suficiente (>= 90 dias de cron), o
// frontend pode mostrar "seu projeto está no percentil X" comparando
// contra esses números.
package api

import (
	"net/http"
)

// benchmark2024 é um snapshot dos percentis do DORA Report 2024.
// Atualize quando sair o report do próximo ano.
type industryBenchmarks struct {
	Source           string                       `json:"source"`
	Year             int                          `json:"year"`
	Notes            string                       `json:"notes"`
	Tiers            map[string]benchmarkTier     `json:"tiers"`
	IndustryAverages benchmarkPercentiles         `json:"industryAverages"`
}

type benchmarkTier struct {
	DeploymentFrequencyPerDay   float64 `json:"deploymentFrequencyPerDay"`
	LeadTimeForChangesSeconds   int64   `json:"leadTimeForChangesSeconds"`
	ChangeFailureRate           float64 `json:"changeFailureRate"`
	MTTRSeconds                 int64   `json:"mttrSeconds"`
	DescriptionPtBR             string  `json:"descriptionPtBR"`
}

type benchmarkPercentiles struct {
	P50 benchmarkTier `json:"p50"`
	P75 benchmarkTier `json:"p75"`
	P90 benchmarkTier `json:"p90"`
}

func currentBenchmarks() industryBenchmarks {
	const (
		hour = int64(3600)
		day  = 24 * hour
		week = 7 * day
		mo   = 30 * day
	)
	return industryBenchmarks{
		Source: "DORA Report 2024 (Google Cloud) + DevOps research baseline",
		Year:   2024,
		Notes: "Limites dos tiers replicam internal/calculator/classification.go. " +
			"Os percentis p50/p75/p90 são estimativas da indústria derivadas " +
			"do DORA Report — use como comparativo, não como meta absoluta.",
		Tiers: map[string]benchmarkTier{
			"elite": {
				DeploymentFrequencyPerDay: 1.0,
				LeadTimeForChangesSeconds: hour,
				ChangeFailureRate:         0.05,
				MTTRSeconds:               hour,
				DescriptionPtBR:           "Time elite: deploy diário, lead time < 1h, < 5% falha, recupera em < 1h.",
			},
			"high": {
				DeploymentFrequencyPerDay: 1.0 / 7,
				LeadTimeForChangesSeconds: week,
				ChangeFailureRate:         0.10,
				MTTRSeconds:               day,
				DescriptionPtBR:           "Time high: deploy semanal, lead time < 1 semana.",
			},
			"medium": {
				DeploymentFrequencyPerDay: 1.0 / 30,
				LeadTimeForChangesSeconds: mo,
				ChangeFailureRate:         0.20,
				MTTRSeconds:               week,
				DescriptionPtBR:           "Time medium: deploy mensal, lead time < 1 mês.",
			},
			"low": {
				DescriptionPtBR: "Time low: deploys raros, lead time > 1 mês.",
			},
		},
		IndustryAverages: benchmarkPercentiles{
			P50: benchmarkTier{
				DeploymentFrequencyPerDay: 0.5,
				LeadTimeForChangesSeconds: 3 * day,
				ChangeFailureRate:         0.12,
				MTTRSeconds:               12 * hour,
				DescriptionPtBR:           "Mediana global aproximada (DORA 2024).",
			},
			P75: benchmarkTier{
				DeploymentFrequencyPerDay: 2.5,
				LeadTimeForChangesSeconds: day,
				ChangeFailureRate:         0.08,
				MTTRSeconds:               4 * hour,
				DescriptionPtBR:           "Top quartil da indústria.",
			},
			P90: benchmarkTier{
				DeploymentFrequencyPerDay: 8.0,
				LeadTimeForChangesSeconds: 4 * hour,
				ChangeFailureRate:         0.04,
				MTTRSeconds:               hour,
				DescriptionPtBR:           "Top decil — empresas como Google/Netflix de referência.",
			},
		},
	}
}

func (s *Server) handleBenchmarks() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, currentBenchmarks())
	}
}
