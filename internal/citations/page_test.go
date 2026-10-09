package citations

import (
	"testing"

	"rag-template/internal/retrieval"
)

// TestParsePageCitations covers the legacy and page citation forms, including
// rejected page segments.
func TestParsePageCitations(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		want   []Citation
	}{
		{
			name:   "legacy form has no page",
			answer: "see [a.md - Fees]",
			want:   []Citation{{Source: "a.md", Section: "Fees"}},
		},
		{
			name:   "page form parses the page",
			answer: "see [a.md - Fees - p.3]",
			want:   []Citation{{Source: "a.md", Section: "Fees", Page: 3}},
		},
		{
			name:   "page form tolerates surrounding spaces",
			answer: "see [ a.md  -  Fees  -  p.12 ]",
			want:   []Citation{{Source: "a.md", Section: "Fees", Page: 12}},
		},
		{
			name:   "p.0 is not a citation",
			answer: "see [a.md - Fees - p.0]",
			want:   nil,
		},
		{
			name:   "non p.N third segment is not a citation",
			answer: "see [a.md - Fees - page 3]",
			want:   nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Parse(test.answer)

			if len(got) != len(test.want) {
				t.Fatalf("Parse(%q) = %v, want %v", test.answer, got, test.want)
			}

			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("Parse(%q)[%d] = %+v, want %+v", test.answer, i, got[i], test.want[i])
				}
			}
		})
	}
}

// TestValidPageCitation asserts a page citation resolves only to a document with
// that page — in particular a page citation must NOT validate against a document
// whose page is unset (Page 0), and a legacy citation still matches any page.
func TestValidPageCitation(t *testing.T) {
	documents := []retrieval.Document{
		{Source: "a.md", Section: "Fees", Page: 3},
		{Source: "c.md", Section: "Fees", Page: 0}, // page-less sibling
	}

	tests := []struct {
		name     string
		citation Citation
		want     bool
	}{
		{name: "legacy matches a paged document", citation: Citation{Source: "a.md", Section: "Fees"}, want: true},
		{name: "legacy matches a page-less document", citation: Citation{Source: "c.md", Section: "Fees"}, want: true},
		{name: "page citation resolves", citation: Citation{Source: "a.md", Section: "Fees", Page: 3}, want: true},
		{name: "page citation with no such page fails", citation: Citation{Source: "a.md", Section: "Fees", Page: 4}, want: false},
		{name: "page citation against a page-less document fails", citation: Citation{Source: "c.md", Section: "Fees", Page: 3}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Valid(test.citation, documents); got != test.want {
				t.Fatalf("Valid(%+v) = %v, want %v", test.citation, got, test.want)
			}
		})
	}
}

// TestValidityPageCitations asserts the valid/total counts for page citations.
func TestValidityPageCitations(t *testing.T) {
	documents := []retrieval.Document{
		{Source: "a.md", Section: "Fees", Page: 3},
	}

	valid, total := Validity("x [a.md - Fees - p.3] y [a.md - Fees - p.9]", documents)
	if valid != 1 || total != 2 {
		t.Fatalf("Validity() = (%d, %d), want (1, 2)", valid, total)
	}
}

// TestStripPageCitation asserts an invalid page citation is stripped and a valid
// one is kept.
func TestStripPageCitation(t *testing.T) {
	documents := []retrieval.Document{
		{Source: "a.md", Section: "Fees", Page: 3},
	}

	got, stripped := Strip("fees [a.md - Fees - p.3] and [a.md - Fees - p.9].", documents)
	want := "fees [a.md - Fees - p.3] and ."

	if got != want || stripped != 1 {
		t.Fatalf("Strip() = (%q, %d), want (%q, 1)", got, stripped, want)
	}
}
