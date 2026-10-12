package rag

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"rag-template/internal/retrieval"
)

// failingGeneratorErr is the sentinel error the failure-path stub returns. It
// stands in for an endpoint/generation error (for example an unreachable model
// endpoint) so the test can assert the error is surfaced clearly.
var failingGeneratorErr = errors.New("generation endpoint unreachable")

// blockingGenerator blocks until its context is done and then reports the
// context error. It is used to prove the rag layer does not hang on
// cancellation and honors a bounded request timeout.
type blockingGenerator struct{}

func (blockingGenerator) Generate(ctx context.Context, _ string) (string, error) {
	<-ctx.Done()

	return "", ctx.Err()
}

// TestReliabilityGenerationErrorIsClearAndNoPartialWrite asserts that a
// generation failure surfaces a clear error and yields no partial answer. The
// rag layer returns the generator error unwrapped and an empty answer, so a
// caller can never mistake a failed run for a successful one.
func TestReliabilityGenerationErrorIsClearAndNoPartialWrite(t *testing.T) {
	generator := stubGenerator{err: failingGeneratorErr}

	answer, err := Answer(t.Context(), generator, "What is the late fee?", e2eDocuments())
	if err == nil {
		t.Fatal("Answer() error = nil, want generation error")
	}

	if !errors.Is(err, failingGeneratorErr) {
		t.Fatalf("Answer() error = %v, want %v", err, failingGeneratorErr)
	}

	if answer != "" {
		t.Fatalf("Answer() = %q, want empty answer (no partial write)", answer)
	}
}

// TestReliabilityAnswerLimitedRejectsBeforeGenerate asserts the fail-fast input
// limit path returns a clear error and never reaches the generator, so an
// oversized request produces no partial write.
func TestReliabilityAnswerLimitedRejectsBeforeGenerate(t *testing.T) {
	generator := stubGenerator{answer: "should not be returned"}

	answer, err := AnswerLimited(
		t.Context(),
		generator,
		"What is the late fee?",
		e2eDocuments(),
		InputLimits{MaxQuestionBytes: 1},
	)
	if err == nil {
		t.Fatal("AnswerLimited() error = nil, want limit error")
	}

	if !strings.Contains(err.Error(), "MAX_QUESTION_BYTES") {
		t.Fatalf("AnswerLimited() error = %v, want MAX_QUESTION_BYTES message", err)
	}

	if answer != "" {
		t.Fatalf("AnswerLimited() = %q, want empty answer", answer)
	}
}

// TestReliabilityCancellationDoesNotHang asserts that cancelling the context
// unblocks a generation call promptly instead of hanging, and that the
// cancellation error is propagated.
func TestReliabilityCancellationDoesNotHang(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		_, err := Answer(ctx, blockingGenerator{}, "q", e2eDocuments())
		done <- err
	}()

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Answer() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Answer() hung after context cancellation")
	}
}

// TestReliabilityTimeoutIsBounded asserts that a request timeout produces a
// bounded failure rather than an unbounded wait, and that the timeout error is
// propagated.
func TestReliabilityTimeoutIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()

	done := make(chan error, 1)

	go func() {
		_, err := Answer(ctx, blockingGenerator{}, "q", e2eDocuments())
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Answer() error = %v, want context.DeadlineExceeded", err)
		}

		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("Answer() took %s, want bounded failure", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Answer() hung after request timeout")
	}
}

// TestReliabilityEmptyContextPlaceholder asserts the retrieved provenance
// surface is stable when retrieval returns no documents, so the E2E test's
// provenance assertions remain meaningful.
func TestReliabilityEmptyContextPlaceholder(t *testing.T) {
	prompt := formatAnswerPrompt("q", nil)

	if !strings.Contains(prompt, "(no retrieved context)") {
		t.Fatalf("formatAnswerPrompt() = %q, want empty-context placeholder", prompt)
	}

	var _ = retrieval.Document{}
}
