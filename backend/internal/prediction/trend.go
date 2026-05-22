// Package prediction implementa detecção de tendência (regressão
// linear simples) sobre histórico de tier rank dos metric_window /
// metric_monthly_snapshot.
//
// Por que regressão linear pura (e não ML "de verdade"):
//
//   - Dataset é tipicamente 10-100 amostras (metric_window 30d
//     amostrados toda semana) — nada que justifique modelo complexo.
//   - Interpretabilidade > acurácia: o alert engine precisa explicar
//     "tier vem caindo a 0.05 ranks/semana há 8 semanas" para um
//     humano agir.
//   - Funciona com dados sintéticos: regressão linear não precisa de
//     training set rotulado.
//
// Quando houver 6+ meses de dados reais, evoluir para STL decomposition
// ou Prophet — mas a interface `Predict` deve permanecer estável.
//
// Funções aqui são puras (sem DB), facilitando testes e reuso.
package prediction

import (
	"math"
	"time"
)

// Sample é um ponto da série temporal. `T` é qualquer marcador
// monotonicamente crescente (timestamp Unix, número de dias do epoch,
// etc) — só importa que xs sejam crescentes.
type Sample struct {
	T    time.Time
	Tier string // "elite" | "high" | "medium" | "low" | "insufficient_data"
}

// Prediction é o resultado de Predict.
type Prediction struct {
	// SlopePerWeek = ranks por semana. Negativo = caindo.
	// Ex: -0.1 = perde ~1 nível a cada 10 semanas.
	SlopePerWeek float64 `json:"slopePerWeek"`

	// R2 = coeficiente de determinação 0..1. Quão bem a reta cabe nos
	// dados. < 0.3 → confiança baixa, sinal ruidoso.
	R2 float64 `json:"r2"`

	// CurrentTier é o tier observado mais recente.
	CurrentTier string `json:"currentTier"`

	// ProjectedTierIn é o tier extrapolado para `HorizonDays` à frente.
	ProjectedTierIn string `json:"projectedTierIn"`
	HorizonDays     int    `json:"horizonDays"`

	// Direction = "degrading" | "improving" | "stable".
	Direction string `json:"direction"`

	// Confidence = "low" | "medium" | "high" (combina R2 + sample size).
	Confidence string `json:"confidence"`

	// WillBreachIn é o número de DIAS estimado até cair para o
	// próximo tier inferior — nil se direção não-degradante ou se a
	// extrapolação levaria mais que `MaxBreachHorizonDays`.
	WillBreachInDays *int `json:"willBreachInDays,omitempty"`

	// Reason é texto humano pronto para UI/alerta. Vazio se sem
	// sinal estatisticamente relevante.
	Reason string `json:"reason,omitempty"`

	SampleSize int `json:"sampleSize"`
}

// HorizonDays default para projeção de tier futuro.
const HorizonDays = 30

// MinSamplesForPrediction é o mínimo de pontos NÃO-insufficient
// necessários para a regressão valer.
const MinSamplesForPrediction = 6

// MaxBreachHorizonDays é o teto da projeção de "quando vai degradar".
// Acima disso, consideramos irrelevante para alerta.
const MaxBreachHorizonDays = 180

// R2HighConfidence acima desse R² consideramos sinal forte.
const R2HighConfidence = 0.6

// R2MediumConfidence intermediário.
const R2MediumConfidence = 0.3

// SlopeStableEpsilon (ranks/semana). Slopes |x| < epsilon são "stable"
// — evita disparar alerta por ruído de classificação na fronteira.
const SlopeStableEpsilon = 0.02

// tierRankMap espelha internal/calculator/classification + internal/gamification.
var tierRankMap = map[string]float64{
	"elite":             4,
	"high":              3,
	"medium":            2,
	"low":               1,
	"insufficient_data": math.NaN(), // filtrado pela regressão
}

// rankToTier é o inverso (com clamp).
func rankToTier(r float64) string {
	switch {
	case r >= 3.5:
		return "elite"
	case r >= 2.5:
		return "high"
	case r >= 1.5:
		return "medium"
	default:
		return "low"
	}
}

// Predict calcula a tendência. Retorna Prediction com `Reason` vazio
// quando a amostra é pequena demais ou os dados não suportam conclusão.
func Predict(samples []Sample) Prediction {
	// Filtra insufficient_data e ordena por tempo (defensivo — o caller
	// já passa ordenado, mas não confio).
	valid := make([]Sample, 0, len(samples))
	for _, s := range samples {
		if _, ok := tierRankMap[s.Tier]; !ok {
			continue
		}
		if math.IsNaN(tierRankMap[s.Tier]) {
			continue
		}
		valid = append(valid, s)
	}

	p := Prediction{SampleSize: len(valid)}
	if len(valid) < MinSamplesForPrediction {
		p.Reason = "histórico insuficiente para previsão (mínimo " + itoa(MinSamplesForPrediction) + " pontos)"
		return p
	}

	// Define x em "dias desde o primeiro sample" para a regressão
	// produzir slope em ranks/dia. Multiplicaremos por 7 para
	// ranks/semana.
	first := valid[0].T
	xs := make([]float64, len(valid))
	ys := make([]float64, len(valid))
	for i, s := range valid {
		xs[i] = s.T.Sub(first).Hours() / 24
		ys[i] = tierRankMap[s.Tier]
	}

	slope, intercept, r2 := linearRegression(xs, ys)
	slopeWeek := slope * 7

	p.SlopePerWeek = slopeWeek
	p.R2 = r2
	p.CurrentTier = valid[len(valid)-1].Tier
	p.HorizonDays = HorizonDays

	// Projeção: y no x_atual + HorizonDays.
	xFuture := xs[len(xs)-1] + HorizonDays
	yFuture := intercept + slope*xFuture
	p.ProjectedTierIn = rankToTier(yFuture)

	switch {
	case slopeWeek < -SlopeStableEpsilon:
		p.Direction = "degrading"
	case slopeWeek > SlopeStableEpsilon:
		p.Direction = "improving"
	default:
		p.Direction = "stable"
	}

	switch {
	case r2 >= R2HighConfidence && len(valid) >= 10:
		p.Confidence = "high"
	case r2 >= R2MediumConfidence:
		p.Confidence = "medium"
	default:
		p.Confidence = "low"
	}

	// Quando vai cruzar pro próximo tier abaixo?
	if p.Direction == "degrading" && slope < 0 {
		currentRank := tierRankMap[p.CurrentTier]
		// próxima fronteira de baixo: floor(currentRank - 0.5) + 0.5
		nextThreshold := math.Floor(currentRank-0.5) + 0.5
		if nextThreshold > 0 {
			// y(x) = intercept + slope*x → resolve para x quando y = nextThreshold
			// Já estamos em y = currentRank approximately em xs[last].
			// Δdias = (nextThreshold - yNow) / slope. yNow ≈ ys[last].
			yNow := ys[len(ys)-1]
			deltaDays := (nextThreshold - yNow) / slope
			if deltaDays > 0 && deltaDays <= MaxBreachHorizonDays {
				d := int(math.Round(deltaDays))
				p.WillBreachInDays = &d
			}
		}
	}

	// Reason textual — só quando confidence ≥ medium e direction
	// degrading. Improving conta apenas no UI (sem alerta).
	if p.Direction == "degrading" && p.Confidence != "low" {
		base := "tier vem caindo a " + fmtFloat(-slopeWeek) + " ranks/semana ao longo de " +
			itoa(p.SampleSize) + " amostras (R²=" + fmtFloat(r2) + ")"
		if p.WillBreachInDays != nil {
			base += "; projeção: rebaixa em ~" + itoa(*p.WillBreachInDays) + " dias se continuar"
		}
		p.Reason = base
	}
	return p
}

// linearRegression calcula slope, intercept e R². Implementação OLS
// direta — sem pacote externo para evitar dependência por 30 LOC.
func linearRegression(xs, ys []float64) (slope, intercept, r2 float64) {
	n := float64(len(xs))
	if n < 2 {
		return 0, 0, 0
	}
	var sumX, sumY, sumXY, sumX2, sumY2 float64
	for i := range xs {
		sumX += xs[i]
		sumY += ys[i]
		sumXY += xs[i] * ys[i]
		sumX2 += xs[i] * xs[i]
		sumY2 += ys[i] * ys[i]
	}
	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0, sumY / n, 0
	}
	slope = (n*sumXY - sumX*sumY) / denom
	intercept = (sumY - slope*sumX) / n

	// R² = SSR / SST
	meanY := sumY / n
	var ssr, sst float64
	for i := range xs {
		pred := intercept + slope*xs[i]
		ssr += (pred - meanY) * (pred - meanY)
		sst += (ys[i] - meanY) * (ys[i] - meanY)
	}
	if sst == 0 {
		// série constante → R² indefinido. Devolvemos 1 se slope=0, 0 senão.
		if slope == 0 {
			return slope, intercept, 1
		}
		return slope, intercept, 0
	}
	r2 = ssr / sst
	if r2 < 0 {
		r2 = 0
	}
	if r2 > 1 {
		r2 = 1
	}
	return
}

// ---- helpers locais para evitar dep do "strconv" só para formatar ----

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

func fmtFloat(f float64) string {
	// 2 casas decimais.
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "0.00"
	}
	neg := f < 0
	if neg {
		f = -f
	}
	whole := int(f)
	frac := int((f-float64(whole))*100 + 0.5)
	if frac >= 100 {
		whole++
		frac = 0
	}
	out := itoa(whole) + "."
	if frac < 10 {
		out += "0"
	}
	out += itoa(frac)
	if neg {
		return "-" + out
	}
	return out
}
