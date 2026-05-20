package calculator

import (
	"testing"
)

func TestClassifyDeploymentFrequency(t *testing.T) {
	th := DefaultThresholds()

	cases := []struct {
		name    string
		perDay  float64
		want    string
	}{
		{"insufficient_zero", 0, TierInsufficientData},
		{"insufficient_negative", -1, TierInsufficientData},
		{"elite_exactly_one", 1.0, TierElite},
		{"elite_above", 5.0, TierElite},
		{"high_weekly", 1.0 / 7.0, TierHigh},
		{"high_below_elite", 0.5, TierHigh},
		{"medium_monthly", 1.0 / 30.0, TierMedium},
		{"medium_below_weekly", 1.0 / 10.0, TierMedium},
		{"low_below_monthly", 1.0 / 60.0, TierLow},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyDeploymentFrequency(tc.perDay, th)
			if got != tc.want {
				t.Errorf("ClassifyDeploymentFrequency(%v) = %q, want %q", tc.perDay, got, tc.want)
			}
		})
	}
}

func TestClassifyLeadTime(t *testing.T) {
	th := DefaultThresholds()
	hour := int64(3600)
	day := 24 * hour
	week := 7 * day
	month := 30 * day

	cases := []struct {
		name string
		secs *int64
		want string
	}{
		{"nil", nil, TierInsufficientData},
		{"elite_30min", ptr(int64(1800)), TierElite},
		{"high_1day", ptr(day), TierHigh},
		{"medium_2weeks", ptr(2 * week), TierMedium},
		{"low_2months", ptr(2 * month), TierLow},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyLeadTime(tc.secs, th)
			if got != tc.want {
				t.Errorf("ClassifyLeadTime(%v) = %q, want %q", tc.secs, got, tc.want)
			}
		})
	}
}

func TestClassifyChangeFailureRate(t *testing.T) {
	th := DefaultThresholds()

	cases := []struct {
		name string
		rate *float64
		want string
	}{
		{"nil", nil, TierInsufficientData},
		{"elite_zero", fptr(0.0), TierElite},
		{"elite_5pct", fptr(0.05), TierElite},
		{"high_7pct", fptr(0.07), TierHigh},
		{"medium_15pct", fptr(0.15), TierMedium},
		{"low_30pct", fptr(0.30), TierLow},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyChangeFailureRate(tc.rate, th)
			if got != tc.want {
				t.Errorf("ClassifyChangeFailureRate(%v) = %q, want %q", tc.rate, got, tc.want)
			}
		})
	}
}

func TestClassifyMTTR(t *testing.T) {
	th := DefaultThresholds()

	cases := []struct {
		name string
		secs *int64
		want string
	}{
		{"nil", nil, TierInsufficientData},
		{"elite_30min", ptr(int64(1800)), TierElite},
		{"high_12h", ptr(int64(43200)), TierHigh},
		{"medium_2d", ptr(int64(2 * 86400)), TierMedium},
		{"low_2w", ptr(int64(14 * 86400)), TierLow},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyMTTR(tc.secs, th)
			if got != tc.want {
				t.Errorf("ClassifyMTTR(%v) = %q, want %q", tc.secs, got, tc.want)
			}
		})
	}
}

func TestWorstOf(t *testing.T) {
	cases := []struct {
		name  string
		tiers []string
		want  string
	}{
		{"all_elite", []string{TierElite, TierElite, TierElite, TierElite}, TierElite},
		{"mixed_low_wins", []string{TierElite, TierHigh, TierMedium, TierLow}, TierLow},
		{"insufficient_ignored", []string{TierElite, TierInsufficientData, TierHigh}, TierHigh},
		{"all_insufficient", []string{TierInsufficientData, TierInsufficientData}, TierInsufficientData},
		{"empty", []string{}, TierInsufficientData},
		{"unknown_ignored", []string{"bogus", TierElite}, TierElite},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := WorstOf(tc.tiers...)
			if got != tc.want {
				t.Errorf("WorstOf(%v) = %q, want %q", tc.tiers, got, tc.want)
			}
		})
	}
}

func TestFromJSON_OverridesOnTopOfDefaults(t *testing.T) {
	raw := []byte(`{"cfr_elite": 0.3, "cfr_high": 0.4}`)
	got, err := FromJSON(raw)
	if err != nil {
		t.Fatalf("FromJSON: %v", err)
	}
	if got.CFRElite != 0.3 {
		t.Errorf("CFRElite override not applied: got %v", got.CFRElite)
	}
	if got.CFRHigh != 0.4 {
		t.Errorf("CFRHigh override not applied: got %v", got.CFRHigh)
	}
	def := DefaultThresholds()
	if got.DFElite != def.DFElite {
		t.Errorf("DFElite default not preserved: got %v want %v", got.DFElite, def.DFElite)
	}
	if got.MTTRMedium != def.MTTRMedium {
		t.Errorf("MTTRMedium default not preserved: got %v want %v", got.MTTRMedium, def.MTTRMedium)
	}
}

func TestFromJSON_EmptyReturnsDefaults(t *testing.T) {
	got, err := FromJSON(nil)
	if err != nil {
		t.Fatalf("FromJSON(nil): %v", err)
	}
	if got != DefaultThresholds() {
		t.Errorf("FromJSON(nil) did not return defaults")
	}
}

func TestFromJSON_InvalidJSONReturnsError(t *testing.T) {
	_, err := FromJSON([]byte(`{not valid`))
	if err == nil {
		t.Fatal("expected error on invalid JSON, got nil")
	}
}

func ptr(v int64) *int64    { return &v }
func fptr(v float64) *float64 { return &v }
