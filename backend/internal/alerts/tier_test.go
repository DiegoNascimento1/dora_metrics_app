package alerts

import "testing"

func TestTierOrder(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"elite", 4},
		{"high", 3},
		{"medium", 2},
		{"low", 1},
		{"insufficient_data", -1},
		{"", -1},
		{"garbage", -1},
	}
	for _, tc := range cases {
		if got := TierOrder(tc.in); got != tc.want {
			t.Errorf("TierOrder(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestIsRegression(t *testing.T) {
	cases := []struct {
		prev, cur string
		want      bool
	}{
		{"elite", "high", true},
		{"high", "elite", false},
		{"elite", "elite", false},
		{"high", "low", true},
		{"low", "high", false},
		{"insufficient_data", "elite", false},
		{"elite", "insufficient_data", false},
		{"", "elite", false},
	}
	for _, tc := range cases {
		if got := IsRegression(tc.prev, tc.cur); got != tc.want {
			t.Errorf("IsRegression(%q, %q) = %v, want %v", tc.prev, tc.cur, got, tc.want)
		}
	}
}

func TestIsChange(t *testing.T) {
	if !IsChange("elite", "high") {
		t.Error("elite -> high should be a change")
	}
	if !IsChange("low", "elite") {
		t.Error("low -> elite should be a change (promoção)")
	}
	if IsChange("elite", "elite") {
		t.Error("elite -> elite is not a change")
	}
	if IsChange("insufficient_data", "elite") {
		t.Error("insufficient_data side should never count as change")
	}
}

func TestRuleMatchesChange(t *testing.T) {
	if !RuleMatchesChange("tier_regression", "elite", "high") {
		t.Error("tier_regression should fire on elite->high")
	}
	if RuleMatchesChange("tier_regression", "high", "elite") {
		t.Error("tier_regression should NOT fire on promotion")
	}
	if !RuleMatchesChange("tier_change", "high", "elite") {
		t.Error("tier_change should fire on promotion")
	}
	if RuleMatchesChange("tier_change", "elite", "elite") {
		t.Error("tier_change should NOT fire on same tier")
	}
	if RuleMatchesChange("unknown_kind", "elite", "high") {
		t.Error("unknown kind should never fire")
	}
}
