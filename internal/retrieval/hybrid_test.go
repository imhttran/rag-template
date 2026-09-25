package retrieval

import "testing"

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
