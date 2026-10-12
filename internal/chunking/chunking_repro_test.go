package chunking

import (
	"testing"

	"rag-template/internal/document"
)

// REGRESSION(F6): chunking.FromSections now numbers chunks with a single
// document-global counter, so Index is strictly increasing across every section
// and never restarts at 0 for a repeated heading. Repeated headings expand per
// section in source order, continuing the numbering.
//
// This baseline is updated (RAG-023B, stage S6) from the A2 characterization
// test that pinned the old per-section restart, so it now asserts the corrected
// behavior.
func TestFromSectionsGlobalIndexAcrossRepeatedHeadings(t *testing.T) {
	const (
		chunkSize = 3
		overlap   = 0
	)

	// Six words with chunkSize 3 and no overlap yields exactly two chunks per
	// section: [w1 w2 w3] and [w4 w5 w6].
	const content = "one two three four five six"

	sections := []document.Section{
		{Title: "Shared Heading", Content: content},
		{Title: "Shared Heading", Content: content},
	}

	chunks := FromSections(sections, chunkSize, overlap)

	if len(chunks) != 4 {
		t.Fatalf("FromSections returned %d chunks, want 4 (two per section)", len(chunks))
	}

	// Every chunk must carry the shared section title, expanded in order.
	for i, chunk := range chunks {
		if chunk.Section != "Shared Heading" {
			t.Errorf("chunk %d Section = %q, want %q", i, chunk.Section, "Shared Heading")
		}
	}

	// The document-global index is strictly increasing and matches the
	// concatenated position across sections: 0, 1, 2, 3.
	wantIndexes := []int{0, 1, 2, 3}

	for i, chunk := range chunks {
		if chunk.Index != wantIndexes[i] {
			t.Errorf("chunk %d Index = %d, want %d (document-global numbering)", i, chunk.Index, wantIndexes[i])
		}
	}

	// The indexes must be strictly increasing with no per-section restart.
	for i := 1; i < len(chunks); i++ {
		if chunks[i].Index <= chunks[i-1].Index {
			t.Errorf(
				"chunk %d Index = %d is not greater than chunk %d Index = %d; numbering must not restart per section",
				i,
				chunks[i].Index,
				i-1,
				chunks[i-1].Index,
			)
		}
	}
}
