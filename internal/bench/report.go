package bench

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// toolUsageSums returns a map of tool→total calls summed across all Aggregated
// entries for the given arm name.
func toolUsageSums(aggs []Aggregated, arm string) map[string]int {
	out := map[string]int{}
	for _, a := range aggs {
		if a.Arm != arm {
			continue
		}
		for name, cnt := range a.ToolCallsByName {
			out[name] += cnt
		}
	}
	return out
}

// sortedToolNames returns tool names sorted descending by count.
func sortedToolNames(m map[string]int) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if m[names[i]] != m[names[j]] {
			return m[names[i]] > m[names[j]]
		}
		return names[i] < names[j]
	})
	return names
}

// isFinite reports whether x is a finite (non-NaN, non-±Inf) float.
func isFinite(x float64) bool {
	return !math.IsInf(x, 0) && !math.IsNaN(x)
}

// correctnessTally counts how many tasks each arm passed at least once.
func correctnessTally(pairs []taskPair) (cs, bl, total int) {
	for _, p := range pairs {
		total++
		if p.cs != nil && p.cs.CorrectnessRate > 0 {
			cs++
		}
		if p.bl != nil && p.bl.CorrectnessRate > 0 {
			bl++
		}
	}
	return cs, bl, total
}

// Meta is the run-scope metadata embedded in a report.
type Meta struct {
	Timestamp time.Time
	Model     string
	N         int
	Corpus    string
	TurnCap   int
}

type reportJSON struct {
	Meta metaJSON  `json:"meta"`
	Aggs []aggJSON `json:"aggregates"`
}

type metaJSON struct {
	Timestamp string `json:"timestamp"`
	Model     string `json:"model"`
	N         int    `json:"n"`
	Corpus    string `json:"corpus"`
	TurnCap   int    `json:"turn_cap"`
}

type aggJSON struct {
	TaskID          string         `json:"task_id"`
	Arm             string         `json:"arm"`
	TokensMedian    float64        `json:"tokens_median"`
	LatencyMedianMs float64        `json:"latency_median_ms"`
	ToolCallsMedian float64        `json:"tool_calls_median"`
	CorrectnessRate float64        `json:"correctness_rate"`
	ValidRuns       int            `json:"valid_runs"`
	TotalRuns       int            `json:"total_runs"`
	ToolCallsByName map[string]int `json:"tool_calls_by_name,omitempty"`
}

// RenderJSON serializes aggregates plus run metadata as indented JSON.
func RenderJSON(aggs []Aggregated, m Meta) string {
	r := reportJSON{
		Meta: metaJSON{
			Timestamp: m.Timestamp.UTC().Format(time.RFC3339),
			Model:     m.Model,
			N:         m.N,
			Corpus:    m.Corpus,
			TurnCap:   m.TurnCap,
		},
	}
	for _, a := range aggs {
		r.Aggs = append(r.Aggs, aggJSON{
			TaskID: a.TaskID, Arm: a.Arm,
			TokensMedian: a.TokensMedian, LatencyMedianMs: a.LatencyMedianMs,
			ToolCallsMedian: a.ToolCallsMedian, CorrectnessRate: a.CorrectnessRate,
			ValidRuns: a.ValidRuns, TotalRuns: a.TotalRuns,
			ToolCallsByName: a.ToolCallsByName,
		})
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}

// RenderMarkdown produces a human-readable report with a headline ratio
// and a per-task breakdown table.
func RenderMarkdown(aggs []Aggregated, m Meta) string {
	pairs := pairByTask(aggs)
	var b strings.Builder
	fmt.Fprintf(&b, "# codesearch bench — %s\n\n", m.Timestamp.UTC().Format(time.RFC3339))

	// Composite cost ratio across tasks where BOTH arms had a finite score
	// (i.e. both arms passed CorrectnessRate == 1.0). Composite returns +Inf
	// when self.CorrectnessRate < 1.0 and GeoMean filters non-finite values.
	csComps, blComps := []float64{}, []float64{}
	bothCorrectTasks := 0
	for _, p := range pairs {
		if p.cs == nil || p.bl == nil {
			continue
		}
		sc := Composite(*p.cs, *p.bl)
		sb := Composite(*p.bl, *p.cs)
		// Only count tasks where BOTH arms scored finitely — those are the
		// fair-fight comparisons. Tasks where only one arm succeeded inflate
		// the geomean of the surviving arm with a 1.0 score and silently
		// disappear from the loser's geomean.
		if !isFinite(sc) || !isFinite(sb) {
			continue
		}
		csComps = append(csComps, sc)
		blComps = append(blComps, sb)
		bothCorrectTasks++
	}
	csOverall := GeoMean(csComps)
	blOverall := GeoMean(blComps)

	csTaskWins, blTaskWins, totalTasks := correctnessTally(pairs)
	fmt.Fprintf(&b, "**Correctness:** codesearch **%d / %d** tasks, baseline **%d / %d** tasks (passed ≥ 1 run).\n\n",
		csTaskWins, totalTasks, blTaskWins, totalTasks)

	if bothCorrectTasks == 0 {
		fmt.Fprintln(&b, "**Cost headline:** no task had both arms 100% correct — no fair-fight cost comparison possible. See per-task table.")
		fmt.Fprintln(&b, "")
	} else {
		ratio := blOverall / csOverall
		if ratio >= 1 {
			fmt.Fprintf(&b, "**Cost (both-arms-correct, %d / %d tasks):** codesearch **%.2f×** cheaper than baseline.\n\n",
				bothCorrectTasks, totalTasks, ratio)
		} else {
			fmt.Fprintf(&b, "**Cost (both-arms-correct, %d / %d tasks):** baseline **%.2f×** cheaper than codesearch.\n\n",
				bothCorrectTasks, totalTasks, 1.0/ratio)
		}
	}

	// Tool usage tables (one per arm), sorted descending by call count.
	for _, arm := range []string{"codesearch", "baseline"} {
		sums := toolUsageSums(aggs, arm)
		if len(sums) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## Tool usage (%s arm)\n\n", arm)
		fmt.Fprintln(&b, "| Tool | Total calls |")
		fmt.Fprintln(&b, "|------|-----:|")
		for _, name := range sortedToolNames(sums) {
			fmt.Fprintf(&b, "| %s | %d |\n", name, sums[name])
		}
		fmt.Fprintln(&b, "")
	}

	fmt.Fprintln(&b, "## Per-task breakdown")
	fmt.Fprintln(&b, "")
	fmt.Fprintln(&b, "| Task | cs tok | bl tok | cs ms | bl ms | cs✓ | bl✓ | winner |")
	fmt.Fprintln(&b, "|------|-------:|-------:|------:|------:|:---:|:---:|:------:|")
	for _, p := range pairs {
		cstok, bltok := "—", "—"
		csms, blms := "—", "—"
		cscorr, blcorr := "—", "—"
		winner := "—"
		if p.cs != nil {
			cstok = fmt.Sprintf("%.0f", p.cs.TokensMedian)
			csms = fmt.Sprintf("%.0f", p.cs.LatencyMedianMs)
			cscorr = fmt.Sprintf("%.0f%%", p.cs.CorrectnessRate*100)
		}
		if p.bl != nil {
			bltok = fmt.Sprintf("%.0f", p.bl.TokensMedian)
			blms = fmt.Sprintf("%.0f", p.bl.LatencyMedianMs)
			blcorr = fmt.Sprintf("%.0f%%", p.bl.CorrectnessRate*100)
		}
		if p.cs != nil && p.bl != nil {
			sc := Composite(*p.cs, *p.bl)
			sb := Composite(*p.bl, *p.cs)
			switch {
			case sc < sb:
				winner = "cs"
			case sb < sc:
				winner = "bl"
			default:
				winner = "tie"
			}
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s |\n",
			p.id, cstok, bltok, csms, blms, cscorr, blcorr, winner)
	}

	fmt.Fprintf(&b, "\n## Run config\n\nmodel=%s, N=%d, corpus=%s, turn_cap=%d\n",
		m.Model, m.N, m.Corpus, m.TurnCap)
	return b.String()
}

type taskPair struct {
	id string
	cs *Aggregated
	bl *Aggregated
}

func pairByTask(aggs []Aggregated) []taskPair {
	m := map[string]*taskPair{}
	for i := range aggs {
		a := &aggs[i]
		p, ok := m[a.TaskID]
		if !ok {
			p = &taskPair{id: a.TaskID}
			m[a.TaskID] = p
		}
		switch a.Arm {
		case "codesearch":
			p.cs = a
		case "baseline":
			p.bl = a
		}
	}
	out := make([]taskPair, 0, len(m))
	for _, p := range m {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}
