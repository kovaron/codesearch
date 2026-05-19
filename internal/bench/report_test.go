package bench

import (
	"strings"
	"testing"
	"time"
)

func sampleAggs() []Aggregated {
	return []Aggregated{
		{TaskID: "t1", Arm: "codesearch", TokensMedian: 1000, LatencyMedianMs: 1000, ToolCallsMedian: 2, CorrectnessRate: 1.0, ValidRuns: 3, TotalRuns: 3},
		{TaskID: "t1", Arm: "baseline", TokensMedian: 4000, LatencyMedianMs: 2000, ToolCallsMedian: 4, CorrectnessRate: 1.0, ValidRuns: 3, TotalRuns: 3},
	}
}

func TestRenderMarkdown_ContainsHeadlineAndTable(t *testing.T) {
	t.Parallel()
	md := RenderMarkdown(sampleAggs(), Meta{
		Timestamp: time.Date(2026, 5, 17, 14, 23, 0, 0, time.UTC),
		Model:     "m", N: 3, Corpus: "self", TurnCap: 20,
	})
	if !strings.Contains(md, "Correctness") {
		t.Error("missing Correctness summary")
	}
	if !strings.Contains(md, "Cost") {
		t.Error("missing Cost line")
	}
	if !strings.Contains(md, "t1") {
		t.Error("missing task id")
	}
	if !strings.Contains(md, "codesearch") || !strings.Contains(md, "baseline") {
		t.Error("missing arm names")
	}
}

func TestRenderMarkdown_BaselineCheaperWording(t *testing.T) {
	t.Parallel()
	// Baseline strictly cheaper on all 3 metrics: expect headline to phrase
	// it as "baseline N× cheaper than codesearch", not "codesearch X× cheaper".
	aggs := []Aggregated{
		{TaskID: "t1", Arm: "codesearch", TokensMedian: 4000, LatencyMedianMs: 2000, ToolCallsMedian: 4, CorrectnessRate: 1.0, ValidRuns: 3, TotalRuns: 3},
		{TaskID: "t1", Arm: "baseline", TokensMedian: 1000, LatencyMedianMs: 1000, ToolCallsMedian: 2, CorrectnessRate: 1.0, ValidRuns: 3, TotalRuns: 3},
	}
	md := RenderMarkdown(aggs, Meta{Timestamp: time.Date(2026, 5, 17, 14, 23, 0, 0, time.UTC), Model: "m", N: 3, Corpus: "self", TurnCap: 20})
	if !strings.Contains(md, "baseline") || !strings.Contains(md, "cheaper than codesearch") {
		t.Errorf("expected 'baseline ... cheaper than codesearch' phrasing, got:\n%s", md)
	}
}

func TestRenderMarkdown_NoFairFightTasks(t *testing.T) {
	t.Parallel()
	// Only one arm correct → no fair-fight comparison possible.
	aggs := []Aggregated{
		{TaskID: "t1", Arm: "codesearch", TokensMedian: 1000, LatencyMedianMs: 1000, ToolCallsMedian: 2, CorrectnessRate: 1.0, ValidRuns: 3, TotalRuns: 3},
		{TaskID: "t1", Arm: "baseline", TokensMedian: 0, LatencyMedianMs: 0, ToolCallsMedian: 0, CorrectnessRate: 0.0, ValidRuns: 0, TotalRuns: 3},
	}
	md := RenderMarkdown(aggs, Meta{Timestamp: time.Date(2026, 5, 17, 14, 23, 0, 0, time.UTC), Model: "m", N: 3, Corpus: "self", TurnCap: 20})
	if !strings.Contains(md, "no fair-fight cost comparison possible") {
		t.Errorf("expected no-fair-fight headline, got:\n%s", md)
	}
}

func TestRenderMarkdown_IncludesToolUsage(t *testing.T) {
	t.Parallel()
	aggs := []Aggregated{
		{
			TaskID: "t1", Arm: "codesearch",
			TokensMedian: 1000, LatencyMedianMs: 1000, ToolCallsMedian: 3,
			CorrectnessRate: 1.0, ValidRuns: 2, TotalRuns: 2,
			ToolCallsByName: map[string]int{"search_hybrid": 5, "trace_path": 3},
		},
		{
			TaskID: "t1", Arm: "baseline",
			TokensMedian: 4000, LatencyMedianMs: 2000, ToolCallsMedian: 6,
			CorrectnessRate: 1.0, ValidRuns: 2, TotalRuns: 2,
			ToolCallsByName: map[string]int{"bash": 12},
		},
	}
	md := RenderMarkdown(aggs, Meta{
		Timestamp: time.Date(2026, 5, 17, 14, 23, 0, 0, time.UTC),
		Model:     "m", N: 2, Corpus: "self", TurnCap: 20,
	})
	if !strings.Contains(md, "## Tool usage") {
		t.Error("missing '## Tool usage' section")
	}
	if !strings.Contains(md, "search_hybrid") {
		t.Error("missing search_hybrid in tool usage")
	}
	if !strings.Contains(md, "trace_path") {
		t.Error("missing trace_path in tool usage")
	}
	if !strings.Contains(md, "bash") {
		t.Error("missing bash in tool usage")
	}
}

func TestRenderJSON_RoundTrips(t *testing.T) {
	t.Parallel()
	js := RenderJSON(sampleAggs(), Meta{
		Timestamp: time.Date(2026, 5, 17, 14, 23, 0, 0, time.UTC),
		Model:     "m", N: 3, Corpus: "self", TurnCap: 20,
	})
	if !strings.Contains(js, `"task_id": "t1"`) && !strings.Contains(js, `"task_id":"t1"`) {
		t.Errorf("json missing task_id: %s", js)
	}
}
