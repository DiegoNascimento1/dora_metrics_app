package collector

import (
	"testing"
	"time"
)

func TestStartOfISOWeek(t *testing.T) {
	// Quarta-feira 2026-05-20 12:00 UTC → ISO week começa segunda 2026-05-18 00:00 UTC.
	wed := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	got := startOfISOWeek(wed)
	want := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("startOfISOWeek(quarta) = %v, want %v", got, want)
	}

	// Segunda → ela mesma.
	mon := time.Date(2026, 5, 18, 8, 30, 0, 0, time.UTC)
	got = startOfISOWeek(mon)
	if !got.Equal(want) {
		t.Errorf("startOfISOWeek(segunda) = %v, want %v", got, want)
	}

	// Domingo 2026-05-24 → semana ISO ainda é a anterior (segunda 18).
	sun := time.Date(2026, 5, 24, 23, 0, 0, 0, time.UTC)
	got = startOfISOWeek(sun)
	if !got.Equal(want) {
		t.Errorf("startOfISOWeek(domingo) = %v, want %v", got, want)
	}

	// Domingo 2026-05-24 + 1s = segunda 00:00:01 → próxima semana começa
	// na segunda 25/05.
	monNext := time.Date(2026, 5, 25, 0, 0, 1, 0, time.UTC)
	got = startOfISOWeek(monNext)
	wantNext := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)
	if !got.Equal(wantNext) {
		t.Errorf("startOfISOWeek(segunda+1s) = %v, want %v", got, wantNext)
	}
}

func TestTierRank(t *testing.T) {
	cases := []struct {
		tier string
		want int
	}{
		{"elite", 4},
		{"high", 3},
		{"medium", 2},
		{"low", 1},
		{"", 0},
		{"weird", 0},
	}
	for _, c := range cases {
		if got := tierRank(c.tier); got != c.want {
			t.Errorf("tierRank(%q) = %d, want %d", c.tier, got, c.want)
		}
	}
}

func TestNullIfEmpty(t *testing.T) {
	if nullIfEmpty("") != nil {
		t.Errorf("empty should map to nil")
	}
	if v := nullIfEmpty("elite"); v != "elite" {
		t.Errorf("nullIfEmpty(elite) = %v", v)
	}
}
