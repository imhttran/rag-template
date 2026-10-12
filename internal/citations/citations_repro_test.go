package citations

import "testing"

// REGRESSION(F8): section titles that themselves contain the " - " separator.
// The shared parser treats a trailing segment as a page only when it matches
// "p.<positive int>". When the SECTION is a title that contains " - " (for
// example "Late Payments - Grace Period"), the second separator is not
// followed by a p.N segment, so the whole remainder is kept as the section and
// the citation is preserved rather than dropped.
//
// These tests are updated (RAG-023C) from the A4 characterization baseline to
// assert the corrected behavior.
func TestParseCitationSectionWithSeparatorKeepsFullSection(t *testing.T) {
	const citation = "Source - Late Payments - Grace Period"

	got, ok := parseCitation(citation)
	if !ok {
		t.Fatalf(
			"parseCitation(%q) ok = false, want true (a section containing \" - \" is preserved)",
			citation,
		)
	}

	if got.Source != "Source" {
		t.Fatalf("parseCitation(%q).Source = %q, want %q", citation, got.Source, "Source")
	}

	if got.Section != "Late Payments - Grace Period" {
		t.Fatalf(
			"parseCitation(%q).Section = %q, want %q (full section preserved)",
			citation,
			got.Section,
			"Late Payments - Grace Period",
		)
	}

	if got.Page != 0 {
		t.Fatalf("parseCitation(%q).Page = %d, want 0 (no page claim)", citation, got.Page)
	}
}

// TestParseKeepsSectionWithSeparator pins the end-to-end consequence at the
// public entry point: an answer that cites a section title containing " - " is
// parsed with the full section preserved. See REGRESSION(F8) above.
func TestParseKeepsSectionWithSeparator(t *testing.T) {
	const answer = "late payment interest applies [Source - Late Payments - Grace Period]"

	got := Parse(answer)
	if len(got) != 1 {
		t.Fatalf(
			"Parse(%q) = %+v, want exactly one citation (section containing \" - \" is preserved)",
			answer,
			got,
		)
	}

	if got[0].Source != "Source" || got[0].Section != "Late Payments - Grace Period" {
		t.Fatalf(
			"Parse(%q)[0] = %+v, want source %q section %q",
			answer,
			got[0],
			"Source",
			"Late Payments - Grace Period",
		)
	}

	if got[0].Page != 0 {
		t.Fatalf("Parse(%q)[0].Page = %d, want 0", answer, got[0].Page)
	}
}

// TestParsePageFormExtractsPage pins the page form: the trailing segment is a
// page only when it matches p.N.
func TestParsePageFormExtractsPage(t *testing.T) {
	got := Parse("grace applies [manual.pdf - Page 3 - p.3]")
	if len(got) != 1 {
		t.Fatalf("Parse = %+v, want exactly one citation", got)
	}

	if got[0].Source != "manual.pdf" || got[0].Section != "Page 3" || got[0].Page != 3 {
		t.Fatalf(
			"Parse page form = %+v, want source %q section %q page 3",
			got[0],
			"manual.pdf",
			"Page 3",
		)
	}
}

// TestParseNonPageTrailingIsPartOfSection documents the chosen behavior for a
// trailing segment that is not p.N: the parser does not split it out as a page
// and does not drop the citation; the full remainder stays the section.
func TestParseNonPageTrailingIsPartOfSection(t *testing.T) {
	tests := []struct {
		name     string
		citation string
		section  string
	}{
		{name: "two segments", citation: "a.md - Fees", section: "Fees"},
		{name: "multi-dash section", citation: "a.md - Late Payments - Grace Period", section: "Late Payments - Grace Period"},
		{name: "malformed page segment", citation: "a.md - Fees - p.x", section: "Fees - p.x"},
		{name: "zero page", citation: "a.md - Fees - p.0", section: "Fees - p.0"},
		{name: "negative page", citation: "a.md - Fees - p.-1", section: "Fees - p.-1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseCitation(test.citation)
			if !ok {
				t.Fatalf("parseCitation(%q) ok = false, want true", test.citation)
			}

			if got.Section != test.section {
				t.Fatalf("parseCitation(%q).Section = %q, want %q", test.citation, got.Section, test.section)
			}

			if got.Page != 0 {
				t.Fatalf("parseCitation(%q).Page = %d, want 0", test.citation, got.Page)
			}
		})
	}
}

// TestParseMalformedBrackets are not citations: an unclosed bracket and a
// bracket with no " - " separator.
func TestParseMalformedBrackets(t *testing.T) {
	tests := []struct {
		name   string
		answer string
	}{
		{name: "unclosed bracket", answer: "see [a.md - Fees"},
		{name: "no separator", answer: "see [a.md-Fees]"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Parse(test.answer); len(got) != 0 {
				t.Fatalf("Parse(%q) = %+v, want no citations", test.answer, got)
			}
		})
	}
}
