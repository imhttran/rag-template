package rag

import (
	"context"
	"errors"
	"testing"

	"rag-template/internal/retrieval"
)

// queueGenerator returns queued answers in order and counts its calls, so the
// bounded-repair contract (at most one extra Generate) is assertable.
type queueGenerator struct {
	answers []string
	err     error
	calls   int
	prompts []string
}

func (f *queueGenerator) Generate(_ context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)

	index := f.calls
	f.calls++

	if f.err != nil {
		return "", f.err
	}

	if index >= len(f.answers) {
		return "", nil
	}

	return f.answers[index], nil
}

func validationDocuments() []retrieval.Document {
	return []retrieval.Document{
		{Source: "a.md", Section: "Fees", Content: "fees apply"},
	}
}

// TestAnswerValidatedDisabledIsByteIdentical proves that with validation off the
// raw model output is returned unchanged with Enabled=false and exactly one
// Generate call (the answer itself).
func TestAnswerValidatedDisabledIsByteIdentical(t *testing.T) {
	generator := &queueGenerator{answers: []string{"raw [invented - X] answer"}}

	answer, outcome, err := AnswerValidated(
		context.Background(),
		generator,
		"question",
		validationDocuments(),
		false,
	)
	if err != nil {
		t.Fatalf("AnswerValidated: %v", err)
	}

	if answer != "raw [invented - X] answer" {
		t.Fatalf("disabled path changed the answer: %q", answer)
	}

	if outcome.Enabled {
		t.Fatalf("disabled path reported Enabled=true")
	}

	if generator.calls != 1 {
		t.Fatalf("disabled path made %d Generate calls, want 1", generator.calls)
	}
}

// TestAnswerValidatedCleanAnswerNotRepaired proves a fully valid answer is
// returned unchanged and is not re-prompted.
func TestAnswerValidatedCleanAnswerNotRepaired(t *testing.T) {
	generator := &queueGenerator{answers: []string{"fees apply [a.md - Fees]"}}

	answer, outcome, err := AnswerValidated(
		context.Background(),
		generator,
		"question",
		validationDocuments(),
		true,
	)
	if err != nil {
		t.Fatalf("AnswerValidated: %v", err)
	}

	if answer != "fees apply [a.md - Fees]" {
		t.Fatalf("clean answer changed: %q", answer)
	}

	if outcome.Repaired {
		t.Fatalf("clean answer was repaired")
	}

	if outcome.Valid != 1 || outcome.Total != 1 {
		t.Fatalf("outcome = %d/%d, want 1/1", outcome.Valid, outcome.Total)
	}

	if generator.calls != 1 {
		t.Fatalf("clean answer made %d Generate calls, want 1", generator.calls)
	}
}

// TestAnswerValidatedRepairSucceeds proves an invented citation triggers exactly
// one bounded re-prompt and that a valid repaired answer is returned as-is.
func TestAnswerValidatedRepairSucceeds(t *testing.T) {
	generator := &queueGenerator{answers: []string{
		"fees apply [b.md - Refunds]",
		"fees apply [a.md - Fees]",
	}}

	answer, outcome, err := AnswerValidated(
		context.Background(),
		generator,
		"question",
		validationDocuments(),
		true,
	)
	if err != nil {
		t.Fatalf("AnswerValidated: %v", err)
	}

	if answer != "fees apply [a.md - Fees]" {
		t.Fatalf("repaired answer = %q", answer)
	}

	if !outcome.Repaired {
		t.Fatalf("expected Repaired=true")
	}

	if generator.calls != 2 {
		t.Fatalf("repair path made %d Generate calls, want 2 (one bounded re-prompt)", generator.calls)
	}

	if outcome.Valid != 1 || outcome.Total != 1 {
		t.Fatalf("outcome = %d/%d, want 1/1", outcome.Valid, outcome.Total)
	}
}

// TestAnswerValidatedRepairStillInvalidIsStripped proves that when the single
// re-prompt still emits an invalid citation, it is stripped and never rendered
// as valid.
func TestAnswerValidatedRepairStillInvalidIsStripped(t *testing.T) {
	generator := &queueGenerator{answers: []string{
		"fees apply [b.md - Bad]",
		"fees apply [c.md - Worse]",
	}}

	answer, outcome, err := AnswerValidated(
		context.Background(),
		generator,
		"question",
		validationDocuments(),
		true,
	)
	if err != nil {
		t.Fatalf("AnswerValidated: %v", err)
	}

	if answer != "fees apply " {
		t.Fatalf("residual invalid citation not stripped: %q", answer)
	}

	if outcome.Valid != 0 || outcome.Total != 0 {
		t.Fatalf("outcome = %d/%d, want 0/0 for the stripped answer", outcome.Valid, outcome.Total)
	}

	if outcome.Stripped != 1 {
		t.Fatalf("outcome.Stripped = %d, want 1", outcome.Stripped)
	}

	if generator.calls != 2 {
		t.Fatalf("repair path made %d Generate calls, want 2", generator.calls)
	}
}

// TestAnswerValidatedRepairErrorFallsBackToStrippingOriginal proves a repair
// Generate error does not discard the answer: the original answer's invalid
// citations are stripped and a usable answer is returned, not an error.
func TestAnswerValidatedRepairErrorFallsBackToStrippingOriginal(t *testing.T) {
	generator := &errorGenerator{
		first: "fees apply [a.md - Fees] and [b.md - Bad]",
		err:   errors.New("model unavailable"),
		calls: 0,
	}

	answer, outcome, err := AnswerValidated(
		context.Background(),
		generator,
		"question",
		validationDocuments(),
		true,
	)
	if err != nil {
		t.Fatalf("repair error must not fail the request: %v", err)
	}

	if answer != "fees apply [a.md - Fees] and " {
		t.Fatalf("fallback answer = %q", answer)
	}

	if outcome.Valid != 1 || outcome.Total != 1 {
		t.Fatalf("outcome = %d/%d, want 1/1", outcome.Valid, outcome.Total)
	}

	if !outcome.Repaired {
		t.Fatalf("expected Repaired=true after an attempted repair")
	}
}

// errorGenerator returns first on the first call, then errors on every later
// call, to exercise the repair-error fallback with a bounded call count.
type errorGenerator struct {
	first string
	err   error
	calls int
}

func (g *errorGenerator) Generate(_ context.Context, _ string) (string, error) {
	g.calls++

	if g.calls == 1 {
		return g.first, nil
	}

	return "", g.err
}

// TestAnswerValidatedUncitedAnswerTriggersRepair proves a missing citation is
// treated as needing repair (the plan covers invalid and missing citations): an
// answer that cites nothing takes the single bounded repair path, and a valid
// repaired answer is returned.
func TestAnswerValidatedUncitedAnswerTriggersRepair(t *testing.T) {
	generator := &queueGenerator{
		answers: []string{
			"fees apply",
			"fees apply [a.md - Fees]",
		},
	}

	answer, outcome, err := AnswerValidated(
		context.Background(),
		generator,
		"question",
		validationDocuments(),
		true,
	)
	if err != nil {
		t.Fatalf("AnswerValidated: %v", err)
	}

	if generator.calls != 2 {
		t.Fatalf(
			"uncited answer made %d Generate calls, want 2 (answer + one repair)",
			generator.calls,
		)
	}

	if !outcome.Repaired {
		t.Fatalf("uncited answer was not repaired")
	}

	if answer != "fees apply [a.md - Fees]" {
		t.Fatalf("repaired answer = %q", answer)
	}

	if outcome.Valid != 1 || outcome.Total != 1 {
		t.Fatalf("outcome = %d/%d, want 1/1", outcome.Valid, outcome.Total)
	}
}

// TestAnswerValidatedStillUncitedIsNotInvented proves a still-uncited answer after
// the bounded repair is returned without inventing a citation.
func TestAnswerValidatedStillUncitedIsNotInvented(t *testing.T) {
	generator := &queueGenerator{answers: []string{"fees apply", "fees apply"}}

	answer, outcome, err := AnswerValidated(
		context.Background(),
		generator,
		"question",
		validationDocuments(),
		true,
	)
	if err != nil {
		t.Fatalf("AnswerValidated: %v", err)
	}

	if generator.calls != 2 {
		t.Fatalf("made %d Generate calls, want 2", generator.calls)
	}

	if answer != "fees apply" {
		t.Fatalf("answer = %q, want the uncited answer unchanged", answer)
	}

	if outcome.Valid != 0 || outcome.Total != 0 {
		t.Fatalf("outcome = %d/%d, want 0/0", outcome.Valid, outcome.Total)
	}
}
