package reranking

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"rag-template/internal/generation"
	"rag-template/internal/retrieval"
)

const llmRerankPrompt = `You are a document relevance reranker.

QUESTION:
%s

CANDIDATES:
%s

INSTRUCTIONS:
- Rank the candidates by how useful they are for answering the QUESTION.
- Judge semantic relevance, not just exact word overlap.
- Prefer candidates that directly contain evidence needed to answer the question.
- Do not answer the question.
- Do not add explanations.
- Return every candidate exactly once.
- Return ONLY a JSON array of candidate IDs from most relevant to least relevant.

Example:
[2,0,1]

RANKING:`

// RerankLLM ranks retrieved documents using a language model.
func RerankLLM(
	ctx context.Context,
	generator *generation.Generator,
	question string,
	documents []retrieval.Document,
) ([]retrieval.Document, error) {
	if len(documents) == 0 {
		return nil, nil
	}

	candidates := retrieval.FormatDocuments(documents, "ID: ")

	prompt := fmt.Sprintf(
		llmRerankPrompt,
		question,
		candidates,
	)

	response, err := generator.Generate(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("generate reranking: %w", err)
	}

	order, err := parseRanking(response, len(documents))
	if err != nil {
		return nil, fmt.Errorf(
			"parse reranking %q: %w",
			response,
			err,
		)
	}

	result := make(
		[]retrieval.Document,
		0,
		len(documents),
	)

	for _, index := range order {
		result = append(result, documents[index])
	}

	return result, nil
}

func parseRanking(
	response string,
	documentCount int,
) ([]int, error) {
	response = strings.TrimSpace(response)

	// Some models may wrap JSON in a Markdown code block.
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	var order []int

	if err := json.Unmarshal(
		[]byte(response),
		&order,
	); err != nil {
		return nil, err
	}

	if len(order) != documentCount {
		return nil, fmt.Errorf(
			"expected %d candidate IDs, got %d",
			documentCount,
			len(order),
		)
	}

	seen := make(map[int]bool)

	for _, index := range order {
		if index < 0 || index >= documentCount {
			return nil, fmt.Errorf(
				"candidate ID %d is out of range",
				index,
			)
		}

		if seen[index] {
			return nil, fmt.Errorf(
				"candidate ID %d appears more than once",
				index,
			)
		}

		seen[index] = true
	}

	return order, nil
}
