package reranking

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"rag-template/internal/generation"
	"rag-template/internal/retrieval"
)

func TestParseRanking(t *testing.T) {
	tests := []struct {
		name          string
		response      string
		documentCount int
		want          []int
		wantErr       bool
	}{
		{
			name:          "plain JSON array",
			response:      "[2,0,1]",
			documentCount: 3,
			want:          []int{2, 0, 1},
		},
		{
			name:          "fenced JSON array",
			response:      "```json\n[2,0,1]\n```",
			documentCount: 3,
			want:          []int{2, 0, 1},
		},
		{
			name:          "bare fenced array",
			response:      "```\n[1,0]\n```",
			documentCount: 2,
			want:          []int{1, 0},
		},
		{
			name:          "surrounding whitespace",
			response:      "\n  [0]  \n",
			documentCount: 1,
			want:          []int{0},
		},
		{
			name:          "empty ranking",
			response:      "[]",
			documentCount: 0,
			want:          []int{},
		},
		{
			name:          "too few IDs",
			response:      "[0,1]",
			documentCount: 3,
			wantErr:       true,
		},
		{
			name:          "too many IDs",
			response:      "[0,1,2,3]",
			documentCount: 3,
			wantErr:       true,
		},
		{
			name:          "duplicate ID",
			response:      "[0,0,1]",
			documentCount: 3,
			wantErr:       true,
		},
		{
			name:          "out of range ID",
			response:      "[0,1,3]",
			documentCount: 3,
			wantErr:       true,
		},
		{
			name:          "negative ID",
			response:      "[-1,0,1]",
			documentCount: 3,
			wantErr:       true,
		},
		{
			name:          "not JSON",
			response:      "The ranking is [0,1,2]",
			documentCount: 3,
			wantErr:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseRanking(
				test.response,
				test.documentCount,
			)

			if test.wantErr {
				if err == nil {
					t.Fatalf(
						"parseRanking(%q, %d) = %v, want error",
						test.response,
						test.documentCount,
						got,
					)
				}

				return
			}

			if err != nil {
				t.Fatalf(
					"parseRanking(%q, %d): unexpected error: %v",
					test.response,
					test.documentCount,
					err,
				)
			}

			if !slices.Equal(got, test.want) {
				t.Fatalf(
					"parseRanking(%q, %d) = %v, want %v",
					test.response,
					test.documentCount,
					got,
					test.want,
				)
			}
		})
	}
}

// fakeGenerator is a generation.Generator whose Generate behavior is supplied by
// a function, so tests can simulate malformed responses, errors, and slow calls.
type fakeGenerator struct {
	generate func(ctx context.Context, prompt string) (string, error)
}

func (f fakeGenerator) Generate(
	ctx context.Context,
	prompt string,
) (string, error) {
	if f.generate == nil {
		return "", errors.New("no generate function configured")
	}

	return f.generate(ctx, prompt)
}

func testDocuments() []retrieval.Document {
	return []retrieval.Document{
		{Source: "a.md", Section: "A", ChunkIndex: 0, Content: "alpha"},
		{Source: "b.md", Section: "B", ChunkIndex: 0, Content: "bravo"},
		{Source: "c.md", Section: "C", ChunkIndex: 0, Content: "charlie"},
	}
}

func sameOrder(a, b []retrieval.Document) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].Source != b[i].Source ||
			a[i].Section != b[i].Section ||
			a[i].ChunkIndex != b[i].ChunkIndex {
			return false
		}
	}

	return true
}

// TestRerankLLMFallbackToFusedOrder asserts that a malformed or invalid reranker
// response degrades to the fused (input) order and surfaces no error.
func TestRerankLLMFallbackToFusedOrder(t *testing.T) {
	documents := testDocuments()

	tests := []struct {
		name     string
		response string
	}{
		{name: "not JSON", response: "there is no JSON here"},
		{name: "wrong candidate count", response: "[0,1]"},
		{name: "out of range ID", response: "[0,1,3]"},
		{name: "duplicate ID", response: "[0,0,1]"},
		{name: "empty response", response: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			generator := fakeGenerator{
				generate: func(context.Context, string) (string, error) {
					return test.response, nil
				},
			}

			got, err := RerankLLM(
				context.Background(),
				generator,
				"question",
				documents,
			)
			if err != nil {
				t.Fatalf("RerankLLM returned error: %v", err)
			}

			if !sameOrder(got, documents) {
				t.Fatalf(
					"RerankLLM fallback = %v, want fused order %v",
					got,
					documents,
				)
			}
		})
	}
}

// TestRerankLLMGeneratorErrorFallsBack asserts a generator error degrades to the
// fused order without surfacing an error.
func TestRerankLLMGeneratorErrorFallsBack(t *testing.T) {
	documents := testDocuments()

	generator := fakeGenerator{
		generate: func(context.Context, string) (string, error) {
			return "", errors.New("generator unavailable")
		},
	}

	got, err := RerankLLM(
		context.Background(),
		generator,
		"question",
		documents,
	)
	if err != nil {
		t.Fatalf("RerankLLM returned error: %v", err)
	}

	if !sameOrder(got, documents) {
		t.Fatalf("RerankLLM = %v, want fused order %v", got, documents)
	}
}

// TestRerankLLMSuccessfulOrder asserts a valid ranking is applied as returned.
func TestRerankLLMSuccessfulOrder(t *testing.T) {
	documents := testDocuments()

	generator := fakeGenerator{
		generate: func(context.Context, string) (string, error) {
			return "[2,0,1]", nil
		},
	}

	got, err := RerankLLM(
		context.Background(),
		generator,
		"question",
		documents,
	)
	if err != nil {
		t.Fatalf("RerankLLM returned error: %v", err)
	}

	want := []retrieval.Document{documents[2], documents[0], documents[1]}

	if !sameOrder(got, want) {
		t.Fatalf("RerankLLM = %v, want %v", got, want)
	}
}

// TestRerankLLMDisabledReturnsInput asserts the disabled path (no documents)
// returns the input unchanged.
func TestRerankLLMDisabledReturnsInput(t *testing.T) {
	generator := fakeGenerator{
		generate: func(context.Context, string) (string, error) {
			t.Fatal("generator must not be called for an empty document set")

			return "", nil
		},
	}

	got, err := RerankLLM(
		context.Background(),
		generator,
		"question",
		nil,
	)
	if err != nil {
		t.Fatalf("RerankLLM returned error: %v", err)
	}

	if got != nil {
		t.Fatalf("RerankLLM = %v, want nil", got)
	}
}

// TestRerankLLMLatencyGuardExpires asserts the latency guard bounds the wait and
// falls back to the fused order when the reranker is slow.
func TestRerankLLMLatencyGuardExpires(t *testing.T) {
	documents := testDocuments()

	started := make(chan struct{})

	generator := fakeGenerator{
		generate: func(ctx context.Context, _ string) (string, error) {
			close(started)

			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(5 * time.Second):
				return "[2,0,1]", nil
			}
		},
	}

	start := time.Now()

	got, err := RerankLLMWithTimeout(
		context.Background(),
		generator,
		"question",
		documents,
		50*time.Millisecond,
	)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("RerankLLMWithTimeout returned error: %v", err)
	}

	<-started

	if !sameOrder(got.Documents, documents) {
		t.Fatalf("guard fallback = %v, want fused order %v", got, documents)
	}

	if !got.FellBack {
		t.Fatalf("guard expiry should report a fallback: %+v", got)
	}

	if got.Reason != FallbackTimeout {
		t.Fatalf("guard expiry reason = %q, want %q", got.Reason, FallbackTimeout)
	}

	if elapsed > time.Second {
		t.Fatalf("guard took %s, want it bounded well under a second", elapsed)
	}
}

// TestRerankLLMNoGuardWhenZero asserts a zero timeout disables the guard, so a
// slow-but-valid reranker still returns its ordering.
func TestRerankLLMNoGuardWhenZero(t *testing.T) {
	documents := testDocuments()

	generator := fakeGenerator{
		generate: func(context.Context, string) (string, error) {
			time.Sleep(20 * time.Millisecond)

			return "[2,0,1]", nil
		},
	}

	got, err := RerankLLMWithTimeout(
		context.Background(),
		generator,
		"question",
		documents,
		0,
	)
	if err != nil {
		t.Fatalf("RerankLLMWithTimeout returned error: %v", err)
	}

	want := []retrieval.Document{documents[2], documents[0], documents[1]}

	if !sameOrder(got.Documents, want) {
		t.Fatalf("RerankLLMWithTimeout = %v, want %v", got, want)
	}

	if got.FellBack {
		t.Fatalf("zero timeout must not fall back: %+v", got)
	}
}

// TestRerankLLMEmptyDocumentsReportsDisabled asserts the no-candidates case is
// reported as disabled, not as a reranker failure, so callers (and eval) can
// tell "nothing to rank" from a genuine fallback.
func TestRerankLLMEmptyDocumentsReportsDisabled(t *testing.T) {
	generator := fakeGenerator{
		generate: func(context.Context, string) (string, error) {
			t.Fatal("generator must not be called for an empty document set")

			return "", nil
		},
	}

	result, err := RerankLLMWithTimeout(
		context.Background(),
		generator,
		"question",
		nil,
		time.Second,
	)
	if err != nil {
		t.Fatalf("RerankLLMWithTimeout returned error: %v", err)
	}

	if !result.FellBack || result.Reason != FallbackDisabled {
		t.Fatalf(
			"empty documents = %+v, want FellBack with reason %q",
			result,
			FallbackDisabled,
		)
	}
}

// TestRerankLLMGuardDoesNotCancelCallerContext asserts the latency guard cancels
// only its own derived context and never the caller's shared context.
func TestRerankLLMGuardDoesNotCancelCallerContext(t *testing.T) {
	documents := testDocuments()

	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	generator := fakeGenerator{
		generate: func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()

			return "", ctx.Err()
		},
	}

	result, err := RerankLLMWithTimeout(
		parent,
		generator,
		"question",
		documents,
		20*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("RerankLLMWithTimeout returned error: %v", err)
	}

	if parent.Err() != nil {
		t.Fatalf("latency guard cancelled the caller's context: %v", parent.Err())
	}

	if result.Reason != FallbackTimeout {
		t.Fatalf("guard expiry reason = %q, want %q", result.Reason, FallbackTimeout)
	}
}

// TestRerankLLMCallerDeadlineNotGuardTimeout asserts that a deadline inherited
// from the caller's context is reported as a generator error, not misattributed
// to the guard as FallbackTimeout.
func TestRerankLLMCallerDeadlineNotGuardTimeout(t *testing.T) {
	documents := testDocuments()

	parent, cancelParent := context.WithTimeout(
		context.Background(),
		20*time.Millisecond,
	)
	defer cancelParent()

	generator := fakeGenerator{
		generate: func(ctx context.Context, _ string) (string, error) {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(2 * time.Second):
				return "[2,0,1]", nil
			}
		},
	}

	// The guard's own limit is far longer than the caller's deadline, so the
	// failure is the caller's, not the guard's.
	result, err := RerankLLMWithTimeout(
		parent,
		generator,
		"question",
		documents,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("RerankLLMWithTimeout returned error: %v", err)
	}

	if result.Reason == FallbackTimeout {
		t.Fatalf("caller deadline misclassified as guard timeout: %+v", result)
	}

	if !result.FellBack {
		t.Fatalf("caller deadline should fall back to the fused order: %+v", result)
	}
}

var _ generation.Generator = fakeGenerator{}
