package ingestion

import (
	"context"
	"fmt"
	"sync"
	"time"

	"rag-template/internal/chunking"
)

// embedFunc embeds one chunk of text. It matches embedding.Embedder.Embed so
// tests can substitute a stub.
type embedFunc func(ctx context.Context, text string) ([]float64, error)

// mapOrdered runs fn for every input with at most workers running at once and
// returns the results indexed by input position, so the output order always
// matches the input order regardless of completion order. The first error to be
// observed cancels the remaining work and is returned; results is nil on error.
func mapOrdered[T any, R any](
	ctx context.Context,
	inputs []T,
	workers int,
	fn func(ctx context.Context, index int, input T) (R, error),
) ([]R, error) {
	results := make([]R, len(inputs))

	if len(inputs) == 0 {
		return results, nil
	}

	if workers < 1 {
		workers = 1
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		sem      = make(chan struct{}, workers)
		errOnce  sync.Once
		firstErr error
	)

	for index, input := range inputs {
		select {
		case <-ctx.Done():
		case sem <- struct{}{}:
			wg.Add(1)

			go func(index int, input T) {
				defer wg.Done()
				defer func() { <-sem }()

				result, err := fn(ctx, index, input)
				if err != nil {
					errOnce.Do(func() {
						firstErr = err

						cancel()
					})

					return
				}

				results[index] = result
			}(index, input)
		}
	}

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	return results, nil
}

// retryEmbed calls embed for chunk, retrying failures with exponential backoff
// up to retries times (so the call runs at most retries+1 times). backoff is the
// delay before the second attempt and doubles each retry, capped at
// maxRetryBackoff. The returned error names the chunk so the caller can locate
// the failure. It is a free function so tests can drive it with a stub embedder.
func retryEmbed(
	ctx context.Context,
	embed embedFunc,
	chunk chunking.Chunk,
	retries int,
	backoff time.Duration,
) ([]float64, error) {
	if retries < 0 {
		retries = 0
	}

	if backoff <= 0 {
		backoff = defaultRetryBackoff
	}

	var lastErr error

	for attempt := 0; attempt <= retries; attempt++ {
		vector, err := embed(ctx, chunk.Content)
		if err == nil {
			return vector, nil
		}

		lastErr = err

		// A canceled or expired context is permanent: stop rather than spend the
		// remaining attempts on a call that can only fail again. Provider errors
		// that are permanent (for example a 4xx) are not retried on their own,
		// because the shared Ollama client does not classify them; the retry is
		// still strictly bounded, so at most retries+1 attempts are made.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf(
				"embed chunk %d in section %q: %w",
				chunk.Index,
				chunk.Section,
				ctxErr,
			)
		}

		if attempt == retries {
			break
		}

		if waitErr := sleepContext(ctx, backoff); waitErr != nil {
			return nil, fmt.Errorf(
				"embed chunk %d in section %q: %w",
				chunk.Index,
				chunk.Section,
				waitErr,
			)
		}

		if backoff < maxRetryBackoff {
			backoff *= 2

			if backoff > maxRetryBackoff {
				backoff = maxRetryBackoff
			}
		}
	}

	return nil, fmt.Errorf(
		"embed chunk %d in section %q after %d attempts: %w",
		chunk.Index,
		chunk.Section,
		retries+1,
		lastErr,
	)
}

// sleepContext waits for delay or until ctx is done, whichever comes first.
func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
