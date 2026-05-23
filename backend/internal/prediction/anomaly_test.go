package prediction

import (
	"testing"
	"time"
)

func TestDetectAnomalies_SerieNormal(t *testing.T) {
	// Série com pequena variação — sem anomalias esperadas com threshold=2.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	samples := []MetricSample{
		{Date: base.AddDate(0, 0, 0), DeployFreq: 1.0, LeadTime: 24, CFR: 0.05, MTTR: 1.0},
		{Date: base.AddDate(0, 0, 1), DeployFreq: 1.1, LeadTime: 25, CFR: 0.04, MTTR: 0.9},
		{Date: base.AddDate(0, 0, 2), DeployFreq: 0.9, LeadTime: 23, CFR: 0.06, MTTR: 1.1},
		{Date: base.AddDate(0, 0, 3), DeployFreq: 1.0, LeadTime: 24, CFR: 0.05, MTTR: 1.0},
		{Date: base.AddDate(0, 0, 4), DeployFreq: 1.05, LeadTime: 24.5, CFR: 0.045, MTTR: 0.95},
		{Date: base.AddDate(0, 0, 5), DeployFreq: 0.95, LeadTime: 23.5, CFR: 0.055, MTTR: 1.05},
	}

	anomalies := DetectAnomalies(samples, 2.0)
	if len(anomalies) != 0 {
		t.Errorf("esperava 0 anomalias em série normal, obteve %d: %+v", len(anomalies), anomalies)
	}
}

func TestDetectAnomalies_SpikeDeployFreq(t *testing.T) {
	// Série com spike claro no último ponto de deploy frequency.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	samples := []MetricSample{
		{Date: base.AddDate(0, 0, 0), DeployFreq: 1.0},
		{Date: base.AddDate(0, 0, 1), DeployFreq: 1.0},
		{Date: base.AddDate(0, 0, 2), DeployFreq: 1.0},
		{Date: base.AddDate(0, 0, 3), DeployFreq: 1.0},
		{Date: base.AddDate(0, 0, 4), DeployFreq: 1.0},
		{Date: base.AddDate(0, 0, 5), DeployFreq: 20.0}, // spike claro
	}

	anomalies := DetectAnomalies(samples, 2.0)

	found := false
	for _, a := range anomalies {
		if a.Metric == "deploy_freq" && a.Direction == "spike" {
			found = true
			if a.ZScore < 2.0 {
				t.Errorf("z-score esperado >= 2.0, obteve %.2f", a.ZScore)
			}
		}
	}
	if !found {
		t.Error("esperava anomalia de spike em deploy_freq, não encontrada")
	}
}

func TestDetectAnomalies_DropCFR(t *testing.T) {
	// Série com drop claro de CFR (melhora súbita de qualidade).
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	samples := []MetricSample{
		{Date: base.AddDate(0, 0, 0), CFR: 0.20},
		{Date: base.AddDate(0, 0, 1), CFR: 0.21},
		{Date: base.AddDate(0, 0, 2), CFR: 0.19},
		{Date: base.AddDate(0, 0, 3), CFR: 0.20},
		{Date: base.AddDate(0, 0, 4), CFR: 0.22},
		{Date: base.AddDate(0, 0, 5), CFR: 0.001}, // drop abrupto
	}

	anomalies := DetectAnomalies(samples, 2.0)

	found := false
	for _, a := range anomalies {
		if a.Metric == "cfr" && a.Direction == "drop" {
			found = true
		}
	}
	if !found {
		t.Error("esperava anomalia de drop em cfr, não encontrada")
	}
}

func TestDetectAnomalies_CriticalSeverity(t *testing.T) {
	// Spike de >3σ deve ser marcado como "critical".
	// Série bem controlada + outlier extremo para garantir |z| > 3.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	samples := []MetricSample{
		{Date: base.AddDate(0, 0, 0), MTTR: 1.0},
		{Date: base.AddDate(0, 0, 1), MTTR: 1.0},
		{Date: base.AddDate(0, 0, 2), MTTR: 1.0},
		{Date: base.AddDate(0, 0, 3), MTTR: 1.0},
		{Date: base.AddDate(0, 0, 4), MTTR: 1.0},
		{Date: base.AddDate(0, 0, 5), MTTR: 1.0},
		{Date: base.AddDate(0, 0, 6), MTTR: 1.0},
		{Date: base.AddDate(0, 0, 7), MTTR: 1.0},
		{Date: base.AddDate(0, 0, 8), MTTR: 1.0},
		{Date: base.AddDate(0, 0, 9), MTTR: 500.0}, // outlier extremo → z >> 3
	}

	anomalies := DetectAnomalies(samples, 2.0)
	for _, a := range anomalies {
		if a.Metric == "mttr" && a.Direction == "spike" {
			if a.Severity != "critical" {
				t.Errorf("esperava severity=critical para z=%.2f, obteve %s", a.ZScore, a.Severity)
			}
			return
		}
	}
	t.Error("anomalia de mttr spike não encontrada")
}

func TestDetectAnomalies_SeriaPequena(t *testing.T) {
	// Série com menos de 3 pontos deve retornar nil.
	samples := []MetricSample{
		{Date: time.Now(), DeployFreq: 1.0},
		{Date: time.Now().AddDate(0, 0, 1), DeployFreq: 100.0},
	}
	anomalies := DetectAnomalies(samples, 2.0)
	if anomalies != nil {
		t.Errorf("esperava nil para série pequena, obteve %d anomalias", len(anomalies))
	}
}

func TestDetectAnomalies_ThresholdZeroUsaDefault(t *testing.T) {
	// threshold=0 deve usar DefaultAnomalyThreshold sem panic.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	samples := []MetricSample{
		{Date: base.AddDate(0, 0, 0), DeployFreq: 1.0},
		{Date: base.AddDate(0, 0, 1), DeployFreq: 1.0},
		{Date: base.AddDate(0, 0, 2), DeployFreq: 1.0},
		{Date: base.AddDate(0, 0, 3), DeployFreq: 1.0},
		{Date: base.AddDate(0, 0, 4), DeployFreq: 1.0},
		{Date: base.AddDate(0, 0, 5), DeployFreq: 50.0},
	}
	// Não deve entrar em panic.
	anomalies := DetectAnomalies(samples, 0)
	_ = anomalies
}
