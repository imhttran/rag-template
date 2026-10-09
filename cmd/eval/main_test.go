package main

import (
	"crypto/sha256"
	"encoding/hex"
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

// manifestEntry maps a dataset source name to its corpus file and content hash.
type manifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// kindedCase is an EvalCase carrying the deterministic id and direction/class
// label the datasets use to distribute cases across retrieval directions and
// case classes. The production EvalCase intentionally omits id and kind; the
// eval harness only needs them for coverage and leakage assertions.
type kindedCase struct {
	EvalCase
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// answerableKinds is the set of answerable-class kind labels, each naming one
// retrieval direction. It is the single source of truth for both the per-kind
// count and the answerable total, and expandedDatasetKinds below adds the two
// non-answerable classes, so an unrecognised kind label fails the test instead
// of silently dropping out of the answerable count.
var answerableKinds = map[string]bool{
	"en-en": true,
	"vi-vi": true,
	"en-vi": true,
	"vi-en": true,
}

var nonAnswerableKinds = map[string]bool{
	"unanswerable": true,
	"misleading":   true,
}

// sectionText returns the text of a markdown section, keyed by its heading.
// Sections run from a "## <name>" heading up to the next "## " heading (a "###"
// subsection stays inside its parent section, matching how the corpus chunks).
func sectionText(document, section string) (string, bool) {
	heading := "## " + section

	lines := strings.Split(document, "\n")
	start := -1

	for index, line := range lines {
		if strings.TrimSpace(line) == strings.TrimSpace(heading) {
			start = index + 1

			break
		}
	}

	if start == -1 {
		return "", false
	}

	var body strings.Builder

	for index := start; index < len(lines); index++ {
		line := lines[index]

		if strings.HasPrefix(line, "## ") {
			break
		}

		body.WriteString(line)
		body.WriteString("\n")
	}

	return body.String(), true
}

// The evaluation and calibration splits are distinct committed datasets with
// globally unique case ids: the calibration split selects the operating floor
// and the evaluation split is held out for the final comparison, so the two
// must never share a case.
var (
	expandedDatasetPath    = filepath.Join("..", "..", "evals", "retrieval-vi-en-expanded.json")
	calibrationDatasetPath = filepath.Join("..", "..", "evals", "retrieval-vi-en-calibration.json")
	corpusManifestPath     = filepath.Join("..", "..", "evals", "corpus-vi-en.json")
)

// loadCorpusManifest reads and parses the corpus manifest keyed by source name.
func loadCorpusManifest(t *testing.T, path string) map[string]manifestEntry {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read corpus manifest %s: %v", path, err)
	}

	var manifest map[string]manifestEntry

	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse corpus manifest %s: %v", path, err)
	}

	return manifest
}

// loadKindDataset reads and parses a kinded evaluation/calibration dataset.
func loadKindDataset(t *testing.T, path string) []kindedCase {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dataset %s: %v", path, err)
	}

	var cases []kindedCase

	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse dataset %s: %v", path, err)
	}

	if len(cases) == 0 {
		t.Fatalf("dataset %s has no cases", path)
	}

	return cases
}

// validateAgainstManifest validates a kinded dataset against the corpus
// manifest: every case has a non-empty, unique id and a recognised kind, every
// expected source is present in the manifest with a matching content hash,
// every referenced section exists in that source file, and every evidence
// string is a verbatim substring of that section's text. It returns the
// per-kind case counts. It is read-only over the committed evals/ artifacts and
// does not touch the production scoring path.
func validateAgainstManifest(
	t *testing.T,
	cases []kindedCase,
	manifest map[string]manifestEntry,
) map[string]int {
	t.Helper()

	// Cache file contents so each corpus file is read once per test run, and so
	// a tampered manifest hash is reported before any label is trusted.
	documents := make(map[string]string)
	loaded := make(map[string]bool)

	loadSource := func(source string) (string, bool) {
		if loaded[source] {
			return documents[source], true
		}

		entry, ok := manifest[source]
		if !ok {
			return "", false
		}

		path := filepath.Join("..", "..", entry.Path)

		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("source %q: read corpus file %s: %v", source, entry.Path, err)
		}

		sum := sha256.Sum256(contents)

		digest := hex.EncodeToString(sum[:])
		if entry.SHA256 != "" && !strings.EqualFold(digest, entry.SHA256) {
			t.Fatalf(
				"source %q: content hash mismatch for %s: manifest=%s computed=%s",
				source,
				entry.Path,
				entry.SHA256,
				digest,
			)
		}

		documents[source] = string(contents)
		loaded[source] = true

		return documents[source], true
	}

	counts := map[string]int{}
	seenIDs := map[string]bool{}

	for index, evalCase := range cases {
		kind := strings.TrimSpace(evalCase.Kind)
		if kind == "" {
			t.Fatalf("case %d (%q): empty kind label", index, evalCase.Question)
		}

		if !answerableKinds[kind] && !nonAnswerableKinds[kind] {
			t.Fatalf(
				"case %d (%q): unknown kind %q; want one of the answerable directions or unanswerable/misleading",
				index,
				evalCase.Question,
				kind,
			)
		}

		id := strings.TrimSpace(evalCase.ID)
		if id == "" {
			t.Fatalf("case %d (%q): empty case id", index, evalCase.Question)
		}

		if seenIDs[id] {
			t.Fatalf("case %d (%q): duplicate case id %q", index, evalCase.Question, id)
		}

		seenIDs[id] = true

		counts[kind]++

		if strings.TrimSpace(evalCase.Question) == "" {
			t.Fatalf("case %d: empty question", index)
		}

		// Unanswerable cases carry no evidence and are asserted separately.
		if kind == "unanswerable" {
			if len(evalCase.Expected) != 0 {
				t.Fatalf("case %d (%q): unanswerable case must have no expected sources", index, evalCase.Question)
			}

			continue
		}

		if len(evalCase.Expected) == 0 {
			t.Fatalf("case %d (%q): answerable kind %q has no expected sources", index, evalCase.Question, kind)
		}

		for expectedIndex, expected := range evalCase.Expected {
			if strings.TrimSpace(expected.Source) == "" {
				t.Fatalf("case %d (%q): expected %d has an empty source", index, evalCase.Question, expectedIndex)
			}

			if strings.TrimSpace(expected.Section) == "" {
				t.Fatalf("case %d (%q): expected %d (%s) has an empty section", index, evalCase.Question, expectedIndex, expected.Source)
			}

			document, ok := loadSource(expected.Source)
			if !ok {
				t.Fatalf(
					"case %d (%q): source %q is not in the corpus manifest",
					index,
					evalCase.Question,
					expected.Source,
				)
			}

			section, ok := sectionText(document, expected.Section)
			if !ok {
				t.Fatalf(
					"case %d (%q): section %q does not exist in source %q",
					index,
					evalCase.Question,
					expected.Section,
					expected.Source,
				)
			}

			for _, evidence := range expected.Evidence {
				if strings.TrimSpace(evidence) == "" {
					t.Fatalf("case %d (%q): empty evidence string", index, evalCase.Question)
				}

				if !strings.Contains(section, evidence) {
					t.Fatalf(
						"case %d (%q): evidence %q is not a substring of section %q in source %q",
						index,
						evalCase.Question,
						evidence,
						expected.Section,
						expected.Source,
					)
				}
			}
		}
	}

	return counts
}

// assertDistribution checks a dataset meets its required per-kind distribution:
// the four answerable directions must each be non-empty and together reach
// minAnswerable, unanswerable cases must reach minUnanswerable (only when > 0),
// and misleading-context cases must reach minMisleading (only when > 0).
func assertDistribution(
	t *testing.T,
	label string,
	counts map[string]int,
	minAnswerable int,
	minUnanswerable int,
	minMisleading int,
) {
	t.Helper()

	answerable := 0

	for kind := range answerableKinds {
		answerable += counts[kind]

		if counts[kind] == 0 {
			t.Errorf("%s: dataset has no %s (answerable) cases; want at least one", label, kind)
		}
	}

	if answerable < minAnswerable {
		t.Errorf("%s: dataset has %d answerable cases, want at least %d", label, answerable, minAnswerable)
	}

	if minUnanswerable > 0 {
		if got := counts["unanswerable"]; got < minUnanswerable {
			t.Errorf("%s: dataset has %d unanswerable cases, want at least %d", label, got, minUnanswerable)
		}
	}

	if minMisleading > 0 {
		if got := counts["misleading"]; got < minMisleading {
			t.Errorf("%s: dataset has %d misleading-context cases, want at least %d", label, got, minMisleading)
		}
	}

	// Report the distribution in the test log for auditability.
	t.Logf("%s dataset distribution: %v", label, counts)
}

// TestExpandedDatasetAgainstManifest validates the held-out evaluation split:
// >=30 answerable cases across the four retrieval directions, >=6 unanswerable,
// and >=3 misleading-context, all labelled against the corpus manifest.
func TestExpandedDatasetAgainstManifest(t *testing.T) {
	manifest := loadCorpusManifest(t, corpusManifestPath)
	cases := loadKindDataset(t, expandedDatasetPath)
	counts := validateAgainstManifest(t, cases, manifest)

	assertDistribution(t, "evaluation", counts, 30, 6, 3)
}

// TestCalibrationDatasetAgainstManifest validates the calibration split with the
// same manifest checks the evaluation split gets, so a floor can only be tuned
// on labels whose sources, sections, and evidence are verified.
func TestCalibrationDatasetAgainstManifest(t *testing.T) {
	manifest := loadCorpusManifest(t, corpusManifestPath)
	cases := loadKindDataset(t, calibrationDatasetPath)
	counts := validateAgainstManifest(t, cases, manifest)

	assertDistribution(t, "calibration", counts, 10, 4, 0)
}

// TestCalibrationAndEvaluationAreDisjoint is the leakage guard: the calibration
// split used to select the operating floor and the held-out evaluation split
// must share no case id and no question, so a floor tuned on calibration is
// never scored against the same case it was tuned for.
func TestCalibrationAndEvaluationAreDisjoint(t *testing.T) {
	evaluation := loadKindDataset(t, expandedDatasetPath)
	calibration := loadKindDataset(t, calibrationDatasetPath)

	ids := map[string]string{}
	questions := map[string]string{}

	index := func(label string, cases []kindedCase) {
		for _, evalCase := range cases {
			id := strings.TrimSpace(evalCase.ID)
			if id == "" {
				t.Fatalf("%s: empty case id for question %q", label, evalCase.Question)
			}

			if other, ok := ids[id]; ok {
				t.Fatalf("case id %q is shared by the %s and %s splits", id, other, label)
			}

			ids[id] = label

			question := strings.ToLower(strings.TrimSpace(evalCase.Question))
			if other, ok := questions[question]; ok {
				t.Fatalf("question %q is shared by the %s and %s splits", evalCase.Question, other, label)
			}

			questions[question] = label
		}
	}

	index("calibration", calibration)
	index("evaluation", evaluation)

	t.Logf(
		"verified %d calibration and %d evaluation cases share no id or question",
		len(calibration),
		len(evaluation),
	)
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
