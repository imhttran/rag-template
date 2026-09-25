package retrieval

import (
	"slices"
	"testing"
)

func TestFuseRankings(t *testing.T) {
	vector := []Document{
		{ID: 1, Section: "Payments"},
		{ID: 2, Section: "Payment Codes"},
		{ID: 3, Section: "Collections"},
	}

	keyword := []Document{
		{ID: 2, Section: "Payment Codes"},
	}

	got := FuseRankings([][]Document{vector, keyword}, 3)

	if len(got) != 3 {
		t.Fatalf("expected 3 documents, got %d", len(got))
	}

	if got[0].ID != 2 {
		t.Fatalf(
			"expected Payment Codes first, got document %d",
			got[0].ID,
		)
	}

	if got[1].ID != 1 {
		t.Fatalf(
			"expected Payments second, got document %d",
			got[1].ID,
		)
	}
}

// FuseRankings ranges over a map, so equal scores need a total order or the
// output order is random. It breaks score ties by best rank, then by ID.
func TestFuseRankingsBreaksScoreTies(t *testing.T) {
	// Document 2 scores 1/61 from a single first-place hit (best rank 1).
	// Document 1 scores the same 1/61 from two 62nd-place hits (best rank 62),
	// but has the lower ID, so only the best-rank tie-break can put document 2
	// first.
	fillers := make([]Document, 61)

	for i := range fillers {
		fillers[i] = Document{ID: int64(1000 + i)}
	}

	longList := append(append([]Document{}, fillers...), Document{ID: 1})

	got := FuseRankings(
		[][]Document{{{ID: 2}}, longList, longList},
		200,
	)

	if last := ids(got[len(got)-2:]); !slices.Equal(last, []int64{2, 1}) {
		t.Fatalf("expected best rank to order the score tie [2 1], got %v", last)
	}

	// The same ranks in swapped lists give documents 1 and 2 an identical score
	// and best rank, so ID order decides.
	got = FuseRankings(
		[][]Document{
			{{ID: 2}, {ID: 1}},
			{{ID: 1}, {ID: 2}},
		},
		2,
	)

	if ordered := ids(got); !slices.Equal(ordered, []int64{1, 2}) {
		t.Fatalf("expected ID order [1 2], got %v", ordered)
	}
}
