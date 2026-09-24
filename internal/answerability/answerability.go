// Package answerability asks the chat model whether retrieved evidence can
// answer a question, or which expected facts it supports.
package answerability

import (
	"context"
	"fmt"
	"strings"

	"rag-template/internal/generation"
	"rag-template/internal/retrieval"
)

const prompt = `Determine whether the retrieved evidence contains enough information to answer the question.

QUESTION:
%s

RETRIEVED EVIDENCE:
%s

INSTRUCTIONS:
- Do not answer the question.
- Decide only whether the evidence contains enough information to answer it.
- Related information is not enough.
- The evidence must provide a direct, grounded response to the question.
- A response may be sufficient even if it refers to another agreement, policy, source, or condition.
- Do not require a specific numeric value, date, amount, or identifier unless the question specifically requires that value to be present in the retrieved evidence.
- Respond with exactly one of:
ANSWERABLE
NOT_ANSWERABLE

DECISION:`

const answerFactsPrompt = `Determine which expected facts are stated or clearly supported by the generated answer.

QUESTION:
%s

GENERATED ANSWER:
%s

EXPECTED FACTS:
%s

INSTRUCTIONS:
- Evaluate only the generated answer.
- Do not use outside knowledge.
- A fact is supported when the answer states it directly or clearly expresses the same meaning.
- Do not require exact wording.
- Return only the numbers of the supported facts, separated by commas.
- If none are supported, return NONE.

SUPPORTED FACTS:`

const groundednessPrompt = `Determine whether the generated answer is fully supported by the retrieved evidence.

QUESTION:
%s

RETRIEVED EVIDENCE:
%s

GENERATED ANSWER:
%s

INSTRUCTIONS:
- Evaluate only whether claims in the generated answer are supported by the retrieved evidence.
- Do not use outside knowledge.
- Every factual claim in the answer must be supported by the evidence.
- Paraphrasing is allowed; exact wording is not required.
- If the answer contains a factual claim that is not supported by the evidence, return NOT_GROUNDED.
- If all factual claims are supported by the evidence, return GROUNDED.
- Respond with exactly one of:
GROUNDED
NOT_GROUNDED

DECISION:`

const citationEntailmentPrompt = `Determine whether the cited evidence supports the claim.

CLAIM:
%s

CITED EVIDENCE:
%s

INSTRUCTIONS:
- Evaluate only whether the cited evidence supports the claim.
- Do not use outside knowledge.
- The evidence does not need to use exactly the same wording.
- Return ENTAILED if the evidence directly supports the claim.
- Return NOT_ENTAILED if it does not support the claim or contradicts it.
- Respond with exactly one of:
ENTAILED
NOT_ENTAILED

DECISION:`

// Judge decides whether retrieved evidence can answer a question.
type Judge struct {
	generator *generation.Generator
}

// New returns a Judge that decides with generator.
func New(generator *generation.Generator) *Judge {
	return &Judge{
		generator: generator,
	}
}

// IsAnswerable reports whether documents contain enough information to answer
// question.
func (j *Judge) IsAnswerable(
	ctx context.Context,
	question string,
	documents []retrieval.Document,
) (bool, error) {
	if len(documents) == 0 {
		return false, nil
	}

	evidence := retrieval.FormatDocuments(documents, "")

	response, err := j.generator.Generate(
		ctx,
		fmt.Sprintf(prompt, question, evidence),
	)
	if err != nil {
		return false, err
	}

	return parseDecision(response)
}

// SupportedAnswerFacts returns how many of expectedFacts the generated answer
// supports, judged by the chat model.
func (j *Judge) SupportedAnswerFacts(
	ctx context.Context,
	question string,
	answer string,
	expectedFacts []string,
) (int, error) {
	if len(expectedFacts) == 0 {
		return 0, nil
	}

	var facts strings.Builder

	for i, fact := range expectedFacts {
		fmt.Fprintf(&facts, "%d. %s\n", i+1, fact)
	}

	response, err := j.generator.Generate(
		ctx,
		fmt.Sprintf(
			answerFactsPrompt,
			question,
			answer,
			facts.String(),
		),
	)
	if err != nil {
		return 0, err
	}

	return parseSupportedFacts(response, len(expectedFacts))
}

// IsGrounded reports whether all factual claims in the generated answer are
// supported by the retrieved documents.
func (j *Judge) IsGrounded(
	ctx context.Context,
	question string,
	answer string,
	documents []retrieval.Document,
) (bool, error) {
	if len(documents) == 0 {
		return false, nil
	}

	evidence := retrieval.FormatDocuments(documents, "")

	response, err := j.generator.Generate(
		ctx,
		fmt.Sprintf(
			groundednessPrompt,
			question,
			evidence,
			answer,
		),
	)
	if err != nil {
		return false, err
	}

	return parseGroundedness(response)
}

// parseDecision reads the model's ANSWERABLE / NOT_ANSWERABLE decision.
func parseDecision(response string) (bool, error) {
	return parseTwoWayDecision(response, "NOT_ANSWERABLE", "ANSWERABLE")
}

// parseTwoWayDecision reads a decision whose negative token is the positive one
// with a "NOT_" prefix. The negative token is checked first, so "NOT_GROUNDED"
// cannot read as "GROUNDED". Case and trailing punctuation or prose are
// tolerated -- "answerable." and "GROUNDED because the policy states it" both
// parse -- while arbitrary prose is rejected as an error.
func parseTwoWayDecision(response, negative, positive string) (bool, error) {
	decision := strings.ToUpper(strings.TrimSpace(response))

	switch {
	case strings.HasPrefix(decision, negative):
		return false, nil

	case strings.HasPrefix(decision, positive):
		return true, nil

	default:
		return false, fmt.Errorf("unexpected decision: %q", response)
	}
}

// parseSupportedFacts counts the fact numbers the model reported, which are
// 1-based in the prompt, ignoring repeats.
func parseSupportedFacts(response string, factCount int) (int, error) {
	value := strings.TrimSpace(response)

	if strings.EqualFold(value, "NONE") {
		return 0, nil
	}

	seen := make(map[int]bool)

	count := 0

	for _, part := range strings.Split(value, ",") {
		var index int

		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &index); err != nil {
			return 0, fmt.Errorf(
				"unexpected supported facts response: %q",
				response,
			)
		}

		if index < 1 || index > factCount {
			return 0, fmt.Errorf(
				"supported fact index out of range: %d",
				index,
			)
		}

		if seen[index] {
			continue
		}

		seen[index] = true

		count++
	}

	return count, nil
}

// parseGroundedness reads the model's GROUNDED / NOT_GROUNDED decision.
func parseGroundedness(response string) (bool, error) {
	return parseTwoWayDecision(response, "NOT_GROUNDED", "GROUNDED")
}

// IsEntailed reports whether evidence supports a claim.
func (j *Judge) IsEntailed(
	ctx context.Context,
	claim string,
	evidence string,
) (bool, error) {
	response, err := j.generator.Generate(
		ctx,
		fmt.Sprintf(
			citationEntailmentPrompt,
			claim,
			evidence,
		),
	)
	if err != nil {
		return false, err
	}

	return parseEntailment(response)
}

func parseEntailment(response string) (bool, error) {
	decision := strings.ToUpper(strings.TrimSpace(response))

	switch {
	case strings.HasPrefix(decision, "NOT_ENTAILED"):
		return false, nil

	case strings.HasPrefix(decision, "ENTAILED"):
		return true, nil

	default:
		return false, fmt.Errorf(
			"unexpected entailment response: %q",
			response,
		)
	}
}
