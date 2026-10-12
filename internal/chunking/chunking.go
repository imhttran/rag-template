package chunking

import (
	"strings"

	"rag-template/internal/document"
)

// Chunk is a chunk of a section. Page is copied from the source section, so it
// carries the same 1-based page provenance; it is 0 when the section had no
// page information. Index is the chunk's position across the whole document
// (all sections concatenated in source order), so it is strictly increasing and
// never restarts at 0 for a repeated heading.
type Chunk struct {
	Section string
	Content string
	Index   int
	Page    int
}

// FromSections splits each section into chunks of at most chunkSize words,
// overlapping consecutive chunks by overlap words. Each chunk records the
// source section's Page and a document-global Index assigned in section order.
func FromSections(
	sections []document.Section,
	chunkSize int,
	overlap int,
) []Chunk {
	var chunks []Chunk

	// documentIndex is a single monotonic counter shared across every section,
	// so a repeated heading's chunks continue the numbering instead of
	// restarting at 0.
	documentIndex := 0

	for _, section := range sections {
		content := strings.TrimSpace(section.Content)

		if content == "" {
			continue
		}

		parts := splitWords(
			content,
			chunkSize,
			overlap,
		)

		for _, part := range parts {
			chunks = append(chunks, Chunk{
				Section: section.Title,
				Content: part,
				Index:   documentIndex,
				Page:    section.Page,
			})

			documentIndex++
		}
	}

	return chunks
}

func splitWords(
	content string,
	chunkSize int,
	overlap int,
) []string {
	if chunkSize <= 0 || overlap < 0 || overlap >= chunkSize {
		return nil
	}

	words := strings.Fields(content)

	if len(words) == 0 {
		return nil
	}

	if len(words) <= chunkSize {
		return []string{strings.Join(words, " ")}
	}

	step := chunkSize - overlap

	var chunks []string

	for start := 0; start < len(words); start += step {
		end := start + chunkSize

		if end > len(words) {
			end = len(words)
		}

		chunks = append(
			chunks,
			strings.Join(words[start:end], " "),
		)

		if end == len(words) {
			break
		}
	}

	return chunks
}
