package document

import "testing"

// TestSetPages asserts page numbers are assigned in order and that an unset
// section keeps Page 0 when pages run out.
func TestSetPages(t *testing.T) {
	sections := []Section{{Title: "A"}, {Title: "B"}, {Title: "C"}}

	SetPages(sections, []int{1, 2})

	want := []int{1, 2, 0}

	for i, section := range sections {
		if section.Page != want[i] {
			t.Fatalf("section %d Page = %d, want %d", i, section.Page, want[i])
		}
	}
}

// TestSetPagesDoesNotOverwrite asserts an already-set page is preserved.
func TestSetPagesDoesNotOverwrite(t *testing.T) {
	sections := []Section{{Title: "A", Page: 5}}

	SetPages(sections, []int{1})

	if sections[0].Page != 5 {
		t.Fatalf("Page = %d, want 5 (unchanged)", sections[0].Page)
	}
}

// TestParseSectionsHasNoPage asserts Markdown sections carry no page.
func TestParseSectionsHasNoPage(t *testing.T) {
	for _, section := range ParseSections("## A\nx\n## B\ny") {
		if section.Page != 0 {
			t.Fatalf("Markdown section %q Page = %d, want 0", section.Title, section.Page)
		}
	}
}
