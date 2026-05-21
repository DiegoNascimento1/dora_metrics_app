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
	EliteMonthsCount      int    // n de meses históricos com classificação Elite (drive de "First Elite Month")
	LeadTimeMedianSeconds *int64 // mediana atual em segundos (drive de "Speed Demon")
	LastIncidentsMTTR     []int64 // MTTR em segundos dos últimos N incidents resolvidos (drive de "Recovery Master")
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

	// 🚀 First Elite Month — primeiro mês inteiro classificado Elite na
	// história do projeto (snapshot mensal congelado em metric_monthly_snapshot).
	if s.EliteMonthsCount >= 1 {
		out = append(out, Achievement{
			Code:        "first_elite_month",
			Title:       "First Elite Month",
			Description: "Pelo menos um mês inteiro com classificação Elite",
			Icon:        "rocket_launch",
			UnlockedAt:  nowISO,
		})
	}

	// ⚡ Speed Demon — Lead Time mediano < 1h com amostra real (>= 4 deploys
	// na janela). É um proxy de "consistentemente rápido" enquanto não temos
	// histórico semanal pra validar "4 semanas consecutivas".
	if s.LeadTimeMedianSeconds != nil && *s.LeadTimeMedianSeconds < 3600 && s.SampleSize >= 4 {
		out = append(out, Achievement{
			Code:        "speed_demon",
			Title:       "Speed Demon",
			Description: "Lead Time mediano < 1h com >= 4 deploys na janela",
			Icon:        "bolt",
			UnlockedAt:  nowISO,
		})
	}

	// 🔁 Recovery Master — últimos 5 incidents resolvidos todos com MTTR < 1h.
	// Precisa de 5 incidents reais (não desbloqueia em projeto que mal viu
	// incidents — celebra recuperação repetida, não ausência de problemas).
	if len(s.LastIncidentsMTTR) >= 5 {
		allFast := true
		for _, mttr := range s.LastIncidentsMTTR {
			if mttr >= 3600 {
				allFast = false
				break
			}
		}
		if allFast {
			out = append(out, Achievement{
				Code:        "recovery_master",
				Title:       "Recovery Master",
				Description: "Últimos 5 incidents resolvidos com MTTR < 1h",
				Icon:        "autorenew",
				UnlockedAt:  nowISO,
			})
		}
	}

	return out
}
