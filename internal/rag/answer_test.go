package rag

import (
	"strings"
	"testing"

	"rag-template/internal/retrieval"
)

func TestFormatContext(t *testing.T) {
	documents := []retrieval.Document{
		{Source: "a.md", Section: "Fees", ChunkIndex: 0, Content: "first"},
		{Source: "b.md", Section: "Refunds", ChunkIndex: 1, Content: "second"},
	}

	want := "Source: a.md\nSection: Fees\nChunk: 0\nContent: first" +
		"\n\n---\n\n" +
		"Source: b.md\nSection: Refunds\nChunk: 1\nContent: second"

	if got := FormatContext(documents); got != want {
		t.Fatalf("FormatContext() = %q, want %q", got, want)
	}
}

func TestFormatContextUnknownSource(t *testing.T) {
	documents := []retrieval.Document{
		{Source: "", Section: "Fees", Content: "body"},
	}

	got := FormatContext(documents)

	if !strings.Contains(got, "Source: unknown") {
		t.Fatalf("expected an unknown source label, got %q", got)
	}
}

func TestFormatContextEmpty(t *testing.T) {
	if got := FormatContext(nil); got != "" {
		t.Fatalf("FormatContext(nil) = %q, want empty", got)
	}
}
