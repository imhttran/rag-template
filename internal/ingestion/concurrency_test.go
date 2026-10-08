package ingestion

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"rag-template/internal/chunking"
)

// TestMapOrderedPreservesOrder verifies that concurrent work with variable
// latency still returns results in input order and tracks peak concurrency.
func TestMapOrderedPreservesOrder(t *testing.T) {
	const n = 20

	inputs := make([]int, n)
	for i := range inputs {
		inputs[i] = i
	}

	var (
		inFlight atomic.Int64
		peak     atomic.Int64
	)

	results, err := mapOrdered(
		context.Background(),
		inputs,
		4,
		func(_ context.Context, index int, input int) (string, error) {
			current := inFlight.Add(1)

			for {
				observed := peak.Load()
				if current <= observed || peak.CompareAndSwap(observed, current) {
					break
				}
			}

			// Make later inputs finish sooner, so completion order runs
			// opposite to input order.
			time.Sleep(time.Duration(n-index) * time.Millisecond)

			inFlight.Add(-1)

			return fmt.Sprintf("value-%d", input), nil
		},
	)
	if err != nil {
		t.Fatalf("mapOrdered: %v", err)
	}

	if len(results) != n {
		t.Fatalf("len(results) = %d, want %d", len(results), n)
	}

	for index, got := range results {
		want := fmt.Sprintf("value-%d", index)
		if got != want {
			t.Fatalf("results[%d] = %q, want %q", index, got, want)
		}
	}

	if peak.Load() > 4 {
		t.Fatalf("peak concurrency = %d, want <= 4", peak.Load())
	}
}

// TestMapOrderedPropagatesFirstError verifies that a failure cancels the
// remaining work and is returned.
func TestMapOrderedPropagatesFirstError(t *testing.T) {
	inputs := []int{0, 1, 2, 3, 4, 5}

	_, err := mapOrdered(
		context.Background(),
		inputs,
		2,
		func(_ context.Context, index int, _ int) (int, error) {
			if index == 0 {
				return 0, fmt.Errorf("boom at %d", index)
			}

			time.Sleep(50 * time.Millisecond)

			return index, nil
		},
	)
	if err == nil {
		t.Fatal("expected an error")
	}
}

// TestRetryEmbedRetriesThenSucceeds verifies the retry bound: a stub that fails
// transiently a couple of times still succeeds within the configured attempts.
func TestRetryEmbedRetriesThenSucceeds(t *testing.T) {
	const failures = 2

	var attempts atomic.Int64

	stub := func(_ context.Context, _ string) ([]float64, error) {
		attempt := attempts.Add(1)
		if attempt <= failures {
			return nil, fmt.Errorf("transient failure %d", attempt)
		}

		return []float64{1, 2, 3}, nil
	}

	chunk := chunking.Chunk{Section: "Fees", Index: 0, Content: "retry me"}

	if _, err := retryEmbed(
		context.Background(),
		stub,
		chunk,
		failures,
		time.Millisecond,
	); err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}

	if got := attempts.Load(); got != failures+1 {
		t.Fatalf("attempts = %d, want %d", got, failures+1)
	}
}

// TestRetryEmbedExhaustsRetries verifies that a persistent failure exhausts the
// bound and surfaces an error naming the chunk.
func TestRetryEmbedExhaustsRetries(t *testing.T) {
	var attempts atomic.Int64

	stub := func(_ context.Context, _ string) ([]float64, error) {
		attempts.Add(1)

		return nil, fmt.Errorf("always fails")
	}

	chunk := chunking.Chunk{Section: "Fees", Index: 3, Content: "always fails"}

	_, err := retryEmbed(
		context.Background(),
		stub,
		chunk,
		2,
		time.Millisecond,
	)
	if err == nil {
		t.Fatal("expected the retries to be exhausted")
	}

	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

// A canceled context is a permanent condition: the retries must stop immediately
// instead of spending the remaining attempts on a call that cannot succeed.
func TestRetryEmbedStopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var attempts atomic.Int64

	stub := func(_ context.Context, _ string) ([]float64, error) {
		attempts.Add(1)
		cancel()

		return nil, fmt.Errorf("context canceled")
	}

	_, err := retryEmbed(
		ctx,
		stub,
		chunking.Chunk{Section: "Fees", Index: 0, Content: "cancel me"},
		3,
		time.Millisecond,
	)
	if err == nil {
		t.Fatal("expected an error after cancellation")
	}

	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry after cancellation)", got)
	}
}
