package store

import (
	"testing"
)

func makeResult(file, name string, startLine int) SearchResult {
	return SearchResult{
		Filepath:  file,
		Name:      name,
		StartLine: startLine,
	}
}

// TestFuseRRF_DedupesAcrossLists verifies that a result appearing in both
// lists accumulates a higher score than one appearing in only one list.
func TestFuseRRF_DedupesAcrossLists(t *testing.T) {
	t.Parallel()
	shared := makeResult("a.go", "Foo", 1)
	onlyInSemantic := makeResult("b.go", "Bar", 5)

	sem := []SearchResult{shared, onlyInSemantic}
	str := []SearchResult{shared}

	results := FuseRRF(sem, str, 10, 60)

	// shared should be first (highest score)
	if len(results) < 2 {
		t.Fatalf("expected >= 2 results, got %d", len(results))
	}
	if results[0].Filepath != "a.go" || results[0].Name != "Foo" {
		t.Errorf("expected shared result first, got %+v", results[0])
	}
}

// TestFuseRRF_RanksByScore verifies that earlier rank produces higher score.
func TestFuseRRF_RanksByScore(t *testing.T) {
	t.Parallel()
	r1 := makeResult("a.go", "First", 1)
	r2 := makeResult("b.go", "Second", 1)
	r3 := makeResult("c.go", "Third", 1)

	sem := []SearchResult{r1, r2, r3}
	str := []SearchResult{}

	results := FuseRRF(sem, str, 10, 60)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Rank 0 (r1) gets highest RRF score, rank 2 (r3) gets lowest.
	if results[0].Name != "First" {
		t.Errorf("expected First ranked first, got %s", results[0].Name)
	}
	if results[2].Name != "Third" {
		t.Errorf("expected Third ranked last, got %s", results[2].Name)
	}
}

// TestFuseRRF_RespectsLimit verifies that output length does not exceed limit.
func TestFuseRRF_RespectsLimit(t *testing.T) {
	t.Parallel()
	sem := []SearchResult{
		makeResult("a.go", "A", 1),
		makeResult("b.go", "B", 1),
		makeResult("c.go", "C", 1),
		makeResult("d.go", "D", 1),
		makeResult("e.go", "E", 1),
	}
	str := []SearchResult{}

	results := FuseRRF(sem, str, 3, 60)
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}

// TestFuseRRF_DefaultK verifies that passing k<=0 produces the same result as k=60.
func TestFuseRRF_DefaultK(t *testing.T) {
	t.Parallel()
	sem := []SearchResult{
		makeResult("a.go", "A", 1),
		makeResult("b.go", "B", 1),
	}
	str := []SearchResult{
		makeResult("a.go", "A", 1),
	}

	withDefault := FuseRRF(sem, str, 10, 0)
	withExplicit := FuseRRF(sem, str, 10, 60)

	if len(withDefault) != len(withExplicit) {
		t.Fatalf("length mismatch: default=%d explicit=%d", len(withDefault), len(withExplicit))
	}
	for i := range withDefault {
		if withDefault[i].Filepath != withExplicit[i].Filepath ||
			withDefault[i].Name != withExplicit[i].Name ||
			withDefault[i].Score != withExplicit[i].Score {
			t.Errorf("result[%d] mismatch: default=%+v explicit=%+v", i, withDefault[i], withExplicit[i])
		}
	}
}

// TestFuseRRF_EmptyInputs verifies graceful handling of empty lists.
func TestFuseRRF_EmptyInputs(t *testing.T) {
	t.Parallel()
	results := FuseRRF(nil, nil, 5, 60)
	if len(results) != 0 {
		t.Errorf("expected empty result, got %d items", len(results))
	}
}
