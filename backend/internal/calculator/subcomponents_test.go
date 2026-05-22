package calculator

import (
	"testing"
	"time"
)

func tp(s string) *time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return &t
}

func TestDecompose_AllPresent(t *testing.T) {
	got := Decompose(MRTimestamps{
		FirstCommitAt: tp("2026-05-01T10:00:00Z"),
		OpenedAt:      tp("2026-05-01T18:00:00Z"), // +8h pickup
		MergedAt:      tp("2026-05-02T10:00:00Z"), // +16h review
		DeployedAt:    tp("2026-05-02T11:00:00Z"), // +1h deploy lag
	})
	wantPickup := int64(8 * 3600)
	wantReview := int64(16 * 3600)
	wantDeploy := int64(3600)
	wantTotal := int64(25 * 3600)
	if got.PickupSeconds == nil || *got.PickupSeconds != wantPickup {
		t.Errorf("pickup = %v, want %d", got.PickupSeconds, wantPickup)
	}
	if got.ReviewSeconds == nil || *got.ReviewSeconds != wantReview {
		t.Errorf("review = %v, want %d", got.ReviewSeconds, wantReview)
	}
	if got.DeployLagSeconds == nil || *got.DeployLagSeconds != wantDeploy {
		t.Errorf("deploy = %v, want %d", got.DeployLagSeconds, wantDeploy)
	}
	if got.TotalLeadSeconds == nil || *got.TotalLeadSeconds != wantTotal {
		t.Errorf("total = %v, want %d", got.TotalLeadSeconds, wantTotal)
	}
}

func TestDecompose_MissingPartialTimestamps(t *testing.T) {
	// só commit + deploy → total ok; outros componentes nil.
	got := Decompose(MRTimestamps{
		FirstCommitAt: tp("2026-05-01T10:00:00Z"),
		DeployedAt:    tp("2026-05-01T12:00:00Z"),
	})
	if got.PickupSeconds != nil {
		t.Error("pickup deveria ser nil (sem opened_at)")
	}
	if got.ReviewSeconds != nil {
		t.Error("review deveria ser nil")
	}
	if got.DeployLagSeconds != nil {
		t.Error("deploy_lag deveria ser nil")
	}
	if got.TotalLeadSeconds == nil || *got.TotalLeadSeconds != 7200 {
		t.Errorf("total = %v, want 7200", got.TotalLeadSeconds)
	}
}

func TestDecompose_NegativeDurationsDiscarded(t *testing.T) {
	// merged BEFORE opened → review negativo, descartado.
	got := Decompose(MRTimestamps{
		OpenedAt: tp("2026-05-02T10:00:00Z"),
		MergedAt: tp("2026-05-01T10:00:00Z"),
	})
	if got.ReviewSeconds != nil {
		t.Errorf("review negativo deveria ser nil, got %v", *got.ReviewSeconds)
	}
}

func TestAggregateLeadTime_MedianOfThree(t *testing.T) {
	mrs := []LeadTimeBreakdown{
		{ReviewSeconds: int64ptr(100)},
		{ReviewSeconds: int64ptr(200)},
		{ReviewSeconds: int64ptr(300)},
	}
	agg := AggregateLeadTime(mrs)
	if agg.ReviewMedianSeconds == nil || *agg.ReviewMedianSeconds != 200 {
		t.Errorf("median = %v, want 200", agg.ReviewMedianSeconds)
	}
	if agg.SampleSize != 3 {
		t.Errorf("sample = %d", agg.SampleSize)
	}
}

func TestAggregateLeadTime_MedianOfFour(t *testing.T) {
	// pares — mediana é (segundo+terceiro)/2
	mrs := []LeadTimeBreakdown{
		{PickupSeconds: int64ptr(10)},
		{PickupSeconds: int64ptr(20)},
		{PickupSeconds: int64ptr(40)},
		{PickupSeconds: int64ptr(80)},
	}
	agg := AggregateLeadTime(mrs)
	if agg.PickupMedianSeconds == nil || *agg.PickupMedianSeconds != 30 {
		t.Errorf("median = %v, want 30", agg.PickupMedianSeconds)
	}
}

func TestAggregateLeadTime_IgnoresNilIndependently(t *testing.T) {
	// MR1 tem pickup mas não review; MR2 tem review mas não pickup.
	mrs := []LeadTimeBreakdown{
		{PickupSeconds: int64ptr(100)},
		{ReviewSeconds: int64ptr(200)},
	}
	agg := AggregateLeadTime(mrs)
	if agg.PickupMedianSeconds == nil || *agg.PickupMedianSeconds != 100 {
		t.Errorf("pickup = %v", agg.PickupMedianSeconds)
	}
	if agg.ReviewMedianSeconds == nil || *agg.ReviewMedianSeconds != 200 {
		t.Errorf("review = %v", agg.ReviewMedianSeconds)
	}
	if agg.SampleSize != 2 {
		t.Errorf("sample = %d", agg.SampleSize)
	}
}

func TestAggregateLeadTime_EmptyReturnsNil(t *testing.T) {
	agg := AggregateLeadTime(nil)
	if agg.PickupMedianSeconds != nil {
		t.Error("pickup deveria ser nil para amostra vazia")
	}
	if agg.SampleSize != 0 {
		t.Errorf("sample = %d, want 0", agg.SampleSize)
	}
}

func int64ptr(v int64) *int64 { return &v }
