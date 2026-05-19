package store

import (
	"fmt"
	"sort"
)

// FuseRRF combines two ranked SearchResult lists using reciprocal rank
// fusion and returns the top `limit` results by fused score. The dedup
// key is (Filepath, Name, StartLine). k is the RRF dampening constant;
// 60 is the canonical default from Cormack et al. 2009. Pass k <= 0 to
// use the default of 60.
func FuseRRF(semantic, structural []SearchResult, limit, k int) []SearchResult {
	if k <= 0 {
		k = 60
	}

	type entry struct {
		result SearchResult
		score  float64
	}

	scores := make(map[string]*entry)

	key := func(r SearchResult) string {
		return fmt.Sprintf("%s\x00%s\x00%d", r.Filepath, r.Name, r.StartLine)
	}

	addList := func(list []SearchResult) {
		for rank, r := range list {
			k2 := key(r)
			if e, ok := scores[k2]; ok {
				e.score += 1.0 / float64(k+rank+1)
			} else {
				cp := r
				scores[k2] = &entry{result: cp, score: 1.0 / float64(k+rank+1)}
			}
		}
	}

	addList(semantic)
	addList(structural)

	entries := make([]*entry, 0, len(scores))
	for _, e := range scores {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].score > entries[j].score
	})

	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	out := make([]SearchResult, len(entries))
	for i, e := range entries {
		out[i] = e.result
		out[i].Score = float32(e.score)
	}
	return out
}
