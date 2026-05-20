// Package calculator implementa o cálculo das 4 métricas DORA
// a partir dos dados consolidados no storage.
//
// Documentação detalhada em ../../../docs/01-dora-metrics.md
// e ../../../docs/06-data-model.md (queries canônicas).
package calculator

import (
	"encoding/json"
	"fmt"
)

// Tiers de classificação DORA.
const (
	TierElite            = "elite"
	TierHigh             = "high"
	TierMedium           = "medium"
	TierLow              = "low"
	TierInsufficientData = "insufficient_data"
)

// Thresholds carrega os limiares Elite/High/Medium para as 4 métricas DORA.
// Cada métrica tem 3 limiares; o tier "low" é tudo abaixo do limiar "medium"
// (ou acima, para métricas onde menor é melhor).
//
// Para Deployment Frequency: maior é melhor. perDay >= DFElite ⇒ elite,
// >= DFHigh ⇒ high, >= DFMedium ⇒ medium, < DFMedium ⇒ low.
//
// Para Lead Time, Change Failure Rate e MTTR: menor é melhor. value < Elite
// ⇒ elite, < High ⇒ high, < Medium ⇒ medium, >= Medium ⇒ low.
type Thresholds struct {
	DFElite  float64 `json:"df_elite_per_day"`
	DFHigh   float64 `json:"df_high_per_day"`
	DFMedium float64 `json:"df_medium_per_day"`

	LTElite  int64 `json:"lt_elite_seconds"`
	LTHigh   int64 `json:"lt_high_seconds"`
	LTMedium int64 `json:"lt_medium_seconds"`

	CFRElite  float64 `json:"cfr_elite"`
	CFRHigh   float64 `json:"cfr_high"`
	CFRMedium float64 `json:"cfr_medium"`

	MTTRElite  int64 `json:"mttr_elite_seconds"`
	MTTRHigh   int64 `json:"mttr_high_seconds"`
	MTTRMedium int64 `json:"mttr_medium_seconds"`
}

// DefaultThresholds devolve os limiares da DORA Report 2023/2024.
// Ver docs/01-dora-metrics.md § Benchmarks.
func DefaultThresholds() Thresholds {
	const (
		hour int64 = 3600
		day        = 24 * hour
		week       = 7 * day
		month      = 30 * day
	)
	return Thresholds{
		DFElite:  1.0,         // >= 1 deploy/dia
		DFHigh:   1.0 / 7.0,   // >= 1/semana
		DFMedium: 1.0 / 30.0,  // >= 1/mês

		LTElite:  hour,
		LTHigh:   week,
		LTMedium: month,

		CFRElite:  0.05,
		CFRHigh:   0.10,
		CFRMedium: 0.20,

		MTTRElite:  hour,
		MTTRHigh:   day,
		MTTRMedium: week,
	}
}

// FromJSON aceita JSONB vindo de platform.classification_threshold.config
// e mescla por cima dos defaults — campos omitidos no JSON ficam com o default.
func FromJSON(raw []byte) (Thresholds, error) {
	t := DefaultThresholds()
	if len(raw) == 0 {
		return t, nil
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return Thresholds{}, fmt.Errorf("decode thresholds: %w", err)
	}
	return t, nil
}

// ClassifyDeploymentFrequency classifica deploys/dia.
func ClassifyDeploymentFrequency(perDay float64, t Thresholds) string {
	switch {
	case perDay <= 0:
		return TierInsufficientData
	case perDay >= t.DFElite:
		return TierElite
	case perDay >= t.DFHigh:
		return TierHigh
	case perDay >= t.DFMedium:
		return TierMedium
	default:
		return TierLow
	}
}

// ClassifyLeadTime classifica o Lead Time mediano (segundos).
func ClassifyLeadTime(medianSeconds *int64, t Thresholds) string {
	if medianSeconds == nil {
		return TierInsufficientData
	}
	switch {
	case *medianSeconds < t.LTElite:
		return TierElite
	case *medianSeconds < t.LTHigh:
		return TierHigh
	case *medianSeconds < t.LTMedium:
		return TierMedium
	default:
		return TierLow
	}
}

// ClassifyChangeFailureRate classifica o CFR (0.0 a 1.0).
func ClassifyChangeFailureRate(rate *float64, t Thresholds) string {
	if rate == nil {
		return TierInsufficientData
	}
	switch {
	case *rate <= t.CFRElite:
		return TierElite
	case *rate <= t.CFRHigh:
		return TierHigh
	case *rate <= t.CFRMedium:
		return TierMedium
	default:
		return TierLow
	}
}

// ClassifyMTTR classifica o MTTR (segundos).
func ClassifyMTTR(meanSeconds *int64, t Thresholds) string {
	if meanSeconds == nil {
		return TierInsufficientData
	}
	switch {
	case *meanSeconds < t.MTTRElite:
		return TierElite
	case *meanSeconds < t.MTTRHigh:
		return TierHigh
	case *meanSeconds < t.MTTRMedium:
		return TierMedium
	default:
		return TierLow
	}
}

// WorstOf devolve o pior tier (mais baixo) entre as classificações fornecidas.
// "Insufficient data" é ignorado — só rebaixa quando há ao menos uma
// classificação real. Se TODAS forem insufficient, retorna insufficient.
func WorstOf(tiers ...string) string {
	rank := map[string]int{
		TierElite:  4,
		TierHigh:   3,
		TierMedium: 2,
		TierLow:    1,
	}

	worst := -1
	worstName := TierInsufficientData
	for _, t := range tiers {
		r, ok := rank[t]
		if !ok {
			continue
		}
		if worst == -1 || r < worst {
			worst = r
			worstName = t
		}
	}
	return worstName
}
