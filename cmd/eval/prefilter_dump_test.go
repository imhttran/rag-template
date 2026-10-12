package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"rag-template/internal/retrieval"
)

// These tests cover the opt-in, evaluation-only EVAL_PREFILTER_DUMP diagnostic
// (RAG-024 RQ-001 amendment). They are deliberately DB-free and deterministic.
//
// The byte-identical claim for the DEFAULT (diagnostic-off) evaluation output is
// verified by a controlled disposable-database run rather than a unit test:
// running `cmd/eval` with EVAL_PREFILTER_DUMP unset reproduces the frozen baseline
// exactly (R@1 0.42 / R@4 0.52 / P@4 0.22 / evidence 0.48 / rejection 6/6) and emits
// zero "Pre-filter" lines, while the same run with the dump on emits the same
// aggregate metrics plus exactly 39x3=117 bounded diagnostic lines.

func TestPrefilterDumpEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "unset-empty", value: "", want: false},
		{name: "zero", value: "0", want: false},
		{name: "false", value: "false", want: false},
		{name: "false-upper", value: "FALSE", want: false},
		{name: "no", value: "no", want: false},
		{name: "garbage", value: "yes-please", want: false},
		{name: "whitespace", value: "   ", want: false},
		{name: "one", value: "1", want: true},
		{name: "true", value: "true", want: true},
		{name: "true-upper", value: "TRUE", want: true},
		{name: "true-padded", value: "  true  ", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(prefilterEnv, test.value)

			if got := prefilterDumpEnabled(); got != test.want {
				t.Fatalf(
					"prefilterDumpEnabled() with %s=%q = %v, want %v",
					prefilterEnv,
					test.value,
					got,
					test.want,
				)
			}
		})
	}
}

// TestPrefilterDumpDefaultOff pins that the diagnostic is off by default and
// that a zero-value evaluator does not enter the dump path, so the default
// evaluation output is unchanged.
func TestPrefilterDumpDefaultOff(t *testing.T) {
	t.Setenv(prefilterEnv, "")

	if prefilterDumpEnabled() {
		t.Fatal("diagnostic enabled with the env switch unset; default behavior would change")
	}

	var eval evaluator

	if eval.prefilterDump {
		t.Fatal("zero-value evaluator unexpectedly enables the dump")
	}
}

// TestReportPrefilterOutput pins the exact, deterministic, bounded output shape
// of the diagnostic: header counts, expected-section presence and pre-filter
// rank, and the dropped-by-floor list (candidates strictly below the floor).
func TestReportPrefilterOutput(t *testing.T) {
	documents := []retrieval.Document{
		{Source: "examples/loan-policy.md", Section: "Payments", ChunkIndex: 0, Similarity: 0.80},
		{Source: "examples/other.md", Section: "X", ChunkIndex: 3, Similarity: 0.65},
		{Source: "examples/want.md", Section: "Target", ChunkIndex: 2, Similarity: 0.55},
	}

	eval := evaluator{minSimilarity: 0.70}
	evalCase := EvalCase{
		Expected: []ExpectedDocument{{Source: "examples/want.md", Section: "Target"}},
	}

	got := captureStdout(t, func() {
		eval.reportPrefilter(1, evalCase, documents)
	})

	want := "" +
		"Pre-filter K=1  candidates=3  sim>=0.70=1  sim<0.70=2\n" +
		"  expected present pre-filter: YES  examples/want.md / Target (rank 3)\n" +
		"  dropped by floor: 0.6500  examples/other.md / X / chunk 3\n" +
		"  dropped by floor: 0.5500  examples/want.md / Target / chunk 2\n"

	if got != want {
		t.Fatalf("reportPrefilter output mismatch:\n got=%q\nwant=%q", got, want)
	}

	// The diagnostic must not leak credentials or connection details.
	for _, forbidden := range []string{"postgres://", "password", "sslmode", "DATABASE_URL"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("diagnostic output contains sensitive token %q: %q", forbidden, got)
		}
	}
}

// TestReportPrefilterAbsentAndUnanswerable covers the two other branches: an
// expected section absent from the pre-filter candidates, and an unanswerable
// case. Both stay bounded (one block per call).
func TestReportPrefilterAbsentAndUnanswerable(t *testing.T) {
	eval := evaluator{minSimilarity: 0.70}
	documents := []retrieval.Document{
		{Source: "examples/a.md", Section: "A", ChunkIndex: 0, Similarity: 0.90},
	}

	absent := captureStdout(t, func() {
		eval.reportPrefilter(4, EvalCase{
			Expected: []ExpectedDocument{{Source: "examples/b.md", Section: "B"}},
		}, documents)
	})

	if !strings.Contains(absent, "expected present pre-filter: NO  examples/b.md / B") {
		t.Fatalf("absent expected section not reported: %q", absent)
	}

	if lines := strings.Count(absent, "\n"); lines != 2 {
		t.Fatalf("absent case emitted %d lines, want 2 (header + NO): %q", lines, absent)
	}

	unanswerable := captureStdout(t, func() {
		eval.reportPrefilter(4, EvalCase{}, documents)
	})

	if !strings.Contains(unanswerable, "expected: (unanswerable)") {
		t.Fatalf("unanswerable marker missing: %q", unanswerable)
	}

	if lines := strings.Count(unanswerable, "\n"); lines != 2 {
		t.Fatalf("unanswerable case emitted %d lines, want 2: %q", lines, unanswerable)
	}
}

// captureStdout runs f with os.Stdout redirected to a pipe and returns what f
// printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()

	original := os.Stdout

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}

	os.Stdout = writer

	f()

	if err := writer.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}

	os.Stdout = original

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}

	if err := reader.Close(); err != nil {
		t.Fatalf("close pipe reader: %v", err)
	}

	return string(data)
}
