package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rag-template/internal/citations"
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

func TestDatasetPath(t *testing.T) {
	tests := []struct {
		name  string
		set   bool
		value string
		want  string
	}{
		{name: "unset uses the default", set: false, want: "evals/retrieval.json"},
		{name: "empty uses the default", set: true, value: "", want: "evals/retrieval.json"},
		{name: "whitespace uses the default", set: true, value: "   ", want: "evals/retrieval.json"},
		{name: "a set path is used", set: true, value: "evals/retrieval-vi-en.json", want: "evals/retrieval-vi-en.json"},
		{name: "an alternative path is used", set: true, value: "/tmp/cases.json", want: "/tmp/cases.json"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.set {
				t.Setenv("EVAL_DATASET", test.value)
			} else {
				// t.Setenv then Unsetenv guarantees the variable is absent
				// even if the ambient environment defines it.
				t.Setenv("EVAL_DATASET", "placeholder")
				os.Unsetenv("EVAL_DATASET")
			}

			if got := datasetPath(); got != test.want {
				t.Fatalf("datasetPath() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLoadCasesErrors(t *testing.T) {
	dir := t.TempDir()

	malformed := filepath.Join(dir, "malformed.json")
	if err := os.WriteFile(malformed, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write malformed fixture: %v", err)
	}

	missing := filepath.Join(dir, "missing.json")

	tests := []struct {
		name string
		path string
	}{
		{name: "missing file", path: missing},
		{name: "malformed json", path: malformed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("EVAL_DATASET", test.path)

			_, err := loadCases()
			if err == nil {
				t.Fatalf("loadCases() = nil error, want an error naming %q", test.path)
			}

			if !strings.Contains(err.Error(), test.path) {
				t.Fatalf("loadCases() error = %q, want it to contain %q", err.Error(), test.path)
			}
		})
	}
}

func TestLoadCasesSelectsDataset(t *testing.T) {
	dir := t.TempDir()

	selected := filepath.Join(dir, "cases.json")
	fixture := `[{"question":"q","expected":[{"source":"a.md","section":"S"}]}]`

	if err := os.WriteFile(selected, []byte(fixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Setenv("EVAL_DATASET", selected)

	cases, err := loadCases()
	if err != nil {
		t.Fatalf("loadCases() error = %v", err)
	}

	if len(cases) != 1 || cases[0].Question != "q" {
		t.Fatalf("loadCases() = %+v, want one case with question %q", cases, "q")
	}
}

// TestBilingualDatasetLoads asserts the shipped bilingual dataset parses and
// covers the retrieval directions plus an unanswerable case.
func TestBilingualDatasetLoads(t *testing.T) {
	path := filepath.Join("..", "..", "evals", "retrieval-vi-en.json")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bilingual dataset: %v", err)
	}

	var cases []EvalCase

	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse bilingual dataset: %v", err)
	}

	if len(cases) < 5 {
		t.Fatalf("bilingual dataset has %d cases, want at least 5", len(cases))
	}

	var answerable, unanswerable int

	for _, evalCase := range cases {
		if strings.TrimSpace(evalCase.Question) == "" {
			t.Fatal("bilingual dataset has an empty question")
		}

		if len(evalCase.Expected) == 0 {
			unanswerable++

			continue
		}

		answerable++
	}

	if answerable == 0 {
		t.Fatal("bilingual dataset has no answerable cases")
	}

	if unanswerable == 0 {
		t.Fatal("bilingual dataset has no unanswerable case")
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
			valid, total := citations.Validity(test.answer, documents)

			if valid != test.valid || total != test.total {
				t.Fatalf(
					"citations.Validity(%q) = (%d, %d), want (%d, %d)",
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
