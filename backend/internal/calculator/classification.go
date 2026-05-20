package calculator

// Tiers de classificação DORA.
const (
	TierElite             = "elite"
	TierHigh              = "high"
	TierMedium            = "medium"
	TierLow               = "low"
	TierInsufficientData  = "insufficient_data"
)

// ClassifyDeploymentFrequency classifica uma frequência de deploys/dia
// nos tiers DORA. Esses thresholds são o ponto de partida; em produção,
// passam a vir da tabela platform.classification_threshold por tenant.
//
// Faixas:
//   - Elite:  >= 1.0  deploys/dia (>= 1 por dia útil)
//   - High:   >= 1/7  deploys/dia (~1/semana)
//   - Medium: >= 1/30 deploys/dia (~1/mês)
//   - Low:    < 1/30
//   - InsufficientData: df <= 0
func ClassifyDeploymentFrequency(perDay float64) string {
	switch {
	case perDay <= 0:
		return TierInsufficientData
	case perDay >= 1.0:
		return TierElite
	case perDay >= 1.0/7.0:
		return TierHigh
	case perDay >= 1.0/30.0:
		return TierMedium
	default:
		return TierLow
	}
}

// ClassifyLeadTime classifica o Lead Time mediano (em segundos) nos tiers DORA.
//
// Faixas (DORA Report 2023/2024):
//   - Elite:  < 1 hora      (3600s)
//   - High:   < 1 semana    (604800s)
//   - Medium: < 1 mês       (2592000s)
//   - Low:    >= 1 mês
//   - InsufficientData: nil
func ClassifyLeadTime(medianSeconds *int64) string {
	if medianSeconds == nil {
		return TierInsufficientData
	}
	const (
		hour  int64 = 3600
		week        = 7 * 24 * hour
		month       = 30 * 24 * hour
	)
	switch {
	case *medianSeconds < hour:
		return TierElite
	case *medianSeconds < week:
		return TierHigh
	case *medianSeconds < month:
		return TierMedium
	default:
		return TierLow
	}
}

// WorstOf devolve o pior tier (mais baixo) entre as classificações fornecidas.
// "Insufficient data" é tratado como ausente: se TODOS forem
// insufficient_data, retorna insufficient_data; caso contrário, ignora os
// insufficient e retorna o pior dos demais.
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
