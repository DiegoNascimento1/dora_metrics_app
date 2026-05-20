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
