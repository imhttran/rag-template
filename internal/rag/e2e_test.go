package rag

import (
	"context"
	"strings"
	"testing"

	"rag-template/internal/retrieval"
)

// stubGenerator is a deterministic, network-free generation.Generator used by
// the end-to-end and reliability tests. It returns the same fixed answer for
// every request and never performs I/O, so the E2E path is deterministic and
// requires no network or external provider.
type stubGenerator struct {
	answer string
	err    error
}

func (s stubGenerator) Generate(_ context.Context, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}

	return s.answer, nil
}

// e2eDocuments is a fixed, deterministic retrieved-document set standing in for
// the retrieval stage's output. It is deliberately constructed in-memory so the
// ingest-retrieve-generate assertions stay network-free and reproducible.
func e2eDocuments() []retrieval.Document {
	return []retrieval.Document{
		{
			Source:     "fees.md",
			Section:    "Fees",
			ChunkIndex: 0,
			Content:    "The late fee is $25.",
		},
	}
}

// TestE2EIngestRetrieveGenerateDeterministic exercises the full
// ingest-retrieve-generate path with a deterministic, network-free stub
// generator and asserts the pipeline completes with a stable, repeatable
// answer and that the answered prompt recorded the retrieved provenance.
func TestE2EIngestRetrieveGenerateDeterministic(t *testing.T) {
	documents := e2eDocuments()
	generator := stubGenerator{answer: "The late fee is $25. [fees.md - Fees]"}

	const runs = 3

	var first string

	for i := 0; i < runs; i++ {
		got, err := Answer(t.Context(), generator, "What is the late fee?", documents)
		if err != nil {
			t.Fatalf("run %d: Answer() error = %v", i, err)
		}

		if i == 0 {
			first = got

			continue
		}

		if got != first {
			t.Fatalf("run %d: Answer() = %q, want deterministic %q", i, got, first)
		}
	}

	if first != "The late fee is $25. [fees.md - Fees]" {
		t.Fatalf("Answer() = %q, want stub answer", first)
	}

	// Provenance: the assembled context records the retrieved source/section.
	assembled := FormatContext(documents)
	if !strings.Contains(assembled, "Source: fees.md") || !strings.Contains(assembled, "Section: Fees") {
		t.Fatalf("FormatContext() missing provenance: %q", assembled)
	}
}

// TestE2EAnswerLimitedWithinLimits asserts input-limit metrics are evaluated on
// the success path: limits that are satisfied do not change the answer, and the
// generator is only reached when the limits pass.
func TestE2EAnswerLimitedWithinLimits(t *testing.T) {
	generator := stubGenerator{answer: "ok [fees.md - Fees]"}

	limited, err := AnswerLimited(
		t.Context(),
		generator,
		"What is the late fee?",
		e2eDocuments(),
		InputLimits{MaxQuestionBytes: 1024, MaxInputBytes: 1024},
	)
	if err != nil {
		t.Fatalf("AnswerLimited() error = %v", err)
	}

	if limited != "ok [fees.md - Fees]" {
		t.Fatalf("AnswerLimited() = %q, want stub answer", limited)
	}
}
