package observability

import (
	"bytes"
	"testing"
	"time"
)

// REGRESSION(F3): JSON mode must preserve the answer. Historically
// JSONReporter.Finish emitted one structured slog record carrying only
// timing/count/usage fields (run_id, retrieved, filtered, context_bytes, chunks,
// stage_ms_<stage>); the generated answer was never part of the record, while
// cmd/rag discarded the human stream (out = io.Discard) so the answer vanished
// entirely. RAG-023C fixes this by recording the answer on the structured record.
//
// This baseline is updated (RAG-023C) from the A3 characterization test (which
// pinned the absence of an answer field) to assert the corrected behavior: the
// record carries the answer and the answered flag.
func TestJSONRecordCarriesAnswerField(t *testing.T) {
	var buf bytes.Buffer

	reporter := NewJSONReporter(&buf)

	// Populate counts, usage, and at least one stage so the timing/count fields
	// are exercised, mirroring a real run, and record an answer.
	reporter.Stage(StageEmbed, 10*time.Millisecond)
	reporter.Stage(StageRetrieve, 20*time.Millisecond)
	reporter.Counts(Counts{Retrieved: 3, Filtered: 1})
	reporter.Usage(Usage{ContextBytes: 128, Chunks: 3})
	reporter.Answer("the answer text", true)
	reporter.Finish()

	record := decodeOne(t, buf.String())

	// The schema must still carry the run ID, the numeric counts, and the usage
	// estimate. These are the timing/count fields the record is defined to
	// carry.
	requiredFields := []string{
		"run_id",
		"retrieved",
		"filtered",
		"context_bytes",
		"chunks",
		"stage_ms_embed",
		"stage_ms_retrieve",
	}

	for _, field := range requiredFields {
		if _, ok := record[field]; !ok {
			t.Fatalf("record is missing required field %q; got keys %v", field, keys(record))
		}
	}

	// JSON mode must now show the answer: the record carries the answer text so
	// the machine-readable output preserves what the human stream would show.
	answer, ok := record["answer"]
	if !ok {
		t.Fatalf("record is missing required answer field; got keys %v", keys(record))
	}

	if got, _ := answer.(string); got != "the answer text" {
		t.Fatalf("record answer = %q, want %q", answer, "the answer text")
	}

	answered, ok := record["answered"]
	if !ok {
		t.Fatalf("record is missing required answered field; got keys %v", keys(record))
	}

	if got, _ := answered.(bool); !got {
		t.Fatalf("record answered = %v, want true", answered)
	}
}

// TestJSONRecordReportsInsufficientInformation asserts the insufficient-
// information outcome is reported in JSON mode: an answered=false record still
// carries the answer text.
func TestJSONRecordReportsInsufficientInformation(t *testing.T) {
	var buf bytes.Buffer

	reporter := NewJSONReporter(&buf)
	reporter.Answer("I do not have enough information.", false)
	reporter.Finish()

	record := decodeOne(t, buf.String())

	answered, ok := record["answered"]
	if !ok {
		t.Fatalf("record is missing required answered field; got keys %v", keys(record))
	}

	if got, _ := answered.(bool); got {
		t.Fatalf("record answered = %v, want false for the insufficient-information outcome", answered)
	}

	answer, _ := record["answer"].(string)
	if answer != "I do not have enough information." {
		t.Fatalf("record answer = %q, want the insufficient-information text", answer)
	}
}

// keys returns the record's keys for failure messages.
func keys(record map[string]any) []string {
	out := make([]string, 0, len(record))

	for key := range record {
		out = append(out, key)
	}

	return out
}
