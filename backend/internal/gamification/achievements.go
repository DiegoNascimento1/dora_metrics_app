// Package gamification implementa a camada opt-in de engajamento (streaks +
// conquistas) descrita em docs/07-roadmap.md § Design UX/UI § Gamificação.
//
// Princípio de design: conquistas são pra time/projeto, NUNCA pra indivíduo,
// e nunca são punitivas. "100 dias sem Change Failure" é celebração; o tier
// Low NÃO desbloqueia "frequent flyer". Ver os anti-padrões no roadmap.
package gamification

// Achievement é uma conquista desbloqueável.
type Achievement struct {
	Code        string `json:"code"`        // stable identifier para o front
	Title       string `json:"title"`       // título curto
	Description string `json:"description"` // contexto de 1 frase
	Icon        string `json:"icon"`        // Material Symbol Outlined
	UnlockedAt  string `json:"unlockedAt"`  // ISO date — "now" no MVP; futuro: timestamp real do unlock
}

// ProjectStats agrega os números que alimentam as regras de conquista.
type ProjectStats struct {
	DaysSinceLastIncident int    // -1 quando o projeto nunca teve incident
	CurrentClassification string // "elite" | "high" | "medium" | "low" | "insufficient_data"
	SampleSize            int    // n de deploys na janela
}

// EvaluateAchievements aplica as regras vigentes e devolve as conquistas
// desbloqueadas no momento da chamada. Função pura para facilitar testes.
//
// Cada regra é intencionalmente conservadora — preferimos desbloquear pouco
// e celebrar quando aparece a desbloquear muito e diluir o sinal.
func EvaluateAchievements(s ProjectStats, nowISO string) []Achievement {
	out := make([]Achievement, 0, 4)

	// Streaks só fazem sentido quando há histórico mínimo de monitoração.
	// Sem sample_size > 0 (deploys), o "streak sem incident" é só "sem dados".
	if s.SampleSize > 0 && s.DaysSinceLastIncident >= 0 {
		switch {
		case s.DaysSinceLastIncident >= 100:
			out = append(out, Achievement{
				Code:        "hundred_green_days",
				Title:       "100 Green Days",
				Description: "100+ dias consecutivos sem incident em produção",
				Icon:        "shield",
				UnlockedAt:  nowISO,
			})
		case s.DaysSinceLastIncident >= 30:
			out = append(out, Achievement{
				Code:        "thirty_green_days",
				Title:       "Steady Hand",
				Description: "30+ dias sem incident em produção",
				Icon:        "shield",
				UnlockedAt:  nowISO,
			})
		case s.DaysSinceLastIncident >= 7:
			out = append(out, Achievement{
				Code:        "week_streak",
				Title:       "Week Streak",
				Description: "7+ dias sem incident — boa cadência",
				Icon:        "local_fire_department",
				UnlockedAt:  nowISO,
			})
		}
	}

	if s.CurrentClassification == "elite" {
		out = append(out, Achievement{
			Code:        "current_elite_tier",
			Title:       "Elite Tier",
			Description: "Classificação combinada atual = Elite. Mantenha o ritmo.",
			Icon:        "workspace_premium",
			UnlockedAt:  nowISO,
		})
	}

	return out
}
