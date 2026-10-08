package rag

import (
	"context"
	"fmt"
	"strings"

	"rag-template/internal/citations"
	"rag-template/internal/generation"
	"rag-template/internal/retrieval"
)

// repairPrompt asks the model to redo an answer that contained invalid
// citations. It is a single, bounded re-prompt: the caller issues it at most
// once. The citation-format contract in answerPrompt is restated so the repair
// is held to the same rule.
const repairPrompt = `You previously answered a question but included citations that do not appear in the retrieved context.

RETRIEVED CONTEXT:
%s

USER QUESTION:
%s

YOUR PREVIOUS ANSWER:
%s

REPAIR INSTRUCTIONS:
- Rewrite the answer using only the RETRIEVED CONTEXT.
- Cite every factual claim with exactly this format: [source - section]
- Copy the source and section exactly as they appear in the RETRIEVED CONTEXT ("Source:" and "Section:").
- Remove every citation that does not name a source and section from the RETRIEVED CONTEXT.
- If the retrieved context does not contain enough information, say "I do not have enough information."

REPAIRED ANSWER:`

// Outcome reports what validation and repair did to an answer.
//
// Enabled is false when validation was turned off, in which case Answer was
// returned unchanged. Repaired reports whether the single allowed re-prompt was
// issued. Stripped counts the citations removed from the final answer (including
// any residual invented citations removed from the repaired answer).
// Valid and Total count the citations in the final, returned answer: Valid is
// the number of citations that resolve to a retrieved source/section and Total
// is the number of citations present, so Valid == Total means every rendered
// citation resolves.
type Outcome struct {
	Enabled  bool
	Repaired bool
	Stripped int
	Valid    int
	Total    int
}

// AnswerValidated answers question the same way Answer does, then, when validate
// is true, ensures every citation in the returned answer resolves to a source
// and section in documents.
//
// Repair is bounded: at most one additional Generate call is made. If the model
// ignores the repair instruction and still emits an invalid citation, that
// citation is stripped, so no invalid citation is ever returned as if valid. If
// the repair call itself fails or returns nothing usable, the original answer is
// stripped instead so the caller still gets a usable, citation-valid answer
// rather than an error that discards it. A repair error therefore never fails
// the request for that reason.
//
// When validate is false the raw model output is returned unchanged and no
// validation or repair runs, so the default path stays byte-identical to
// baseline.
func AnswerValidated(
	ctx context.Context,
	generator generation.Generator,
	question string,
	documents []retrieval.Document,
	validate bool,
) (string, Outcome, error) {
	answer, err := Answer(ctx, generator, question, documents)
	if err != nil {
		return "", Outcome{}, err
	}

	if !validate {
		return answer, Outcome{Enabled: false}, nil
	}

	if citationAnswerClean(answer, documents) {
		return finishOutcome(answer, documents, false, 0)
	}

	// One bounded re-prompt with repair instructions. A failing or empty repair
	// does not discard the answer: fall back to stripping the original answer's
	// invalid citations so none are rendered as valid.
	repaired, repairErr := generator.Generate(
		ctx,
		fmt.Sprintf(repairPrompt, FormatContext(documents), question, answer),
	)

	if repairErr != nil || strings.TrimSpace(repaired) == "" {
		final, stripped := citations.Strip(answer, documents)

		return finishOutcome(final, documents, true, stripped)
	}

	// Even after the repair attempt, strip whatever still does not resolve so no
	// invalid citation is rendered as valid.
	final, stripped := citations.Strip(repaired, documents)

	return finishOutcome(final, documents, true, stripped)
}

// citationAnswerClean reports whether answer needs no repair. An answer is clean
// only when it makes at least one citation and every citation it makes resolves
// to a retrieved document. An answer that cites nothing is therefore NOT clean:
// a missing citation is as unusable as an invented one, so the plan's scope
// covers missing as well as invalid citations and an uncited answer takes the
// same single bounded repair path. After the repair, whatever citations remain
// are stripped if they still do not resolve, so no invalid citation is ever
// rendered as valid.
func citationAnswerClean(answer string, documents []retrieval.Document) bool {
	valid, total := citations.Validity(answer, documents)

	return total > 0 && valid == total
}

// finishOutcome computes the Outcome for a final answer string. Valid and Total
// describe the citations actually present in finalAnswer, so the reported
// validity never overstates what was rendered.
func finishOutcome(
	finalAnswer string,
	documents []retrieval.Document,
	repaired bool,
	stripped int,
) (string, Outcome, error) {
	valid, total := citations.Validity(finalAnswer, documents)

	return finalAnswer, Outcome{
		Enabled:  true,
		Repaired: repaired,
		Stripped: stripped,
		Valid:    valid,
		Total:    total,
	}, nil
}
