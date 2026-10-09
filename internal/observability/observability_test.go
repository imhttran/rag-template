package observability

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestHumanReporterWritesNothing asserts the default reporter adds no lines to
// the human-readable stream: its stage/count/usage methods buffer nothing to
// stdout, so the default workflow output is unchanged.
func TestHumanReporterWritesNothing(t *testing.T) {
	var buf bytes.Buffer

	reporter := New(false, &buf)

	reporter.Stage(StageEmbed, 10*time.Millisecond)
	reporter.Counts(Counts{Retrieved: 3, Filtered: 1})
	reporter.Usage(Usage{ContextBytes: 128, Chunks: 3})
	reporter.Finish()

	if buf.Len() != 0 {
		t.Fatalf("default reporter wrote %q, want no output", buf.String())
	}

	if reporter.RunID() == "" {
		t.Fatal("HumanReporter.RunID is empty")
	}
}

// TestNewSelectsReporter asserts the configuration switch: JSON only when
// requested, human otherwise (the default).
func TestNewSelectsReporter(t *testing.T) {
	if _, ok := New(false, &bytes.Buffer{}).(*HumanReporter); !ok {
		t.Fatal("New(false) must return the default HumanReporter")
	}

	if _, ok := New(true, &bytes.Buffer{}).(*JSONReporter); !ok {
		t.Fatal("New(true) must return the JSONReporter")
	}
}

// TestJSONReporterSchemaAndValues asserts the emitted record is a single JSON
// line carrying the documented schema and the recorded values.
func TestJSONReporterSchemaAndValues(t *testing.T) {
	var buf bytes.Buffer

	reporter := NewJSONReporter(&buf)

	reporter.Stage(StageEmbed, 1500*time.Millisecond)
	reporter.Stage(StageAnswer, 250*time.Millisecond)
	reporter.Counts(Counts{Retrieved: 7, Filtered: 2})
	reporter.Usage(Usage{ContextBytes: 4096, Chunks: 7})
	reporter.Finish()

	record := decodeOne(t, buf.String())

	if got := record["run_id"]; got != reporter.RunID() {
		t.Fatalf("run_id = %v, want %q", got, reporter.RunID())
	}

	wantNumbers := map[string]float64{
		"retrieved":       7,
		"filtered":        2,
		"context_bytes":   4096,
		"chunks":          7,
		"stage_ms_embed":  1500,
		"stage_ms_answer": 250,
	}

	for key, want := range wantNumbers {
		got, ok := record[key].(float64)
		if !ok {
			t.Fatalf("record[%q] = %v (%T), want a number", key, record[key], record[key])
		}

		if got != want {
			t.Fatalf("record[%q] = %v, want %v", key, got, want)
		}
	}
}

// TestJSONReporterStageDurationsAreMilliseconds pins the unit: a 2-second stage
// is reported as 2000 (milliseconds), not 2 (seconds). This is the regression
// guard for the stage_ms unit bug.
func TestJSONReporterStageDurationsAreMilliseconds(t *testing.T) {
	var buf bytes.Buffer

	reporter := NewJSONReporter(&buf)

	reporter.Stage(StageRetrieve, 2*time.Second)
	reporter.Finish()

	record := decodeOne(t, buf.String())

	got, ok := record["stage_ms_retrieve"].(float64)
	if !ok {
		t.Fatalf("stage_ms_retrieve = %v (%T), want a number", record["stage_ms_retrieve"], record["stage_ms_retrieve"])
	}

	if got != 2000 {
		t.Fatalf("stage_ms_retrieve = %v, want 2000 (milliseconds)", got)
	}
}

// TestJSONReporterSubMillisecond asserts fractional milliseconds are preserved,
// so short stages are not rounded away to zero.
func TestJSONReporterSubMillisecond(t *testing.T) {
	var buf bytes.Buffer

	reporter := NewJSONReporter(&buf)

	reporter.Stage(StageBudget, 500*time.Microsecond)
	reporter.Finish()

	record := decodeOne(t, buf.String())

	got, ok := record["stage_ms_budget"].(float64)
	if !ok {
		t.Fatalf("stage_ms_budget = %v (%T), want a number", record["stage_ms_budget"], record["stage_ms_budget"])
	}

	if got != 0.5 {
		t.Fatalf("stage_ms_budget = %v, want 0.5", got)
	}
}

// TestJSONReporterEmptyStagesAsserts a run with no recorded stages still emits
// one correlated record (every run emits a record).
func TestJSONReporterEmptyStages(t *testing.T) {
	var buf bytes.Buffer

	reporter := NewJSONReporter(&buf)
	reporter.Finish()

	record := decodeOne(t, buf.String())

	if record["run_id"] != reporter.RunID() {
		t.Fatalf("run_id = %v, want %q", record["run_id"], reporter.RunID())
	}

	if got := record["retrieved"]; got != float64(0) {
		t.Fatalf("retrieved = %v, want 0", got)
	}
}

// TestRunIDUniquenessAndShape asserts run IDs are unique and carry the run-
// prefix, so records from different invocations do not collide.
func TestRunIDUniquenessAndShape(t *testing.T) {
	seen := make(map[string]bool, 256)

	for i := 0; i < 256; i++ {
		id := NewRunID()

		if !strings.HasPrefix(id, "run-") {
			t.Fatalf("run ID %q does not start with run-", id)
		}

		if seen[id] {
			t.Fatalf("duplicate run ID %q", id)
		}

		seen[id] = true
	}
}

// decodeOne parses the single JSON line the JSONReporter emits.
func decodeOne(t *testing.T, output string) map[string]any {
	t.Helper()

	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		t.Fatal("no JSON record emitted")
	}

	if strings.Contains(trimmed, "\n") {
		t.Fatalf("expected a single JSON line, got:\n%s", trimmed)
	}

	var record map[string]any

	if err := json.Unmarshal([]byte(trimmed), &record); err != nil {
		t.Fatalf("unmarshal record: %v\n%s", err, trimmed)
	}

	return record
}
