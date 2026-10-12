package main

import "testing"

// REGRESSION(F7): eval's extractCitedClaims now delegates to the shared
// internal/citations parser rather than a local bracket scanner, so one parser
// serves cmd/rag, cmd/eval, and every other consumer. The shared parser treats a
// trailing segment as a page only when it matches p.N, so for
// "[manual.pdf - Page 3 - p.3]" it yields Source "manual.pdf", Section
// "Page 3", Page 3 (the page is parsed out of the section).
//
// This test is updated (RAG-023C) from the A4 page-citation characterization
// baseline to assert the corrected shared-parser behavior.
func TestExtractCitedClaimsParsesSharedCitation(t *testing.T) {
	const answer = "Grace period applies [manual.pdf - Page 3 - p.3]"

	claims := extractCitedClaims(answer)
	if len(claims) != 1 {
		t.Fatalf(
			"extractCitedClaims(%q) returned %d claims, want exactly 1: %+v",
			answer,
			len(claims),
			claims,
		)
	}

	got := claims[0]
	if got.Source != "manual.pdf" {
		t.Fatalf("claim source = %q, want %q", got.Source, "manual.pdf")
	}

	// The shared parser parses out the page segment, so the section is just
	// "Page 3"; the page form is not kept inside the section.
	if got.Section != "Page 3" {
		t.Fatalf(
			"claim section = %q, want %q (shared parser parses the page out)",
			got.Section,
			"Page 3",
		)
	}
}

// TestExtractCitedClaimsMultiDashSection asserts the shared parser is what gives
// eval its section-title handling: a section title containing " - " is kept
// whole rather than split into section/page.
func TestExtractCitedClaimsMultiDashSection(t *testing.T) {
	const answer = "Interest accrues [Source - Late Payments - Grace Period]"

	claims := extractCitedClaims(answer)
	if len(claims) != 1 {
		t.Fatalf("extractCitedClaims(%q) = %+v, want exactly one claim", answer, claims)
	}

	if claims[0].Source != "Source" || claims[0].Section != "Late Payments - Grace Period" {
		t.Fatalf(
			"claim = %+v, want source %q section %q",
			claims[0],
			"Source",
			"Late Payments - Grace Period",
		)
	}
}

// TestExtractCitedClaimsMultiplePerLine asserts multiple citations on one line
// each pair with the claim text preceding them, and that claim text is trimmed
// (leading "-", trailing ".").
func TestExtractCitedClaimsMultiplePerLine(t *testing.T) {
	const answer = "- First fact. [a.md - Fees] and - second fact. [b.md - Refunds]"

	claims := extractCitedClaims(answer)
	if len(claims) != 2 {
		t.Fatalf("extractCitedClaims(%q) = %+v, want exactly two claims", answer, claims)
	}

	if claims[0].Claim != "First fact" || claims[0].Source != "a.md" || claims[0].Section != "Fees" {
		t.Fatalf("claim[0] = %+v, want claim %q source %q section %q", claims[0], "First fact", "a.md", "Fees")
	}

	if claims[1].Claim != "second fact" || claims[1].Source != "b.md" || claims[1].Section != "Refunds" {
		t.Fatalf("claim[1] = %+v, want claim %q source %q section %q", claims[1], "second fact", "b.md", "Refunds")
	}
}

// TestExtractCitedClaimsSkipsNonCitations asserts the shared parser's rule is
// inherited: a bracket without a " - " separator is not a citation and yields
// no claim.
func TestExtractCitedClaimsSkipsNonCitations(t *testing.T) {
	claims := extractCitedClaims("see [a.md-Fees] here")
	if len(claims) != 0 {
		t.Fatalf("extractCitedClaims = %+v, want no claims for a bracket without a separator", claims)
	}
}
