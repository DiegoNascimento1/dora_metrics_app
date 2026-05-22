package calculator

import "testing"

func TestAnalyzeGaming_SmallSampleReturnsEmpty(t *testing.T) {
	mrs := []MRSize{
		{Additions: 1, Deletions: 0},
		{Additions: 100, Deletions: 50},
	}
	rep := AnalyzeGaming(mrs)
	if rep.GamingFlag {
		t.Error("amostra de 2 não deveria sinalizar gaming")
	}
	if rep.SampleSize != 2 {
		t.Errorf("sample = %d", rep.SampleSize)
	}
}

func TestAnalyzeGaming_NoGamingWithRealMRs(t *testing.T) {
	mrs := []MRSize{
		{Additions: 100, Deletions: 20},
		{Additions: 50, Deletions: 10},
		{Additions: 200, Deletions: 80},
		{Additions: 30, Deletions: 5},
		{Additions: 1, Deletions: 0}, // 1 trivial em 5 = 20% < 50%
	}
	rep := AnalyzeGaming(mrs)
	if rep.GamingFlag {
		t.Errorf("não deveria sinalizar gaming com 20%% triviais, reason=%q", rep.Reason)
	}
	if rep.TrivialCount != 1 {
		t.Errorf("trivial = %d", rep.TrivialCount)
	}
	if rep.SampleSize != 5 {
		t.Errorf("sample = %d", rep.SampleSize)
	}
}

func TestAnalyzeGaming_FlagsHighTrivialPercent(t *testing.T) {
	mrs := []MRSize{
		{Additions: 1, Deletions: 0},
		{Additions: 2, Deletions: 1},
		{Additions: 0, Deletions: 1},
		{Additions: 1, Deletions: 0},
		{Additions: 50, Deletions: 10},
	}
	rep := AnalyzeGaming(mrs)
	if !rep.GamingFlag {
		t.Errorf("4/5 = 80%% triviais deveria sinalizar, got %+v", rep)
	}
	if rep.TrivialCount != 4 {
		t.Errorf("trivial = %d", rep.TrivialCount)
	}
	if rep.TrivialPercent < 50 {
		t.Errorf("percent = %.1f", rep.TrivialPercent)
	}
	if rep.Reason == "" {
		t.Error("Reason vazio em flag=true")
	}
}

func TestAnalyzeGaming_IgnoresUnknownLines(t *testing.T) {
	// 4 unknown + 4 reais — só os reais contam.
	mrs := []MRSize{
		{LinesUnknown: true},
		{LinesUnknown: true},
		{LinesUnknown: true},
		{LinesUnknown: true},
		{Additions: 1, Deletions: 0},
		{Additions: 1, Deletions: 1},
		{Additions: 200, Deletions: 0},
		{Additions: 100, Deletions: 0},
	}
	rep := AnalyzeGaming(mrs)
	if rep.SampleSize != 4 {
		t.Errorf("sample = %d, want 4 (descartando unknown)", rep.SampleSize)
	}
}

func TestAnalyzeGaming_MedianMRSize(t *testing.T) {
	mrs := []MRSize{
		{Additions: 5, Deletions: 5},     // 10
		{Additions: 25, Deletions: 25},   // 50
		{Additions: 50, Deletions: 50},   // 100
		{Additions: 100, Deletions: 100}, // 200
	}
	rep := AnalyzeGaming(mrs)
	// mediana de [10, 50, 100, 200] = (50+100)/2 = 75
	if rep.MedianMRSize != 75 {
		t.Errorf("median = %d, want 75", rep.MedianMRSize)
	}
}

func TestAnalyzeGaming_ExactlyAtThreshold(t *testing.T) {
	// 50% triviais — deveria sinalizar (>=)
	mrs := []MRSize{
		{Additions: 1, Deletions: 0},
		{Additions: 0, Deletions: 1},
		{Additions: 100, Deletions: 0},
		{Additions: 100, Deletions: 0},
	}
	rep := AnalyzeGaming(mrs)
	if !rep.GamingFlag {
		t.Errorf("50%% deveria flagar (limite inclusivo), got %+v", rep)
	}
}

func TestMRSize_TotalWithUnknown(t *testing.T) {
	if m := (MRSize{LinesUnknown: true, Additions: 999}); m.Total() != 0 {
		t.Errorf("unknown deveria devolver 0, got %d", m.Total())
	}
	if m := (MRSize{Additions: 5, Deletions: 3}); m.Total() != 8 {
		t.Errorf("total = %d, want 8", m.Total())
	}
}

func TestItoaAndFmtPct(t *testing.T) {
	// helpers internos — sanity checks.
	if itoa(0) != "0" {
		t.Error("itoa(0)")
	}
	if itoa(-42) != "-42" {
		t.Error("itoa(-42)")
	}
	if fmtPct(50.0) != "50.0" {
		t.Errorf("fmtPct(50.0) = %q", fmtPct(50.0))
	}
	if fmtPct(65.4) != "65.4" {
		t.Errorf("fmtPct(65.4) = %q", fmtPct(65.4))
	}
}
