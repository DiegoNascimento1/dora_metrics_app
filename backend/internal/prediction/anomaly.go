package prediction

import (
	"math"
	"time"
)

// MetricSample é um ponto da série temporal multivariada para detecção
// de anomalias. Os campos zero indicam ausência de dados naquele ponto.
type MetricSample struct {
	Date       time.Time
	LeadTime   float64 // horas
	DeployFreq float64 // deploys/dia
	CFR        float64 // taxa [0,1]
	MTTR       float64 // horas
}

// AnomalyPoint representa um ponto detectado como anômalo na série.
type AnomalyPoint struct {
	Date      time.Time `json:"date"`
	Metric    string    `json:"metric"`
	Value     float64   `json:"value"`
	Mean      float64   `json:"mean"`
	StdDev    float64   `json:"std_dev"`
	ZScore    float64   `json:"z_score"`
	Direction string    `json:"direction"` // "spike" | "drop"
	Severity  string    `json:"severity"`  // "warning" (2-3σ) | "critical" (>3σ)
}

// DefaultAnomalyThreshold é o z-score padrão para detecção.
const DefaultAnomalyThreshold = 2.0

// metricExtractor extrai o valor numérico de um MetricSample para uma
// métrica específica.
type metricExtractor struct {
	name    string
	extract func(MetricSample) float64
}

var extractors = []metricExtractor{
	{"lead_time", func(s MetricSample) float64 { return s.LeadTime }},
	{"deploy_freq", func(s MetricSample) float64 { return s.DeployFreq }},
	{"cfr", func(s MetricSample) float64 { return s.CFR }},
	{"mttr", func(s MetricSample) float64 { return s.MTTR }},
}

// DetectAnomalies percorre a série e identifica pontos onde o z-score
// (desvio em relação à média da série) ultrapassa o threshold.
//
//   - threshold=0 → usa DefaultAnomalyThreshold (2.0)
//   - Pontos com valor zero são ignorados (ausência de dados).
//   - Direction "spike" = valor muito acima da média (ruim para LT, CFR, MTTR;
//     bom sinal para DF — mas reportado igual, o caller decide a interpretação).
//   - Direction "drop" = valor muito abaixo da média.
//   - Severity "warning" = |z| ∈ [threshold, 3σ); "critical" = |z| ≥ 3σ.
func DetectAnomalies(samples []MetricSample, threshold float64) []AnomalyPoint {
	if threshold <= 0 {
		threshold = DefaultAnomalyThreshold
	}
	if len(samples) < 3 {
		return nil
	}

	var anomalies []AnomalyPoint

	for _, ex := range extractors {
		// Coleta valores não-zero para calcular média e desvio-padrão.
		vals := make([]float64, 0, len(samples))
		for _, s := range samples {
			v := ex.extract(s)
			if v != 0 {
				vals = append(vals, v)
			}
		}
		if len(vals) < 3 {
			continue
		}

		mean, stddev := meanStdDev(vals)
		if stddev == 0 {
			// Série constante — nenhuma anomalia.
			continue
		}

		for _, s := range samples {
			v := ex.extract(s)
			if v == 0 {
				continue
			}
			z := (v - mean) / stddev
			absZ := math.Abs(z)
			if absZ < threshold {
				continue
			}

			direction := "spike"
			if z < 0 {
				direction = "drop"
			}

			severity := "warning"
			if absZ >= 3.0 {
				severity = "critical"
			}

			anomalies = append(anomalies, AnomalyPoint{
				Date:      s.Date,
				Metric:    ex.name,
				Value:     v,
				Mean:      mean,
				StdDev:    stddev,
				ZScore:    z,
				Direction: direction,
				Severity:  severity,
			})
		}
	}

	return anomalies
}

// meanStdDev calcula média e desvio-padrão populacional de uma série.
func meanStdDev(vals []float64) (mean, stddev float64) {
	n := float64(len(vals))
	for _, v := range vals {
		mean += v
	}
	mean /= n

	var variance float64
	for _, v := range vals {
		d := v - mean
		variance += d * d
	}
	variance /= n
	stddev = math.Sqrt(variance)
	return
}
