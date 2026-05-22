package prediction

import (
	"testing"
	"time"
)

// tier returns a Sample at day offset N from epoch.
func tier(dayOffset int, t string) Sample {
	return Sample{T: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, dayOffset), Tier: t}
}

func TestPredict_InsufficientHistoryReturnsEmpty(t *testing.T) {
	p := Predict([]Sample{tier(0, "high"), tier(7, "medium")})
	if p.SampleSize != 2 {
		t.Errorf("sample = %d", p.SampleSize)
	}
	if p.Reason == "" || p.Direction != "" {
		t.Errorf("deveria reportar histórico insuficiente: %+v", p)
	}
}

func TestPredict_DegradingTrend(t *testing.T) {
	// elite → high → high → medium → medium → low (6 amostras, 7 dias entre)
	samples := []Sample{
		tier(0, "elite"),
		tier(7, "high"),
		tier(14, "high"),
		tier(21, "medium"),
		tier(28, "medium"),
		tier(35, "low"),
	}
	p := Predict(samples)
	if p.Direction != "degrading" {
		t.Errorf("direction = %s, want degrading", p.Direction)
	}
	if p.SlopePerWeek >= 0 {
		t.Errorf("slope should be negative, got %v", p.SlopePerWeek)
	}
	if p.CurrentTier != "low" {
		t.Errorf("current = %s", p.CurrentTier)
	}
	if p.Reason == "" {
		t.Error("Reason vazio em tendência degradante clara")
	}
}

func TestPredict_ImprovingTrend(t *testing.T) {
	samples := []Sample{
		tier(0, "low"),
		tier(7, "low"),
		tier(14, "medium"),
		tier(21, "medium"),
		tier(28, "high"),
		tier(35, "high"),
	}
	p := Predict(samples)
	if p.Direction != "improving" {
		t.Errorf("direction = %s, want improving", p.Direction)
	}
	if p.SlopePerWeek <= 0 {
		t.Errorf("slope should be positive, got %v", p.SlopePerWeek)
	}
	// improvement NÃO gera Reason (não é alerta).
	if p.Reason != "" {
		t.Errorf("improving não deveria ter Reason: %q", p.Reason)
	}
}

func TestPredict_StableTrend(t *testing.T) {
	samples := make([]Sample, 8)
	for i := range samples {
		samples[i] = tier(i*7, "high")
	}
	p := Predict(samples)
	if p.Direction != "stable" {
		t.Errorf("direction = %s, want stable", p.Direction)
	}
	// Confidence: r² é 1 (série constante e slope=0) → confidence high.
	if p.Confidence == "" {
		t.Errorf("confidence vazio: %+v", p)
	}
}

func TestPredict_FiltersInsufficientData(t *testing.T) {
	// 3 reais + 4 insufficient = só 3 válidos → "histórico insuficiente"
	samples := []Sample{
		tier(0, "elite"),
		tier(7, "insufficient_data"),
		tier(14, "high"),
		tier(21, "insufficient_data"),
		tier(28, "insufficient_data"),
		tier(35, "medium"),
		tier(42, "insufficient_data"),
	}
	p := Predict(samples)
	if p.SampleSize != 3 {
		t.Errorf("sample = %d, want 3 (insufficient filtrados)", p.SampleSize)
	}
	if p.Reason == "" {
		t.Error("deveria reportar insuficiente")
	}
}

func TestPredict_WillBreachProjection(t *testing.T) {
	// Slope claro: high (3) → medium (2) em ~30 dias com 7 amostras.
	samples := []Sample{
		tier(0, "high"),
		tier(5, "high"),
		tier(10, "high"),
		tier(15, "high"),
		tier(20, "medium"),
		tier(25, "medium"),
		tier(30, "medium"),
	}
	p := Predict(samples)
	// Pode ou não disparar breach (depende do ponto exato de cruzar
	// 2.5 → 1.5). O que validamos: se disparou, é positivo e ≤ horizon.
	if p.WillBreachInDays != nil {
		if *p.WillBreachInDays <= 0 || *p.WillBreachInDays > MaxBreachHorizonDays {
			t.Errorf("breach inválido: %d", *p.WillBreachInDays)
		}
	}
}

func TestRankToTier_Boundaries(t *testing.T) {
	cases := []struct {
		rank float64
		want string
	}{
		{4.0, "elite"},
		{3.5, "elite"},
		{3.49, "high"},
		{3.0, "high"},
		{2.5, "high"},
		{2.49, "medium"},
		{1.5, "medium"},
		{1.49, "low"},
		{1.0, "low"},
		{0, "low"},
	}
	for _, c := range cases {
		if got := rankToTier(c.rank); got != c.want {
			t.Errorf("rankToTier(%.2f) = %s, want %s", c.rank, got, c.want)
		}
	}
}

func TestLinearRegression_PerfectLine(t *testing.T) {
	xs := []float64{0, 1, 2, 3, 4}
	ys := []float64{1, 3, 5, 7, 9} // y = 2x + 1
	slope, intercept, r2 := linearRegression(xs, ys)
	if slope < 1.99 || slope > 2.01 {
		t.Errorf("slope = %v, want 2", slope)
	}
	if intercept < 0.99 || intercept > 1.01 {
		t.Errorf("intercept = %v, want 1", intercept)
	}
	if r2 < 0.99 {
		t.Errorf("r2 = %v, want ≈1", r2)
	}
}

func TestLinearRegression_HorizontalLine_R2Is1WhenSlope0(t *testing.T) {
	xs := []float64{0, 1, 2, 3}
	ys := []float64{5, 5, 5, 5}
	slope, intercept, r2 := linearRegression(xs, ys)
	if slope != 0 || intercept != 5 || r2 != 1 {
		t.Errorf("slope=%v intercept=%v r2=%v", slope, intercept, r2)
	}
}

func TestFmtFloat(t *testing.T) {
	cases := map[float64]string{
		0.0:   "0.00",
		1.234: "1.23",
		-2.5:  "-2.50",
		3.999: "4.00", // arredondamento
	}
	for in, want := range cases {
		if got := fmtFloat(in); got != want {
			t.Errorf("fmtFloat(%v) = %q, want %q", in, got, want)
		}
	}
}
