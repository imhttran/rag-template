package rag

import (
	"testing"

	"rag-template/internal/retrieval"
)

// TestFormatContextPageLine asserts a document with a page renders a "Page: N"
// line between Section and Chunk.
func TestFormatContextPageLine(t *testing.T) {
	documents := []retrieval.Document{
		{Source: "a.md", Section: "Fees", ChunkIndex: 0, Content: "x", Page: 3},
	}

	want := "Source: a.md\nSection: Fees\nPage: 3\nChunk: 0\nContent: x"

	if got := FormatContext(documents); got != want {
		t.Fatalf("FormatContext() = %q, want %q", got, want)
	}
}

// TestFormatContextNoPageUnchanged asserts a page-less document renders exactly
// as before RAG-017 (no Page line).
func TestFormatContextNoPageUnchanged(t *testing.T) {
	documents := []retrieval.Document{
		{Source: "a.md", Section: "Fees", ChunkIndex: 0, Content: "x"},
	}

	want := "Source: a.md\nSection: Fees\nChunk: 0\nContent: x"

	if got := FormatContext(documents); got != want {
		t.Fatalf("FormatContext() = %q, want %q", got, want)
	}
}
