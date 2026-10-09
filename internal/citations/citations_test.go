package citations

import (
	"testing"

	"rag-template/internal/retrieval"
)

func TestValidity(t *testing.T) {
	documents := []retrieval.Document{
		{Source: "a.md", Section: "Fees"},
	}

	tests := []struct {
		name   string
		answer string
		valid  int
		total  int
	}{
		{name: "no citations", answer: "no citations here", valid: 0, total: 0},
		{name: "valid citation", answer: "see [a.md - Fees].", valid: 1, total: 1},
		{name: "unknown section counts as a citation but not valid", answer: "see [a.md - Refunds].", valid: 0, total: 1},
		{name: "two citations", answer: "[a.md - Fees] and [a.md - Refunds]", valid: 1, total: 2},
		{name: "unclosed bracket is not a citation", answer: "see [a.md - Fees", valid: 0, total: 0},
		{name: "a bracket without a separator is not a citation", answer: "see [a.md-Fees]", valid: 0, total: 0},
		{name: "extra spaces are trimmed", answer: "see [ a.md  -  Fees ]", valid: 1, total: 1},
		{name: "a stray closing bracket first", answer: "note] then [a.md - Fees]", valid: 1, total: 1},
		{name: "a non-citation bracket then a real one", answer: "[x] [a.md - Fees]", valid: 1, total: 1},
		{name: "a source without the .md suffix is still valid", answer: "see [a - Fees].", valid: 1, total: 1},
		{name: "source capitalization is ignored", answer: "see [A.MD - Fees].", valid: 1, total: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			valid, total := Validity(test.answer, documents)

			if valid != test.valid || total != test.total {
				t.Fatalf(
					"Validity(%q) = (%d, %d), want (%d, %d)",
					test.answer,
					valid,
					total,
					test.valid,
					test.total,
				)
			}
		})
	}
}

func TestNormalizeCitation(t *testing.T) {
	tests := map[string]string{
		"a - b":      "a - b",
		"a \u2013 b": "a - b",
		"a \u2014 b": "a - b",
	}

	for input, want := range tests {
		if got := NormalizeCitation(input); got != want {
			t.Fatalf("NormalizeCitation(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeSource(t *testing.T) {
	tests := map[string]string{
		"a.md":   "a",
		"A.MD":   "a",
		" A.md ": "a",
		"b":      "b",
	}

	for input, want := range tests {
		if got := NormalizeSource(input); got != want {
			t.Fatalf("NormalizeSource(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestStrip(t *testing.T) {
	documents := []retrieval.Document{
		{Source: "a.md", Section: "Fees"},
	}

	tests := []struct {
		name     string
		answer   string
		want     string
		stripped int
	}{
		{
			name:     "valid citation kept",
			answer:   "fees apply [a.md - Fees].",
			want:     "fees apply [a.md - Fees].",
			stripped: 0,
		},
		{
			name:     "invented citation stripped",
			answer:   "fees apply [b.md - Refunds].",
			want:     "fees apply .",
			stripped: 1,
		},
		{
			name:     "mixed keeps only valid",
			answer:   "a [a.md - Fees] b [b.md - Other]",
			want:     "a [a.md - Fees] b ",
			stripped: 1,
		},
		{
			name:     "adjacent words are not joined",
			answer:   "foo[b.md - X]bar",
			want:     "foo bar",
			stripped: 1,
		},
		{
			name:     "citation already bounded by punctuation needs no separator",
			answer:   "foo.[b.md - X].bar",
			want:     "foo..bar",
			stripped: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, stripped := Strip(test.answer, documents)

			if got != test.want || stripped != test.stripped {
				t.Fatalf(
					"Strip(%q) = (%q, %d), want (%q, %d)",
					test.answer,
					got,
					stripped,
					test.want,
					test.stripped,
				)
			}
		})
	}
}
