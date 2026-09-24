package main

import (
	"testing"

	"rag-template/internal/retrieval"
)

// retrieved builds a result document; only the fields the metrics read matter.
func retrieved(source, section string, chunk int) retrieval.Document {
	return retrieval.Document{
		Source:     source,
		Section:    section,
		ChunkIndex: chunk,
	}
}

func TestRecallAtK(t *testing.T) {
	expected := []ExpectedDocument{
		{Source: "a.md", Section: "Fees"},
		{Source: "b.md", Section: "Refunds"},
	}

	tests := []struct {
		name   string
		actual []retrieval.Document
		want   float64
	}{
		{
			name:   "everything retrieved",
			actual: []retrieval.Document{retrieved("a.md", "Fees", 0), retrieved("b.md", "Refunds", 1)},
			want:   1,
		},
		{
			name:   "half retrieved",
			actual: []retrieval.Document{retrieved("a.md", "Fees", 0)},
			want:   0.5,
		},
		{
			name:   "nothing relevant retrieved",
			actual: []retrieval.Document{retrieved("c.md", "Other", 0)},
			want:   0,
		},
		{
			name:   "nothing retrieved",
			actual: nil,
			want:   0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := recallAtK(expected, test.actual); got != test.want {
				t.Fatalf("recallAtK = %v, want %v", got, test.want)
			}
		})
	}

	if got := recallAtK(nil, []retrieval.Document{retrieved("a.md", "Fees", 0)}); got != 0 {
		t.Fatalf("recallAtK with no expected documents = %v, want 0", got)
	}
}

func TestPrecisionAtK(t *testing.T) {
	expected := []ExpectedDocument{
		{Source: "a.md", Section: "Fees"},
	}

	tests := []struct {
		name   string
		actual []retrieval.Document
		want   float64
	}{
		{
			name:   "relevant only",
			actual: []retrieval.Document{retrieved("a.md", "Fees", 0)},
			want:   1,
		},
		{
			name:   "half relevant",
			actual: []retrieval.Document{retrieved("a.md", "Fees", 0), retrieved("b.md", "Other", 0)},
			want:   0.5,
		},
		{
			name: "a repeated section counts once",
			actual: []retrieval.Document{
				retrieved("a.md", "Fees", 0),
				retrieved("a.md", "Fees", 1),
				retrieved("b.md", "Other", 0),
			},
			want: 0.5,
		},
		{
			name:   "nothing retrieved",
			actual: nil,
			want:   0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := precisionAtK(expected, test.actual); got != test.want {
				t.Fatalf("precisionAtK = %v, want %v", got, test.want)
			}
		})
	}
}

func TestEvidenceRecall(t *testing.T) {
	expected := []ExpectedDocument{
		{
			Source:   "a.md",
			Section:  "Missed Payments",
			Evidence: []string{"10-day grace period", "fifteen days"},
		},
	}

	tests := []struct {
		name   string
		actual []retrieval.Document
		want   float64
	}{
		{
			name: "both substrings present",
			actual: []retrieval.Document{
				{Source: "a.md", Section: "Missed Payments", Content: "the loan enters a 10-day grace period, unlike the fifteen days elsewhere"},
			},
			want: 1,
		},
		{
			name: "one substring present",
			actual: []retrieval.Document{
				{Source: "a.md", Section: "Missed Payments", Content: "a 10-day grace period applies"},
			},
			want: 0.5,
		},
		{
			name: "matching is case insensitive",
			actual: []retrieval.Document{
				{Source: "a.md", Section: "Missed Payments", Content: "A 10-DAY GRACE PERIOD and FIFTEEN DAYS after"},
			},
			want: 1,
		},
		{
			name: "the wrong section does not match",
			actual: []retrieval.Document{
				{Source: "a.md", Section: "Late Fees", Content: "a 10-day grace period and fifteen days"},
			},
			want: 0,
		},
		{
			name:   "nothing retrieved",
			actual: nil,
			want:   0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := evidenceRecall(expected, test.actual); got != test.want {
				t.Fatalf("evidenceRecall = %v, want %v", got, test.want)
			}
		})
	}

	if got := evidenceRecall(expected, nil); got != 0 {
		t.Fatalf("evidenceRecall with nothing retrieved = %v, want 0", got)
	}
}

func TestCitationValidity(t *testing.T) {
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
			valid, total := citationValidity(test.answer, documents)

			if valid != test.valid || total != test.total {
				t.Fatalf(
					"citationValidity(%q) = (%d, %d), want (%d, %d)",
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

func TestAverage(t *testing.T) {
	if got := average(6, 2); got != 3 {
		t.Fatalf("average(6, 2) = %v, want 3", got)
	}

	// A count of zero must not produce NaN.
	if got := average(6, 0); got != 0 {
		t.Fatalf("average(6, 0) = %v, want 0", got)
	}
}
