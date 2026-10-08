package contextbudget

import (
	"reflect"
	"testing"

	"rag-template/internal/retrieval"
)

// doc builds a document with content of the requested length. Section identity
// is (source, section); chunk index is carried through for readability only.
func doc(source, section string, index, size int) retrieval.Document {
	content := make([]byte, size)
	for i := range content {
		content[i] = 'x'
	}

	return retrieval.Document{
		Source:     source,
		Section:    section,
		ChunkIndex: index,
		Content:    string(content),
	}
}

// TestBuildNeverExceedsBudget asserts the central guarantee on a variety of
// inputs: the summed estimate of the selection never exceeds the budget.
func TestBuildNeverExceedsBudget(t *testing.T) {
	inputs := [][]retrieval.Document{
		nil,
		{},
		{doc("a", "s1", 0, 10)},
		{
			doc("a", "s1", 0, 100),
			doc("a", "s2", 0, 100),
			doc("b", "s1", 0, 100),
		},
		{
			doc("a", "s1", 0, 5),
			doc("a", "s1", 1, 5),
			doc("a", "s1", 2, 5),
			doc("a", "s2", 0, 5),
			doc("b", "s1", 0, 5),
		},
	}

	budgets := []int{-1, 0, 1, 20, 50, 200, 1000}

	for _, documents := range inputs {
		for _, budget := range budgets {
			kept := Build(documents, budget)

			if budget <= 0 {
				if !reflect.DeepEqual(kept, documents) {
					t.Fatalf("budget %d: got %v, want input unchanged", budget, kept)
				}

				continue
			}

			if got := EstimateAll(kept); got > budget {
				t.Fatalf("budget %d: selected estimate %d exceeds budget (kept %d docs)", budget, got, len(kept))
			}
		}
	}
}

// TestBuildZeroOrNegativeReturnsInputUnchanged pins the fallback that preserves
// the current chunk-count behaviour.
func TestBuildZeroOrNegativeReturnsInputUnchanged(t *testing.T) {
	documents := []retrieval.Document{
		doc("a", "s1", 0, 500),
		doc("a", "s2", 0, 500),
	}

	for _, budget := range []int{0, -1, -100} {
		kept := Build(documents, budget)

		if !reflect.DeepEqual(kept, documents) {
			t.Fatalf("budget %d: got %v, want input unchanged", budget, kept)
		}
	}
}

// TestBuildEmptyInputYieldsEmptyOutput asserts an empty input produces no
// documents regardless of budget.
func TestBuildEmptyInputYieldsEmptyOutput(t *testing.T) {
	for _, budget := range []int{0, 100} {
		kept := Build(nil, budget)
		if len(kept) != 0 {
			t.Fatalf("budget %d: got %d docs, want 0", budget, len(kept))
		}

		kept = Build([]retrieval.Document{}, budget)
		if len(kept) != 0 {
			t.Fatalf("budget %d: got %d docs, want 0", budget, len(kept))
		}
	}
}

// TestBuildHigherRankedSectionsFirst asserts that with a tight budget the
// best-ranked sections are preferred over lower-ranked ones.
func TestBuildHigherRankedSectionsFirst(t *testing.T) {
	high := doc("a", "high", 0, 10)
	low := doc("a", "low", 0, 10)

	documents := []retrieval.Document{high, low}

	// Each chunk costs 10 + 16 = 26. A budget that fits exactly one chunk.
	kept := Build(documents, 26)

	if len(kept) != 1 {
		t.Fatalf("got %d docs, want 1", len(kept))
	}

	if kept[0].Section != "high" {
		t.Fatalf("kept section %q, want %q", kept[0].Section, "high")
	}
}

// TestBuildRoundRobinFairness asserts that across sections each section gets a
// chunk before any gets a second, so one long section cannot starve the others.
func TestBuildRoundRobinFairness(t *testing.T) {
	// Section "long" has three chunks; "other" has one. All estimates equal.
	long0 := doc("a", "long", 0, 10)
	long1 := doc("a", "long", 1, 10)
	long2 := doc("a", "long", 2, 10)
	other0 := doc("b", "other", 0, 10)

	documents := []retrieval.Document{long0, long1, long2, other0}

	// Each chunk costs 10 + 16 = 26. Budget for exactly two chunks.
	kept := Build(documents, 52)

	if len(kept) != 2 {
		t.Fatalf("got %d docs, want 2", len(kept))
	}

	sections := map[string]int{}
	for _, document := range kept {
		sections[document.Section]++
	}

	if sections["other"] != 1 {
		t.Fatalf("other section count = %d, want 1 (must not be starved by long)", sections["other"])
	}

	if sections["long"] != 1 {
		t.Fatalf("long section count = %d, want 1", sections["long"])
	}
}

// TestBuildOversizeChunkExcluded asserts that a single chunk whose own estimate
// exceeds the budget is excluded from the result.
func TestBuildOversizeChunkExcluded(t *testing.T) {
	oversize := doc("a", "s1", 0, 1000)

	kept := Build([]retrieval.Document{oversize}, 100)

	if len(kept) != 0 {
		t.Fatalf("got %d docs, want oversize chunk excluded", len(kept))
	}
}

// TestBuildDeterministicAndStable asserts identical input yields identical,
// stably ordered output across repeated calls.
func TestBuildDeterministicAndStable(t *testing.T) {
	documents := []retrieval.Document{
		doc("a", "s1", 0, 10),
		doc("b", "s2", 0, 10),
		doc("a", "s1", 1, 10),
		doc("c", "s3", 0, 10),
		doc("b", "s2", 1, 10),
	}

	first := Build(documents, 150)

	for i := 0; i < 10; i++ {
		again := Build(documents, 150)

		if !reflect.DeepEqual(first, again) {
			t.Fatalf("run %d: output differs from first call", i)
		}
	}

	// Selection preserves the input ordering of the kept documents.
	lastIndex := -1

	for _, document := range first {
		index := -1

		for i, candidate := range documents {
			if candidate.Source == document.Source &&
				candidate.Section == document.Section &&
				candidate.ChunkIndex == document.ChunkIndex {
				index = i

				break
			}
		}

		if index <= lastIndex {
			t.Fatalf("selection is not in input order: index %d after %d", index, lastIndex)
		}

		lastIndex = index
	}
}

// TestBuildNeverExceedsBudgetStarvationPrevented combines a tight budget with
// one long section and asserts both the guarantee and that the shorter section
// is represented.
func TestBuildNeverExceedsBudgetStarvationPrevented(t *testing.T) {
	documents := []retrieval.Document{
		doc("a", "long", 0, 10),
		doc("a", "long", 1, 10),
		doc("a", "long", 2, 10),
		doc("b", "short", 0, 10),
	}

	// Budget for exactly two chunks; fairness should include the short section.
	kept := Build(documents, 52)

	if got := EstimateAll(kept); got > 52 {
		t.Fatalf("selected estimate %d exceeds budget 52", got)
	}

	var hasShort bool

	for _, document := range kept {
		if document.Section == "short" {
			hasShort = true
		}
	}

	if !hasShort {
		t.Fatalf("short section was starved: kept %v", kept)
	}
}

// TestEstimateIsContentLengthPlusOverhead pins the estimator contract.
func TestEstimateIsContentLengthPlusOverhead(t *testing.T) {
	document := doc("a", "s1", 0, 42)

	if got := Estimate(document); got != 42+chunkOverhead {
		t.Fatalf("Estimate = %d, want %d", got, 42+chunkOverhead)
	}
}
