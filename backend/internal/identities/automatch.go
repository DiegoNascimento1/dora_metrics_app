// Package identities oferece heurísticas para sugerir merges entre
// identidades não-linkadas (GitLab username vs Jira accountId).
//
// IMPORTANTE: o auto-match SUGERE, nunca decide. Cada sugestão tem um
// score de confiança; a aplicação humana é responsável pela aprovação
// via CLI ou UI.
//
// Documentação: ../../../docs/07-roadmap.md § Fase 3.5
package identities

import (
	"sort"
	"strings"

	"github.com/google/uuid"
)

// Identity é a projeção mínima que o auto-match consome.
// Mantida desacoplada do tipo gerado pelo sqlc para facilitar testes.
type Identity struct {
	ID               uuid.UUID
	Kind             string // "gitlab" | "jira"
	ExternalUsername string
	ExternalEmail    string // pode ser ""
}

// Suggestion descreve um par de identidades que provavelmente são a
// mesma pessoa, com a razão e o score [0,1].
type Suggestion struct {
	A      Identity
	B      Identity
	Reason string // "email_exact" | "username_exact"
	Score  float64
}

// Match recebe identidades não-linkadas e devolve sugestões de par
// (sempre cruzando kinds diferentes — gitlab×jira; nunca gitlab×gitlab).
// As sugestões vêm ordenadas por score desc, sem duplicar pares.
func Match(identities []Identity) []Suggestion {
	gitlab := []Identity{}
	jira := []Identity{}
	for _, id := range identities {
		switch id.Kind {
		case "gitlab":
			gitlab = append(gitlab, id)
		case "jira":
			jira = append(jira, id)
		}
	}

	out := []Suggestion{}
	seen := map[string]bool{}

	for _, g := range gitlab {
		for _, j := range jira {
			s, ok := score(g, j)
			if !ok {
				continue
			}
			key := pairKey(g.ID, j.ID)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, s)
		}
	}

	sort.SliceStable(out, func(i, k int) bool {
		return out[i].Score > out[k].Score
	})
	return out
}

// score compara duas identidades e devolve uma sugestão se houver match.
// Estratégias em ordem de confiança:
//
//  1. Email exato (case-insensitive) — score 1.0
//  2. Username exato (case-insensitive) — score 0.7
//
// Pode ser estendida com Levenshtein, embeddings etc na Fase 6.
func score(a, b Identity) (Suggestion, bool) {
	emailA := strings.ToLower(strings.TrimSpace(a.ExternalEmail))
	emailB := strings.ToLower(strings.TrimSpace(b.ExternalEmail))
	if emailA != "" && emailA == emailB {
		return Suggestion{A: a, B: b, Reason: "email_exact", Score: 1.0}, true
	}

	userA := strings.ToLower(strings.TrimSpace(a.ExternalUsername))
	userB := strings.ToLower(strings.TrimSpace(b.ExternalUsername))
	if userA != "" && userA == userB {
		return Suggestion{A: a, B: b, Reason: "username_exact", Score: 0.7}, true
	}

	return Suggestion{}, false
}

func pairKey(a, b uuid.UUID) string {
	if a.String() < b.String() {
		return a.String() + "|" + b.String()
	}
	return b.String() + "|" + a.String()
}
