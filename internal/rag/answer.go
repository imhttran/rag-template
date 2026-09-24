// Package rag turns retrieved documents into an answer prompt and sends it to
// the chat model.
package rag

import (
	"context"
	"fmt"
	"strings"

	"rag-template/internal/generation"
	"rag-template/internal/retrieval"
)

const answerPrompt = `You are answering a question using retrieved documents.

RETRIEVED CONTEXT:
%s

USER QUESTION:
%s

INSTRUCTIONS:
- Answer the USER QUESTION using only the RETRIEVED CONTEXT.
- Do not use outside knowledge.
- If the retrieved context does not contain enough information, say "I do not have enough information."
- Place citations immediately after the factual claim they support.
- Use exactly this citation format: [source - section]
- Copy the source and section exactly as they appear in the RETRIEVED CONTEXT ("Source:" and "Section:"), including the file extension.
- Use one citation block per source. Do not combine multiple sources inside one pair of brackets.
- Do not include chunk numbers in citations.
- Do not place a citation before the claim it supports.
- Never invent a source or citation.
- If retrieved sources disagree, explicitly state that they disagree.
- Do not guess why the sources disagree.
- Do not choose one source over another unless the retrieved context establishes which source is authoritative.
- State the conflicting information and cite each source.

ANSWER:`

// Answer asks the model to answer question using documents as context.
func Answer(
	ctx context.Context,
	generator *generation.Generator,
	question string,
	documents []retrieval.Document,
) (string, error) {
	prompt := fmt.Sprintf(
		answerPrompt,
		FormatContext(documents),
		question,
	)

	return generator.Generate(ctx, prompt)
}

// FormatContext renders documents as the context block sent to the model.
func FormatContext(documents []retrieval.Document) string {
	parts := make([]string, len(documents))

	for i, doc := range documents {
		source := doc.Source
		if source == "" {
			source = "unknown"
		}

		parts[i] = fmt.Sprintf(
			"Source: %s\nSection: %s\nChunk: %d\nContent: %s",
			source,
			doc.Section,
			doc.ChunkIndex,
			doc.Content,
		)
	}

	return strings.Join(parts, "\n\n---\n\n")
}
