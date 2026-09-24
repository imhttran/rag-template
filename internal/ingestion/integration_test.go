// PostgreSQL integration tests for ReplaceDocument's SQL.
//
// They are skipped unless RAG_INTEGRATION=1 is set, so `go test ./...` (and the
// pre-commit hook) stays fast. TestMain connects to DATABASE_URL and applies
// migrations/001_init.sql, so they run against the docker-compose database
// (`make db-up`). Embeddings come from a stub Ollama server, so no model is
// needed.
//
//	RAG_INTEGRATION=1 go test ./internal/ingestion/ -run Integration -v
//
// These tests delete only the rows they own, but internal/retrieval's
// integration tests truncate the whole table, so the two packages must not run
// in parallel -- the Makefile's integration target passes -p 1.

package ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"rag-template/internal/chunking"
	"rag-template/internal/config"
	"rag-template/internal/embedding"
	"rag-template/internal/ollama"
	"rag-template/internal/retrieval"
)

// embeddingDim matches the vector(768) column in migrations/001_init.sql.
const embeddingDim = 768

// testSource is the source every test in this file writes.
const testSource = "ingestion-test.md"

// testConn is the shared database connection; it stays nil when the integration
// tests are not enabled.
var testConn *pgx.Conn

func TestMain(m *testing.M) {
	if os.Getenv("RAG_INTEGRATION") == "" {
		os.Exit(m.Run())
	}

	ctx := context.Background()

	conn, err := openTestDatabase(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration setup: %v\n", err)
		os.Exit(1)
	}

	testConn = conn

	code := m.Run()

	if err := conn.Close(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "close connection: %v\n", err)
	}

	os.Exit(code)
}

// openTestDatabase connects to DATABASE_URL and applies the migration, so the
// tests work against a fresh docker-compose database.
func openTestDatabase(ctx context.Context) (*pgx.Conn, error) {
	conn, err := pgx.Connect(ctx, config.Load().DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	if err := applyMigration(ctx, conn); err != nil {
		_ = conn.Close(ctx)

		return nil, err
	}

	return conn, nil
}

// applyMigration runs migrations/001_init.sql, whose statements are all
// idempotent.
func applyMigration(ctx context.Context, conn *pgx.Conn) error {
	migration, err := os.ReadFile(
		filepath.Join("..", "..", "migrations", "001_init.sql"),
	)
	if err != nil {
		return fmt.Errorf("read migration: %w", err)
	}

	if _, err := conn.Exec(ctx, string(migration)); err != nil {
		return fmt.Errorf("apply migration: %w", err)
	}

	return nil
}

// requireDatabase skips a test unless TestMain connected to the database.
func requireDatabase(t *testing.T) {
	t.Helper()

	if testConn == nil {
		t.Skip(
			"set RAG_INTEGRATION=1 (with the database up) to run the " +
				"PostgreSQL integration tests",
		)
	}
}

// stubVector is the embedding the fake Ollama server returns for every input.
func stubVector() []float64 {
	vector := make([]float64, embeddingDim)

	for i := range vector {
		vector[i] = float64(i%7) + 1
	}

	return vector
}

// fakeOllama serves /api/embed with stubVector, and fails when the input
// contains "FAIL" so a test can exercise the embedding error path.
func fakeOllama(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		var payload struct {
			Input string `json:"input"`
		}

		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(writer, "bad request", http.StatusBadRequest)

			return
		}

		if strings.Contains(payload.Input, "FAIL") {
			http.Error(writer, "embed failed", http.StatusInternalServerError)

			return
		}

		writer.Header().Set("Content-Type", "application/json")

		_ = json.NewEncoder(writer).Encode(map[string]any{
			"embeddings": [][]float64{stubVector()},
		})
	}))

	t.Cleanup(server.Close)

	return server
}

// newIngester returns an Ingester whose embeddings come from the fake server.
func newIngester(t *testing.T) *Ingester {
	t.Helper()

	server := fakeOllama(t)

	return New(
		testConn,
		embedding.New(ollama.New(server.URL, server.Client()), "fake"),
	)
}

// resetTestDocument removes the rows the tests in this file own.
func resetTestDocument(t *testing.T, ctx context.Context) {
	t.Helper()

	if _, err := testConn.Exec(
		ctx,
		"DELETE FROM documents WHERE source = $1",
		testSource,
	); err != nil {
		t.Fatalf("delete the test document: %v", err)
	}
}

// storedChunks returns the stored chunks of testSource as "section|index|content"
// in section and chunk order.
func storedChunks(t *testing.T, ctx context.Context) []string {
	t.Helper()

	rows, err := testConn.Query(
		ctx,
		`
		SELECT
		    section,
		    chunk_index,
		    content
		FROM documents
		WHERE source = $1
		ORDER BY section, chunk_index
		`,
		testSource,
	)
	if err != nil {
		t.Fatalf("query the stored chunks: %v", err)
	}
	defer rows.Close()

	var chunks []string

	for rows.Next() {
		var (
			section string
			index   int
			content string
		)

		if err := rows.Scan(&section, &index, &content); err != nil {
			t.Fatalf("scan a stored chunk: %v", err)
		}

		chunks = append(chunks, fmt.Sprintf("%s|%d|%s", section, index, content))
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("iterate the stored chunks: %v", err)
	}

	return chunks
}

func TestReplaceDocumentIntegration(t *testing.T) {
	ctx := context.Background()
	requireDatabase(t)

	resetTestDocument(t, ctx)

	ingester := newIngester(t)

	chunks := []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "first chunk"},
		{Section: "Fees", Index: 1, Content: "second chunk"},
		{Section: "Refunds", Index: 0, Content: "third chunk"},
	}

	if err := ingester.ReplaceDocument(ctx, testSource, chunks); err != nil {
		t.Fatalf("replace document: %v", err)
	}

	want := []string{
		"Fees|0|first chunk",
		"Fees|1|second chunk",
		"Refunds|0|third chunk",
	}

	if got := storedChunks(t, ctx); !slices.Equal(got, want) {
		t.Fatalf("stored chunks = %v, want %v", got, want)
	}

	// The vector round-trips through the $5::vector cast: searching with the
	// stub vector returns this document with cosine similarity 1.
	documents, err := retrieval.New(testConn).Search(ctx, stubVector(), 1)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(documents) != 1 || documents[0].Source != testSource {
		t.Fatalf("expected the stored document, got %+v", documents)
	}

	if math.Abs(documents[0].Similarity-1) > 1e-6 {
		t.Fatalf(
			"expected similarity 1 for the stored vector, got %v",
			documents[0].Similarity,
		)
	}
}

func TestReplaceDocumentIntegrationReplaces(t *testing.T) {
	ctx := context.Background()
	requireDatabase(t)

	resetTestDocument(t, ctx)

	ingester := newIngester(t)

	if err := ingester.ReplaceDocument(ctx, testSource, []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "old one"},
		{Section: "Fees", Index: 1, Content: "old two"},
	}); err != nil {
		t.Fatalf("first replace: %v", err)
	}

	if err := ingester.ReplaceDocument(ctx, testSource, []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "new only"},
	}); err != nil {
		t.Fatalf("second replace: %v", err)
	}

	want := []string{"Fees|0|new only"}

	if got := storedChunks(t, ctx); !slices.Equal(got, want) {
		t.Fatalf("stored chunks after replacing = %v, want %v", got, want)
	}
}

func TestReplaceDocumentIntegrationKeepsDocumentOnEmbedError(t *testing.T) {
	ctx := context.Background()
	requireDatabase(t)

	resetTestDocument(t, ctx)

	ingester := newIngester(t)

	if err := ingester.ReplaceDocument(ctx, testSource, []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "keep me"},
	}); err != nil {
		t.Fatalf("seed replace: %v", err)
	}

	// Embeddings are generated before the transaction starts, so a failure
	// leaves the stored document unchanged.
	err := ingester.ReplaceDocument(ctx, testSource, []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "FAIL to embed"},
	})
	if err == nil {
		t.Fatal("expected the embedding error to surface")
	}

	want := []string{"Fees|0|keep me"}

	if got := storedChunks(t, ctx); !slices.Equal(got, want) {
		t.Fatalf("stored chunks after a failed embed = %v, want %v", got, want)
	}
}
