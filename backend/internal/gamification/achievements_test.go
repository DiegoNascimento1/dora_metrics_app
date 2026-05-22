package gamification

import "testing"

func TestEvaluateAchievements_NoSample(t *testing.T) {
	out := EvaluateAchievements(ProjectStats{
		DaysSinceLastIncident: 200,
		CurrentClassification: "insufficient_data",
		SampleSize:            0,
	}, "2026-05-20")
	if len(out) != 0 {
		t.Errorf("no sample should yield no achievements, got %d", len(out))
	}
}

func TestEvaluateAchievements_NoIncidentEver(t *testing.T) {
	out := EvaluateAchievements(ProjectStats{
		DaysSinceLastIncident: -1, // nunca teve incident
		CurrentClassification: "high",
		SampleSize:            15,
	}, "2026-05-20")
	for _, a := range out {
		if a.Code == "week_streak" || a.Code == "thirty_green_days" || a.Code == "hundred_green_days" {
			t.Errorf("DaysSinceLastIncident=-1 should NOT unlock streak: %s", a.Code)
		}
	}
}

func TestEvaluateAchievements_StreaksTiered(t *testing.T) {
	cases := []struct {
		days int
		code string
	}{
		{0, ""},
		{3, ""},
		{7, "week_streak"},
		{15, "week_streak"},
		{30, "thirty_green_days"},
		{75, "thirty_green_days"},
		{100, "hundred_green_days"},
		{250, "hundred_green_days"},
	}
	for _, tc := range cases {
		out := EvaluateAchievements(ProjectStats{
			DaysSinceLastIncident: tc.days,
			CurrentClassification: "medium",
			SampleSize:            5,
		}, "2026-05-20")

		gotCode := ""
		for _, a := range out {
			if a.Code == "week_streak" || a.Code == "thirty_green_days" || a.Code == "hundred_green_days" {
				gotCode = a.Code
				break
			}
		}
		if gotCode != tc.code {
			t.Errorf("days=%d: got %q, want %q", tc.days, gotCode, tc.code)
		}
	}
}

func TestEvaluateAchievements_EliteTier(t *testing.T) {
	out := EvaluateAchievements(ProjectStats{
		DaysSinceLastIncident: 10,
		CurrentClassification: "elite",
		SampleSize:            30,
	}, "2026-05-20")

	hasElite := false
	for _, a := range out {
		if a.Code == "current_elite_tier" {
			hasElite = true
		}
	}
	if !hasElite {
		t.Error("elite classification should unlock current_elite_tier")
	}
}

func TestEvaluateAchievements_NonEliteDoesNotUnlock(t *testing.T) {
	for _, tier := range []string{"high", "medium", "low", "insufficient_data"} {
		out := EvaluateAchievements(ProjectStats{
			DaysSinceLastIncident: 10,
			CurrentClassification: tier,
			SampleSize:            5,
		}, "2026-05-20")
		for _, a := range out {
			if a.Code == "current_elite_tier" {
				t.Errorf("tier=%s should NOT unlock current_elite_tier", tier)
			}
		}
	}
}

func TestEvaluateAchievements_StreakStopsAtHighest(t *testing.T) {
	// 150 dias deveria desbloquear SÓ hundred_green_days, não os três.
	out := EvaluateAchievements(ProjectStats{
		DaysSinceLastIncident: 150,
		CurrentClassification: "elite",
		SampleSize:            10,
	}, "2026-05-20")

	streakCount := 0
	for _, a := range out {
		switch a.Code {
		case "week_streak", "thirty_green_days", "hundred_green_days":
			streakCount++
		}
	}
	if streakCount != 1 {
		t.Errorf("expected exactly 1 streak achievement, got %d", streakCount)
	}
}

// ---- batch 2 ----

func TestEvaluateAchievements_FirstEliteMonth(t *testing.T) {
	out := EvaluateAchievements(ProjectStats{
		DaysSinceLastIncident: 5,
		CurrentClassification: "high",
		SampleSize:            5,
		EliteMonthsCount:      1,
	}, "2026-05-20")
	if !hasCode(out, "first_elite_month") {
		t.Error("EliteMonthsCount=1 should unlock first_elite_month")
	}
}

func TestEvaluateAchievements_FirstEliteMonth_NotYet(t *testing.T) {
	out := EvaluateAchievements(ProjectStats{
		CurrentClassification: "elite",
		SampleSize:            10,
		EliteMonthsCount:      0,
	}, "2026-05-20")
	if hasCode(out, "first_elite_month") {
		t.Error("EliteMonthsCount=0 should NOT unlock first_elite_month")
	}
}

func TestEvaluateAchievements_SpeedDemon(t *testing.T) {
	lt := int64(1800) // 30min
	out := EvaluateAchievements(ProjectStats{
		CurrentClassification: "high",
		SampleSize:            5,
		LeadTimeMedianSeconds: &lt,
	}, "2026-05-20")
	if !hasCode(out, "speed_demon") {
		t.Error("LT < 1h with sample >= 4 should unlock speed_demon")
	}
}

func TestEvaluateAchievements_SpeedDemon_LowSample(t *testing.T) {
	lt := int64(1800)
	out := EvaluateAchievements(ProjectStats{
		SampleSize:            3, // só 3 deploys
		LeadTimeMedianSeconds: &lt,
	}, "2026-05-20")
	if hasCode(out, "speed_demon") {
		t.Error("speed_demon requires sample >= 4 even with fast LT")
	}
}

func TestEvaluateAchievements_SpeedDemon_TooSlow(t *testing.T) {
	lt := int64(4000) // > 1h
	out := EvaluateAchievements(ProjectStats{
		SampleSize:            10,
		LeadTimeMedianSeconds: &lt,
	}, "2026-05-20")
	if hasCode(out, "speed_demon") {
		t.Error("LT >= 1h should NOT unlock speed_demon")
	}
}

func TestEvaluateAchievements_RecoveryMaster(t *testing.T) {
	out := EvaluateAchievements(ProjectStats{
		LastIncidentsMTTR: []int64{1200, 800, 2400, 600, 1800}, // todos < 1h
	}, "2026-05-20")
	if !hasCode(out, "recovery_master") {
		t.Error("5 incidents all < 1h MTTR should unlock recovery_master")
	}
}

func TestEvaluateAchievements_RecoveryMaster_OneSlow(t *testing.T) {
	out := EvaluateAchievements(ProjectStats{
		LastIncidentsMTTR: []int64{1200, 800, 7200 /* 2h */, 600, 1800},
	}, "2026-05-20")
	if hasCode(out, "recovery_master") {
		t.Error("one incident >= 1h should block recovery_master")
	}
}

func TestEvaluateAchievements_RecoveryMaster_TooFew(t *testing.T) {
	out := EvaluateAchievements(ProjectStats{
		LastIncidentsMTTR: []int64{1200, 800, 600}, // só 3 incidents
	}, "2026-05-20")
	if hasCode(out, "recovery_master") {
		t.Error("< 5 incidents should NOT unlock recovery_master")
	}
}

func TestEvaluateAchievements_MostImproved_Unlocks(t *testing.T) {
	// low → medium → elite (salto >= 2 ranks)
	out := EvaluateAchievements(ProjectStats{
		TierProgressionLast3Months: []string{"low", "medium", "elite"},
		CurrentClassification:      "elite",
	}, "2026-05-22")
	if !hasCode(out, "most_improved") {
		t.Error("salto low→elite deveria desbloquear most_improved")
	}
}

func TestEvaluateAchievements_MostImproved_SingleStepDoesNotUnlock(t *testing.T) {
	// low → medium (salto de 1 rank)
	out := EvaluateAchievements(ProjectStats{
		TierProgressionLast3Months: []string{"low", "medium"},
	}, "2026-05-22")
	if hasCode(out, "most_improved") {
		t.Error("salto de 1 rank não deveria desbloquear most_improved")
	}
}

func TestEvaluateAchievements_MostImproved_DownwardDoesNotUnlock(t *testing.T) {
	// elite → medium (regrediu)
	out := EvaluateAchievements(ProjectStats{
		TierProgressionLast3Months: []string{"elite", "medium"},
	}, "2026-05-22")
	if hasCode(out, "most_improved") {
		t.Error("regressão não deveria desbloquear most_improved")
	}
}

func TestEvaluateAchievements_MostImproved_InsufficientData(t *testing.T) {
	// 1 ponto só / insufficient_data no fim
	for _, p := range [][]string{
		nil,
		{"low"},
		{"low", "insufficient_data"},
	} {
		out := EvaluateAchievements(ProjectStats{TierProgressionLast3Months: p}, "2026-05-22")
		if hasCode(out, "most_improved") {
			t.Errorf("progression %v não deveria desbloquear", p)
		}
	}
}

func TestEvaluateAchievements_MostImproved_IgnoresInsufficientInMin(t *testing.T) {
	// insufficient_data → low → high (medium foi pulado).
	// min real é 1 (low), latest é 3 (high) → diff 2 → desbloqueia.
	out := EvaluateAchievements(ProjectStats{
		TierProgressionLast3Months: []string{"insufficient_data", "low", "high"},
	}, "2026-05-22")
	if !hasCode(out, "most_improved") {
		t.Error("insufficient inicial não deveria impedir cálculo de min")
	}
}

func hasCode(achievements []Achievement, code string) bool {
	for _, a := range achievements {
		if a.Code == code {
			return true
		}
	}
	return false
}
