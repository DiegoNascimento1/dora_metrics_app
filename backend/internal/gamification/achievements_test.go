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
