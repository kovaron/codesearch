package bench

import (
	"math"
	"testing"
)

func TestMedian(t *testing.T) {
	t.Parallel()
	got := median([]float64{1, 2, 3})
	if got != 2 {
		t.Fatalf("median(1,2,3)=%v", got)
	}
	got = median([]float64{1, 2, 3, 4})
	if got != 2.5 {
		t.Fatalf("median(1,2,3,4)=%v", got)
	}
	if median(nil) != 0 {
		t.Fatal("median(nil) want 0")
	}
}

func TestAggregate_DropsInvalidAndTruncated(t *testing.T) {
	t.Parallel()
	runs := []RunResult{
		{TaskID: "t1", Arm: "codesearch", TotalTokens: 100, LatencyMs: 1000, ToolCalls: 3, Correct: true},
		{TaskID: "t1", Arm: "codesearch", TotalTokens: 200, LatencyMs: 2000, ToolCalls: 4, Correct: true},
		{TaskID: "t1", Arm: "codesearch", TotalTokens: 999, LatencyMs: 9999, ToolCalls: 9, Truncated: true},
	}
	agg := Aggregate(runs)
	if len(agg) != 1 {
		t.Fatalf("want 1 agg got %d", len(agg))
	}
	a := agg[0]
	if a.TokensMedian != 150 {
		t.Errorf("TokensMedian=%v want 150", a.TokensMedian)
	}
	if a.LatencyMedianMs != 1500 {
		t.Errorf("LatencyMedianMs=%v want 1500", a.LatencyMedianMs)
	}
	if a.CorrectnessRate != 2.0/3.0 {
		t.Errorf("CorrectnessRate=%v", a.CorrectnessRate)
	}
}

func TestComposite_TokenWeighted(t *testing.T) {
	t.Parallel()
	cs := Aggregated{TokensMedian: 1000, LatencyMedianMs: 1000, ToolCallsMedian: 2, CorrectnessRate: 1.0}
	bl := Aggregated{TokensMedian: 4000, LatencyMedianMs: 2000, ToolCallsMedian: 4, CorrectnessRate: 1.0}
	scs := Composite(cs, bl)
	sbl := Composite(bl, cs)
	if scs >= sbl {
		t.Fatalf("expected codesearch < baseline, got cs=%v bl=%v", scs, sbl)
	}
	if math.Abs(scs-1.0) > 1e-9 {
		t.Errorf("scs=%v want 1.0", scs)
	}
	want := 0.6*4 + 0.2*2 + 0.2*2
	if math.Abs(sbl-want) > 1e-9 {
		t.Errorf("sbl=%v want %v", sbl, want)
	}
}

func TestComposite_IncorrectIsInfinity(t *testing.T) {
	t.Parallel()
	cs := Aggregated{TokensMedian: 1, LatencyMedianMs: 1, ToolCallsMedian: 1, CorrectnessRate: 0.5}
	bl := Aggregated{TokensMedian: 1, LatencyMedianMs: 1, ToolCallsMedian: 1, CorrectnessRate: 1.0}
	if !math.IsInf(Composite(cs, bl), +1) {
		t.Fatal("incorrect arm must score +Inf")
	}
}

func TestAggregate_FoldsToolCallsByName(t *testing.T) {
	t.Parallel()
	runs := []RunResult{
		{TaskID: "t1", Arm: "codesearch", TotalTokens: 100, LatencyMs: 1000, ToolCalls: 3, Correct: true,
			ToolCallsByName: map[string]int{"search_hybrid": 2, "trace_path": 1}},
		{TaskID: "t1", Arm: "codesearch", TotalTokens: 200, LatencyMs: 2000, ToolCalls: 4, Correct: true,
			ToolCallsByName: map[string]int{"search_hybrid": 1, "search_semantic": 3}},
		{TaskID: "t1", Arm: "codesearch", TotalTokens: 999, LatencyMs: 9999, ToolCalls: 9, Truncated: true,
			ToolCallsByName: map[string]int{"search_hybrid": 5}},
	}
	agg := Aggregate(runs)
	if len(agg) != 1 {
		t.Fatalf("want 1 agg got %d", len(agg))
	}
	a := agg[0]
	if a.ToolCallsByName == nil {
		t.Fatal("ToolCallsByName must not be nil")
	}
	// All three runs contribute to the sum (including the truncated one).
	if got := a.ToolCallsByName["search_hybrid"]; got != 8 {
		t.Errorf("search_hybrid want 8 got %d", got)
	}
	if got := a.ToolCallsByName["trace_path"]; got != 1 {
		t.Errorf("trace_path want 1 got %d", got)
	}
	if got := a.ToolCallsByName["search_semantic"]; got != 3 {
		t.Errorf("search_semantic want 3 got %d", got)
	}
}

func TestGeoMean(t *testing.T) {
	t.Parallel()
	got := GeoMean([]float64{1, 4, 16})
	if math.Abs(got-4) > 1e-9 {
		t.Errorf("GeoMean(1,4,16)=%v want 4", got)
	}
	if !math.IsInf(GeoMean([]float64{}), +1) {
		t.Error("GeoMean(empty) want +Inf")
	}
	if !math.IsInf(GeoMean([]float64{math.Inf(+1)}), +1) {
		t.Error("GeoMean(+Inf only) want +Inf")
	}
}
