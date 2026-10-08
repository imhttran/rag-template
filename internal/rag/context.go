package rag

import (
	"fmt"
	"strings"

	"rag-template/internal/retrieval"
)

// FormatContext renders documents as the context block sent to the model.
//
// A "Page: N" line is emitted only when a document carries a page (Page > 0),
// so page-less formats (Markdown, plain text) render byte-identically to the
// pre-page behavior.
func FormatContext(documents []retrieval.Document) string {
	parts := make([]string, len(documents))

	for i, doc := range documents {
		source := doc.Source
		if source == "" {
			source = "unknown"
		}

		var builder strings.Builder

		fmt.Fprintf(&builder, "Source: %s\n", source)
		fmt.Fprintf(&builder, "Section: %s\n", doc.Section)

		if doc.Page > 0 {
			fmt.Fprintf(&builder, "Page: %d\n", doc.Page)
		}

		fmt.Fprintf(&builder, "Chunk: %d\n", doc.ChunkIndex)
		fmt.Fprintf(&builder, "Content: %s", doc.Content)

		parts[i] = builder.String()
	}

	return strings.Join(parts, "\n\n---\n\n")
}
