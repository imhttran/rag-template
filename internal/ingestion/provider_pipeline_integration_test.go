// End-to-end integration test for the pluggable Embedder seam.
//
// It wires a stub in-memory embedding.Embedder (no HTTP, no vendor type) into
// ingestion.ReplaceDocument, then retrieves the stored chunks with
// retrieval.New(conn).Search and asserts the stub vector round-trips with
// cosine similarity 1. It reuses the package TestMain + applyMigrations harness
// from integration_test.go and is skipped unless RAG_INTEGRATION=1 is set:
//
//	RAG_INTEGRATION=1 go test ./internal/ingestion/ -run Integration -p 1
//
// No vendor type (internal/ollama) appears anywhere on this path: the stub
// satisfies embedding.Embedder directly, proving the ingestion/retrieval
// pipeline depends only on the interface. Ollama remains the configured default
// provider (internal/config/config.go); this test does not change that.

package ingestion

import (
	"context"
	"math"
	"testing"

	"rag-template/internal/chunking"
	"rag-template/internal/retrieval"
)

// stubProviderSource is the source this test owns. It reuses testSource so the
// shared resetTestDocument cleanup stays consistent with the rest of the file's
// integration tests.
const stubProviderSource = testSource

// stubProviderEmbedder is an in-memory embedding.Embedder with no HTTP client
// and no vendor dependency. It returns the same deterministic vector for every
// input so the stored vector can be compared exactly on retrieval.
type stubProviderEmbedder struct {
	vector []float64
}

// Embed returns the fixed stub vector, ignoring the input text.
func (s stubProviderEmbedder) Embed(
	_ context.Context,
	_ string,
) ([]float64, error) {
	return s.vector, nil
}

// stubProviderVector is a deterministic non-trivial vector. Every component is
// non-zero so cosine similarity is well defined and equals exactly 1 when the
// same vector round-trips through pgvector.
func stubProviderVector() []float64 {
	vector := make([]float64, embeddingDim)

	for i := range vector {
		vector[i] = float64(i%11) + 1
	}

	return vector
}

// TestStubProviderPipelineIntegration proves a stub provider drives ingestion
// and retrieval end-to-end with no vendor type. It is named with the
// Integration suffix so it is selected by -run Integration and is skipped
// unless RAG_INTEGRATION=1 (via requireDatabase, which mirrors the harness's
// skip behaviour).
func TestStubProviderPipelineIntegration(t *testing.T) {
	ctx := context.Background()
	requireDatabase(t)

	resetTestDocument(t, ctx)

	vector := stubProviderVector()
	embedder := stubProviderEmbedder{vector: vector}

	ingester := New(testConn, embedder)

	chunks := []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "stub provider first chunk"},
		{Section: "Fees", Index: 1, Content: "stub provider second chunk"},
		{Section: "Refunds", Index: 0, Content: "stub provider third chunk"},
	}

	if err := ingester.ReplaceDocument(ctx, stubProviderSource, chunks); err != nil {
		t.Fatalf("replace document with stub provider: %v", err)
	}

	// Retrieval uses the same stub vector, so the nearest document must be the
	// one just stored, with cosine similarity 1.
	documents, err := retrieval.New(testConn).Search(ctx, vector, 1)
	if err != nil {
		t.Fatalf("search with stub vector: %v", err)
	}

	if len(documents) == 0 {
		t.Fatal("expected the stub-ingested document to be retrievable")
	}

	got := documents[0]
	if got.Source != stubProviderSource {
		t.Fatalf("retrieved source = %q, want %q", got.Source, stubProviderSource)
	}

	if math.Abs(got.Similarity-1) > 1e-6 {
		t.Fatalf(
			"expected cosine similarity 1 for the round-tripped stub vector, got %v",
			got.Similarity,
		)
	}
}
