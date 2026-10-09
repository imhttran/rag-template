package reranking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

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

// FallbackReason classifies why the reranker degraded to the fused order. It is
// reported alongside the result so callers can distinguish a successful model
// ordering from a fallback without comparing document contents (a model may
// legitimately return the documents in the same order as the input).
const (
	// FallbackNone means the model returned a usable ordering.
	FallbackNone FallbackReason = ""
	// FallbackDisabled means the reranker had nothing to rank.
	FallbackDisabled FallbackReason = "disabled"
	// FallbackParse means the response was malformed or failed validation.
	FallbackParse FallbackReason = "parse_failure"
	// FallbackGeneratorError means the generator call failed.
	FallbackGeneratorError FallbackReason = "generator_error"
	// FallbackTimeout means the latency guard expired before the model answered.
	FallbackTimeout FallbackReason = "timeout"
)

// FallbackReason is why the reranker fell back to the fused order.
type FallbackReason string

// Result is the outcome of an LLM rerank. Documents always holds the fused
// order when FellBack is true, so callers can answer without inspecting the
// reranker's internals. A returned error is never surfaced for a fallback;
// callers should treat any fallback as "use Documents as-is".
type Result struct {
	Documents []retrieval.Document
	FellBack  bool
	Reason    FallbackReason
}

// RerankLLM ranks retrieved documents using a language model.
//
// Hardening contract:
//   - A disabled reranker (no documents) returns the input unchanged with a nil
//     error and reports FallbackDisabled; an empty document set is not a
//     reranker failure.
//   - A parse/validation failure (not-JSON, wrong candidate count, out-of-range
//     ID, duplicate ID) or a generator error degrades to the fused order (the
//     input documents) and logs the fallback; no error is returned.
//   - A latency guard bounds the wait. When it expires (guarded by timeout) the
//     fused order is returned and the expiry is logged distinctly.
//   - A successful ranking is returned in the model's order.
//
// The returned error is always nil for the fallback paths; it is reserved so
// callers keep a single call shape.
func RerankLLM(
	ctx context.Context,
	generator generation.Generator,
	question string,
	documents []retrieval.Document,
) ([]retrieval.Document, error) {
	result, err := RerankLLMWithTimeout(ctx, generator, question, documents, 0)

	return result.Documents, err
}

// RerankLLMWithTimeout is RerankLLM with an explicit latency bound. A timeout
// of zero or less disables the guard, preserving the baseline behavior. It
// returns a Result whose FellBack/Reason fields let callers report used vs
// fell back without comparing document contents.
func RerankLLMWithTimeout(
	ctx context.Context,
	generator generation.Generator,
	question string,
	documents []retrieval.Document,
	timeout time.Duration,
) (Result, error) {
	if len(documents) == 0 {
		return Result{
			Documents: nil,
			FellBack:  true,
			Reason:    FallbackDisabled,
		}, nil
	}

	candidates := retrieval.FormatDocuments(documents, "ID: ")

	prompt := fmt.Sprintf(
		llmRerankPrompt,
		question,
		candidates,
	)

	// The latency guard derives a child context from the caller's context and
	// cancels only that child when it expires; the caller's context is never
	// cancelled. The guard therefore stays local to the reranker call.
	generateCtx := ctx
	guarded := timeout > 0

	if guarded {
		var cancel context.CancelFunc

		generateCtx, cancel = context.WithTimeout(ctx, timeout)

		defer cancel()
	}

	response, err := generator.Generate(generateCtx, prompt)
	if err != nil {
		return fallback(
			documents,
			classifyGeneratorError(err, guarded, generateCtx, ctx),
			err,
		), nil
	}

	order, err := parseRanking(response, len(documents))
	if err != nil {
		return fallback(documents, FallbackParse, err), nil
	}

	result := make(
		[]retrieval.Document,
		0,
		len(documents),
	)

	for _, index := range order {
		result = append(result, documents[index])
	}

	return Result{Documents: result}, nil
}

// fallback builds the fused-order Result and logs the fallback reason so
// operators can tell a slow reranker from a parse failure from a generator
// error.
func fallback(
	documents []retrieval.Document,
	reason FallbackReason,
	cause error,
) Result {
	log.Printf(
		"reranking: %s; falling back to fused order: %v",
		reason,
		cause,
	)

	return Result{
		Documents: documents,
		FellBack:  true,
		Reason:    reason,
	}
}

// classifyGeneratorError distinguishes a latency-guard expiry from any other
// generator failure. Only a deadline the guard itself imposed counts as
// FallbackTimeout; a deadline or cancellation inherited from the caller's
// context (for example the request timeout) is reported as a generator error,
// so the caller's own cancellation semantics are not misattributed to the
// guard.
func classifyGeneratorError(
	err error,
	guarded bool,
	guardCtx context.Context,
	parentCtx context.Context,
) FallbackReason {
	if guarded &&
		errors.Is(err, context.DeadlineExceeded) &&
		guardCtx.Err() == context.DeadlineExceeded &&
		parentCtx.Err() == nil {
		return FallbackTimeout
	}

	return FallbackGeneratorError
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
