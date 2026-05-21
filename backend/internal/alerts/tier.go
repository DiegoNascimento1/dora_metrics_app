// Package alerts cuida da detecção de mudanças de classificação DORA e do
// dispatch dos webhooks correspondentes. As regras vivem em platform.alert_rule
// e o histórico em platform.alert_event.
//
// Documentação: ../../docs/07-roadmap.md § Fase 4 § Engine de alertas.
package alerts

// TierOrder devolve a posição ordinal de uma classificação (maior = melhor).
// "insufficient_data" devolve -1 para que comparações com ela sempre indiquem
// "estado indefinido" — não fazem regressão nem promoção.
func TierOrder(tier string) int {
	switch tier {
	case "elite":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	}
	return -1
}

// IsRegression devolve true se current é estritamente pior que previous.
// Ambos precisam ser tiers conhecidos (ignora insufficient_data).
func IsRegression(previous, current string) bool {
	p, c := TierOrder(previous), TierOrder(current)
	if p < 0 || c < 0 {
		return false
	}
	return c < p
}

// IsChange devolve true se current difere de previous e ambos são conhecidos.
func IsChange(previous, current string) bool {
	p, c := TierOrder(previous), TierOrder(current)
	if p < 0 || c < 0 {
		return false
	}
	return p != c
}

// RuleMatchesChange decide se a regra deve disparar dada uma transição.
// kind == "tier_regression" só dispara em regressões; "tier_change" em qualquer
// mudança entre tiers conhecidos.
func RuleMatchesChange(kind, previous, current string) bool {
	switch kind {
	case "tier_regression":
		return IsRegression(previous, current)
	case "tier_change":
		return IsChange(previous, current)
	}
	return false
}
