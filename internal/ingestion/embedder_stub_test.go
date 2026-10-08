package ingestion

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"rag-template/internal/chunking"
)

// stubEmbedder is an embedding.Embedder that performs no HTTP and returns a
// fixed vector for every input. It proves ingestion depends only on the
// embedding.Embedder interface rather than the concrete Ollama-backed
// implementation.
type stubEmbedder struct {
	vector []float64
	err    error

	calls atomic.Int64
}

func (s *stubEmbedder) Embed(_ context.Context, _ string) ([]float64, error) {
	s.calls.Add(1)

	if s.err != nil {
		return nil, s.err
	}

	return s.vector, nil
}

// Compile-time assertion that the stub satisfies the interface ingestion
// expects.
var _ interface {
	Embed(ctx context.Context, text string) ([]float64, error)
} = (*stubEmbedder)(nil)

// TestStubEmbedderDrivesReplaceDocument wires a no-HTTP stub Embedder into the
// Ingester and exercises the embedding path via embedAll. ReplaceDocument's
// database write is covered by the integration tests; here we assert the
// interface seam is what ingestion depends on and that no vendor type is
// needed.
func TestStubEmbedderDrivesReplaceDocument(t *testing.T) {
	stub := &stubEmbedder{vector: []float64{1, 2, 3}}

	ingester := NewWithOptions(nil, stub, 2, 1)

	chunks := []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "first"},
		{Section: "Fees", Index: 1, Content: "second"},
	}

	embedded, err := ingester.embedAll(context.Background(), chunks)
	if err != nil {
		t.Fatalf("embedAll with stub embedder: %v", err)
	}

	if got, want := stub.calls.Load(), int64(len(chunks)); got != want {
		t.Fatalf("stub embed calls = %d, want %d", got, want)
	}

	if len(embedded) != len(chunks) {
		t.Fatalf("embedded chunks = %d, want %d", len(embedded), len(chunks))
	}

	// The stub vector is threaded through unchanged and results are in input
	// order regardless of completion order.
	for index, item := range embedded {
		if item.chunk.Section != chunks[index].Section ||
			item.chunk.Index != chunks[index].Index {
			t.Fatalf("embedded[%d] chunk = %+v, want %+v", index, item.chunk, chunks[index])
		}

		if len(item.vector) != len(stub.vector) {
			t.Fatalf("embedded[%d] vector = %v, want %v", index, item.vector, stub.vector)
		}
	}
}

// TestStubEmbedderEmbedError proves the interface seam surfaces embedder errors
// without any HTTP or vendor dependency.
func TestStubEmbedderEmbedError(t *testing.T) {
	stub := &stubEmbedder{err: errors.New("no http here")}

	ingester := NewWithOptions(nil, stub, 1, 0)

	_, err := ingester.embedAll(context.Background(), []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "first"},
	})
	if err == nil {
		t.Fatal("expected the stub embedder error to surface")
	}
}
