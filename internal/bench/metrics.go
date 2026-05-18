package bench

import (
	"math"
	"sort"
)

// RunResult is the per-run record emitted by the bench harness.
type RunResult struct {
	TaskID           string
	Arm              string
	RunIdx           int
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	TotalTokens      int
	LatencyMs        int64
	ToolCalls        int
	Turns            int
	Correct          bool
	Truncated        bool
	Error            string
}

// Aggregated is the per (task, arm) summary across N runs.
type Aggregated struct {
	TaskID          string
	Arm             string
	TokensMedian    float64
	LatencyMedianMs float64
	ToolCallsMedian float64
	CorrectnessRate float64
	ValidRuns       int
	TotalRuns       int
}

// Aggregate folds a flat slice of RunResults into per (task, arm) medians.
// Runs with Error != "" or Truncated == true are dropped from medians but
// still count toward CorrectnessRate denominator.
func Aggregate(runs []RunResult) []Aggregated {
	type key struct{ task, arm string }
	groups := map[key][]RunResult{}
	for _, r := range runs {
		k := key{r.TaskID, r.Arm}
		groups[k] = append(groups[k], r)
	}
	out := make([]Aggregated, 0, len(groups))
	for k, rs := range groups {
		var tok, lat, tc []float64
		correct := 0
		for _, r := range rs {
			if r.Correct {
				correct++
			}
			if r.Error != "" || r.Truncated {
				continue
			}
			tok = append(tok, float64(r.TotalTokens))
			lat = append(lat, float64(r.LatencyMs))
			tc = append(tc, float64(r.ToolCalls))
		}
		out = append(out, Aggregated{
			TaskID:          k.task,
			Arm:             k.arm,
			TokensMedian:    median(tok),
			LatencyMedianMs: median(lat),
			ToolCallsMedian: median(tc),
			CorrectnessRate: float64(correct) / float64(len(rs)),
			ValidRuns:       len(tok),
			TotalRuns:       len(rs),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TaskID != out[j].TaskID {
			return out[i].TaskID < out[j].TaskID
		}
		return out[i].Arm < out[j].Arm
	})
	return out
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// Composite returns the weighted score of self against other.
// Weights: tokens 0.6, latency 0.2, tool calls 0.2. Lower = better.
// CorrectnessRate < 1.0 returns +Inf (auto-loss).
func Composite(self, other Aggregated) float64 {
	if self.CorrectnessRate < 1.0 {
		return math.Inf(+1)
	}
	norm := func(a, b float64) float64 {
		denom := math.Min(a, b)
		if denom == 0 {
			return 1
		}
		return a / denom
	}
	return 0.6*norm(self.TokensMedian, other.TokensMedian) +
		0.2*norm(self.LatencyMedianMs, other.LatencyMedianMs) +
		0.2*norm(self.ToolCallsMedian, other.ToolCallsMedian)
}

// GeoMean returns the geometric mean of finite positive values in xs.
// Non-finite or non-positive entries are skipped. Empty input returns +Inf.
func GeoMean(xs []float64) float64 {
	logSum := 0.0
	n := 0
	for _, x := range xs {
		if !math.IsInf(x, 0) && !math.IsNaN(x) && x > 0 {
			logSum += math.Log(x)
			n++
		}
	}
	if n == 0 {
		return math.Inf(+1)
	}
	return math.Exp(logSum / float64(n))
}
