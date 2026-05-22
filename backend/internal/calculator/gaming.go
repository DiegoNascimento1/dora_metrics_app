// Detecção de "gaming" das métricas DORA.
//
// Risco conhecido no roadmap: deploy de MR vazio para inflar DF
// artificialmente. Este pacote computa estatísticas de tamanho médio
// dos MRs e sinaliza anomalias.
//
// Estratégia:
//
//   - MR \"trivial\" = additions + deletions <= TrivialThreshold (default 5).
//   - Se mais de X% dos deploys na janela vieram de MRs triviais,
//     sinalizamos como gaming.
//   - A heurística é deliberadamente conservadora — preferimos um
//     false-negative a uma acusação injusta.
//
// O sinal é exposto no /metrics como `gamingFlag` + `medianMRSize` para
// o frontend renderizar o aviso.
package calculator

import "sort"

// TrivialMRThreshold é o limiar default de additions+deletions para um
// MR ser considerado trivial.
const TrivialMRThreshold = 5

// GamingThresholdPercent é o % de deploys triviais que dispara o flag.
const GamingThresholdPercent = 50.0

// MRSize é o tamanho de um MR em linhas. Caller agrega tudo o que tem
// (additions + deletions do GitLab) — quando o backend não tem o dado,
// passa LinesUnknown=true.
type MRSize struct {
	Additions     int
	Deletions     int
	LinesUnknown  bool
}

// Total devolve additions+deletions ou 0 se desconhecido.
func (m MRSize) Total() int {
	if m.LinesUnknown {
		return 0
	}
	return m.Additions + m.Deletions
}

// GamingReport é o resultado da análise de uma janela.
type GamingReport struct {
	SampleSize       int     // n de MRs analisados (excluindo unknown)
	TrivialCount     int     // MRs com Total() <= threshold
	TrivialPercent   float64 // TrivialCount / SampleSize * 100
	MedianMRSize     int     // mediana de Total() — só linhas conhecidas
	GamingFlag       bool    // true se TrivialPercent >= GamingThresholdPercent
	// Razão textual já formatada para UI ("65% dos deploys vieram de MRs
	// triviais — possível gaming"). Vazio quando GamingFlag=false.
	Reason string
}

// AnalyzeGaming aplica a heurística. Devolve relatório vazio se a
// amostra (excluindo unknown) for menor que 4 — abaixo disso ruído
// estatístico domina.
func AnalyzeGaming(mrs []MRSize) GamingReport {
	known := make([]int, 0, len(mrs))
	for _, m := range mrs {
		if m.LinesUnknown {
			continue
		}
		known = append(known, m.Total())
	}
	if len(known) < 4 {
		return GamingReport{SampleSize: len(known)}
	}

	trivial := 0
	for _, t := range known {
		if t <= TrivialMRThreshold {
			trivial++
		}
	}

	pct := float64(trivial) / float64(len(known)) * 100

	rep := GamingReport{
		SampleSize:     len(known),
		TrivialCount:   trivial,
		TrivialPercent: pct,
		MedianMRSize:   medianOfInts(known),
	}
	if pct >= GamingThresholdPercent {
		rep.GamingFlag = true
		rep.Reason = formatGamingReason(trivial, len(known), pct)
	}
	return rep
}

func formatGamingReason(trivial, total int, pct float64) string {
	// Mensagem propositalmente neutra — "possível" + "considere
	// revisar". Não acusatória.
	return fmtPct(pct) + "% dos deploys (" + itoa(trivial) + "/" + itoa(total) +
		") vieram de MRs triviais (<=" + itoa(TrivialMRThreshold) +
		" linhas). Considere revisar se esses MRs representam mudança real."
}

// ---- helpers locais para evitar dep do "strconv" só por isto ----

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func fmtPct(p float64) string {
	// 1 casa decimal.
	whole := int(p)
	frac := int((p-float64(whole))*10 + 0.5)
	if frac >= 10 {
		whole++
		frac = 0
	}
	return itoa(whole) + "." + itoa(frac)
}

func medianOfInts(xs []int) int {
	sorted := make([]int, len(xs))
	copy(sorted, xs)
	sort.Ints(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
