// Sub-componentes do Lead Time for Changes (Fase 6 — métricas auxiliares).
//
// O Lead Time end-to-end DORA é "do primeiro commit ao deploy em prod",
// mas só esse número não diz onde está o gargalo. Decompõe em:
//
//   Pickup Time     = first_commit_at → mr_opened_at
//                     (quanto tempo o código fica em branch local)
//   Code Review Time = mr_opened_at  → mr_merged_at
//                      (quanto tempo o PR fica aberto)
//   Deploy Lag      = mr_merged_at  → deployed_at
//                     (quanto tempo o merge fica em main sem ir pra prod)
//
// LeadTime = Pickup + Review + Deploy Lag.
//
// Útil para responder:
//   - "nosso Lead Time é alto: é PR demorando, ou pipeline lento?"
//   - "Pickup Time crescendo = scope creep dos MRs"
//
// Funções aqui são puras (sem DB) — caller passa os timestamps já
// resolvidos. Cálculo por MR; agregação (mediana) é responsabilidade
// do caller, que tem acesso a `metric_window`.
package calculator

import (
	"sort"
	"time"
)

// MRTimestamps captura os 4 pontos relevantes de um MR para decomposição
// do Lead Time. Todos opcionais — quando ausente, o sub-componente vira
// nil e a agregação ignora.
type MRTimestamps struct {
	FirstCommitAt *time.Time
	OpenedAt      *time.Time
	MergedAt      *time.Time
	DeployedAt    *time.Time
}

// LeadTimeBreakdown é o resultado por MR. Valores em segundos; nil =
// não pôde calcular (timestamp faltando).
type LeadTimeBreakdown struct {
	PickupSeconds      *int64
	ReviewSeconds      *int64
	DeployLagSeconds   *int64
	TotalLeadSeconds   *int64
}

// Decompose calcula os 4 sub-componentes para 1 MR. Se algum timestamp
// estiver faltando, o componente correspondente vira nil; os outros
// ainda são computados quando possível.
func Decompose(ts MRTimestamps) LeadTimeBreakdown {
	var b LeadTimeBreakdown
	if ts.FirstCommitAt != nil && ts.OpenedAt != nil {
		s := int64(ts.OpenedAt.Sub(*ts.FirstCommitAt).Seconds())
		if s >= 0 {
			b.PickupSeconds = &s
		}
	}
	if ts.OpenedAt != nil && ts.MergedAt != nil {
		s := int64(ts.MergedAt.Sub(*ts.OpenedAt).Seconds())
		if s >= 0 {
			b.ReviewSeconds = &s
		}
	}
	if ts.MergedAt != nil && ts.DeployedAt != nil {
		s := int64(ts.DeployedAt.Sub(*ts.MergedAt).Seconds())
		if s >= 0 {
			b.DeployLagSeconds = &s
		}
	}
	if ts.FirstCommitAt != nil && ts.DeployedAt != nil {
		s := int64(ts.DeployedAt.Sub(*ts.FirstCommitAt).Seconds())
		if s >= 0 {
			b.TotalLeadSeconds = &s
		}
	}
	return b
}

// LeadTimeAggregate é o resultado agregado de N MRs.
type LeadTimeAggregate struct {
	PickupMedianSeconds      *int64
	ReviewMedianSeconds      *int64
	DeployLagMedianSeconds   *int64
	TotalLeadMedianSeconds   *int64
	SampleSize               int
}

// AggregateLeadTime devolve as medianas dos 4 componentes em N MRs.
// Ignora valores nil em cada componente independentemente.
func AggregateLeadTime(mrs []LeadTimeBreakdown) LeadTimeAggregate {
	pickup := make([]int64, 0, len(mrs))
	review := make([]int64, 0, len(mrs))
	deploy := make([]int64, 0, len(mrs))
	total := make([]int64, 0, len(mrs))
	for _, m := range mrs {
		if m.PickupSeconds != nil {
			pickup = append(pickup, *m.PickupSeconds)
		}
		if m.ReviewSeconds != nil {
			review = append(review, *m.ReviewSeconds)
		}
		if m.DeployLagSeconds != nil {
			deploy = append(deploy, *m.DeployLagSeconds)
		}
		if m.TotalLeadSeconds != nil {
			total = append(total, *m.TotalLeadSeconds)
		}
	}
	return LeadTimeAggregate{
		PickupMedianSeconds:    medianInt64(pickup),
		ReviewMedianSeconds:    medianInt64(review),
		DeployLagMedianSeconds: medianInt64(deploy),
		TotalLeadMedianSeconds: medianInt64(total),
		SampleSize:             len(mrs),
	}
}

// medianInt64 devolve a mediana ou nil se a amostra estiver vazia.
// Mediana de N pares é a média dos 2 do meio — devolvemos arredondada
// (int64) porque segundos não precisam de subprecisão.
func medianInt64(xs []int64) *int64 {
	if len(xs) == 0 {
		return nil
	}
	sorted := make([]int64, len(xs))
	copy(sorted, xs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	n := len(sorted)
	var med int64
	if n%2 == 1 {
		med = sorted[n/2]
	} else {
		med = (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return &med
}
