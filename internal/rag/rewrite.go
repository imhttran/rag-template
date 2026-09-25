package rag

import (
	"context"
	"fmt"
	"strings"

	"rag-template/internal/generation"
)

const rewritePrompt = `Rewrite the user's question into a concise search query for retrieving relevant documents.

USER QUESTION:
%s

INSTRUCTIONS:
- Preserve the exact intent of the user's question.
- Produce a search query, not an answer.
- Do not answer the question.
- Do not infer what the answer might be.
- Do not add possible outcomes, actions, causes, policies, or consequences that the user did not mention.
- Do not introduce facts, names, numbers, dates, identifiers, or assumptions that are not present in the question.
- You may replace informal wording with equivalent domain terminology.
- Prefer concise terminology useful for document retrieval.
- Return only the rewritten query.
- Do not include explanations, labels, quotes, or formatting.

EXAMPLES:

User question:
What happens if I pay twice?

Good:
duplicate payment handling

Bad:
duplicate payment refund policy

User question:
What happens if I'm late?

Good:
late payment handling

Bad:
late payment fees and penalties

SEARCH QUERY:`

func Rewrite(
	ctx context.Context,
	generator *generation.Generator,
	question string,
) (string, error) {
	rewritten, err := generator.Generate(
		ctx,
		fmt.Sprintf(rewritePrompt, question),
	)
	if err != nil {
		return "", err
	}

	rewritten = strings.TrimSpace(rewritten)

	if rewritten == "" {
		return question, nil
	}

	return rewritten, nil
}
