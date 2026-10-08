package chunking

import (
	"testing"

	"rag-template/internal/document"
)

// TestFromSectionsCarriesPage asserts each chunk inherits its source section's
// page, and a page-less section yields page 0.
func TestFromSectionsCarriesPage(t *testing.T) {
	chunks := FromSections([]document.Section{
		{Title: "A", Content: "one two three four five", Page: 7},
	}, 50, 0)

	if len(chunks) == 0 {
		t.Fatal("no chunks produced")
	}

	for i, chunk := range chunks {
		if chunk.Page != 7 {
			t.Fatalf("chunk %d Page = %d, want 7", i, chunk.Page)
		}
	}

	pageLess := FromSections([]document.Section{
		{Title: "B", Content: "one two three"},
	}, 50, 0)

	if len(pageLess) == 0 {
		t.Fatal("no chunks produced")
	}

	if pageLess[0].Page != 0 {
		t.Fatalf("page-less chunk Page = %d, want 0", pageLess[0].Page)
	}
}
