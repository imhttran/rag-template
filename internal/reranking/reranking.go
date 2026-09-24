package reranking

import (
	"sort"
	"strings"

	"rag-template/internal/retrieval"
)

type scoredDocument struct {
	document retrieval.Document
	score    int
}

func Rerank(
	question string,
	documents []retrieval.Document,
) []retrieval.Document {
	questionWords := words(question)

	scored := make([]scoredDocument, 0, len(documents))

	for _, document := range documents {
		documentWords := words(
			document.Section + " " + document.Content,
		)

		score := overlapScore(
			questionWords,
			documentWords,
		)

		scored = append(scored, scoredDocument{
			document: document,
			score:    score,
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	result := make([]retrieval.Document, 0, len(scored))

	for _, item := range scored {
		result = append(result, item.document)
	}

	return result
}

func words(text string) map[string]bool {
	result := make(map[string]bool)

	for _, word := range strings.Fields(strings.ToLower(text)) {
		word = strings.Trim(word, ".,!?;:()[]\"'")

		if word != "" {
			result[word] = true
		}
	}

	return result
}

func overlapScore(
	questionWords map[string]bool,
	documentWords map[string]bool,
) int {
	score := 0

	for word := range questionWords {
		if documentWords[word] {
			score++
		}
	}

	return score
}
