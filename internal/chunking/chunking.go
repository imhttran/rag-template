package chunking

import (
	"strings"

	"rag-template/internal/document"
)

type Chunk struct {
	Section string
	Content string
	Index   int
}

// FromSections splits each section into chunks of at most chunkSize words,
// overlapping consecutive chunks by overlap words.
func FromSections(
	sections []document.Section,
	chunkSize int,
	overlap int,
) []Chunk {
	var chunks []Chunk

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

		for index, part := range parts {
			chunks = append(chunks, Chunk{
				Section: section.Title,
				Content: part,
				Index:   index,
			})
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
