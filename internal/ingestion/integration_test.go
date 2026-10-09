// PostgreSQL integration tests for ReplaceDocument's SQL.
//
// They are skipped unless RAG_INTEGRATION=1 is set, so `go test ./...` (and the
// pre-commit hook) stays fast. TestMain connects to DATABASE_URL and applies
// every migration in migrations/, so they run against the docker-compose database
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
	"sync/atomic"
	"testing"
	"time"

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
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	if err := applyMigrations(ctx, conn); err != nil {
		_ = conn.Close(ctx)

		return nil, err
	}

	return conn, nil
}

// applyMigrations runs every migrations/*.sql file in name order, the way
// `make db-schema` does, so the tests work against a fresh database and pick up
// migrations added after 001 (for example 002_ingestion_metadata.sql). Every
// statement is idempotent.
func applyMigrations(ctx context.Context, conn *pgx.Conn) error {
	paths, err := filepath.Glob(
		filepath.Join("..", "..", "migrations", "*.sql"),
	)
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}

	slices.Sort(paths)

	for _, path := range paths {
		migration, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", path, err)
		}

		if _, err := conn.Exec(ctx, string(migration)); err != nil {
			return fmt.Errorf("apply migration %s: %w", path, err)
		}
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

// flakyOllama serves /api/embed like fakeOllama but fails the first failFirst
// requests, so a test can exercise transient failures and bounded retries. It
// returns the server and a counter of the requests it received.
func flakyOllama(t *testing.T, failFirst int) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var attempts atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		if attempts.Add(1) <= int64(failFirst) {
			http.Error(writer, "transient embed failure", http.StatusInternalServerError)

			return
		}

		writer.Header().Set("Content-Type", "application/json")

		_ = json.NewEncoder(writer).Encode(map[string]any{
			"embeddings": [][]float64{stubVector()},
		})
	}))

	t.Cleanup(server.Close)

	return server, &attempts
}

// newIngesterWith returns an Ingester wired to server with an explicit worker
// count and retry bound, so retry tests can pin concurrency to 1 and make the
// request count deterministic.
func newIngesterWith(
	t *testing.T,
	server *httptest.Server,
	workers int,
	retries int,
) *Ingester {
	t.Helper()

	return NewWithOptions(
		testConn,
		embedding.New(ollama.New(server.URL, server.Client()), "fake"),
		workers,
		retries,
	)
}

// A flaky embedder must be retried a bounded number of times: with one worker the
// first two requests fail, so the ingest needs exactly two extra attempts and
// still stores every chunk in order.
func TestReplaceDocumentIntegrationRetriesTransientEmbedFailures(t *testing.T) {
	ctx := context.Background()
	requireDatabase(t)

	resetTestDocument(t, ctx)

	server, attempts := flakyOllama(t, 2)
	ingester := newIngesterWith(t, server, 1, 3)

	chunks := []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "first chunk"},
		{Section: "Fees", Index: 1, Content: "second chunk"},
		{Section: "Refunds", Index: 0, Content: "third chunk"},
	}

	if err := ingester.ReplaceDocument(ctx, testSource, chunks); err != nil {
		t.Fatalf("replace document with transient failures: %v", err)
	}

	if got, want := attempts.Load(), int64(2+len(chunks)); got != want {
		t.Fatalf(
			"embedding attempts = %d, want %d (one per chunk plus the two retries)",
			got,
			want,
		)
	}

	want := []string{
		"Fees|0|first chunk",
		"Fees|1|second chunk",
		"Refunds|0|third chunk",
	}

	if got := storedChunks(t, ctx); !slices.Equal(got, want) {
		t.Fatalf("stored chunks after retries = %v, want %v", got, want)
	}
}

// A permanently failing embedder must exhaust the bounded retries, fail the
// ingest, and leave the previously stored document untouched.
func TestReplaceDocumentIntegrationRetryExhaustionPreservesDocument(t *testing.T) {
	ctx := context.Background()
	requireDatabase(t)

	resetTestDocument(t, ctx)

	server := fakeOllama(t)
	ingester := newIngesterWith(t, server, 1, 2)

	if err := ingester.ReplaceDocument(ctx, testSource, []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "keep me"},
	}); err != nil {
		t.Fatalf("seed replace: %v", err)
	}

	// fakeOllama fails every input containing "FAIL", so this cannot succeed;
	// the bounded retries are exhausted and the ingest fails before the
	// transaction opens, leaving the seeded document in place.
	err := ingester.ReplaceDocument(ctx, testSource, []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "FAIL to embed"},
	})
	if err == nil {
		t.Fatal("expected retry exhaustion to fail the ingest")
	}

	if !strings.Contains(err.Error(), "attempts") {
		t.Fatalf("error = %q, want it to name the bounded attempts", err)
	}

	want := []string{"Fees|0|keep me"}

	if got := storedChunks(t, ctx); !slices.Equal(got, want) {
		t.Fatalf("stored chunks after retry exhaustion = %v, want %v", got, want)
	}
}

// TestIsUpToDateIntegrationZeroDimension is the regression guard for the
// provenance lookup when no embedding dimension is pinned (the case the ingest
// CLI uses). With no dimension the query's FILTER is TRUE and there is no $5, so
// the bind must supply four arguments; before the fix this failed with
// "expected 4 arguments, got 5" and made every ingest error out.
func TestIsUpToDateIntegrationZeroDimension(t *testing.T) {
	ctx := context.Background()
	requireDatabase(t)

	resetTestDocument(t, ctx)

	ingester := newIngester(t)

	provenance := Provenance{
		ContentHash:   "hash-1",
		EmbedModel:    "fake",
		ChunkerConfig: "size=50,overlap=20",
		IngestedAt:    time.Now().UTC(),
	}

	if err := ingester.ReplaceDocumentWithProvenance(ctx, testSource, []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "first chunk"},
	}, provenance); err != nil {
		t.Fatalf("seed replace: %v", err)
	}

	upToDate, err := ingester.IsUpToDate(ctx, testSource, provenance)
	if err != nil {
		t.Fatalf("IsUpToDate (unpinned dimension): %v", err)
	}

	if !upToDate {
		t.Fatal("unchanged document reported not up to date")
	}

	changed := provenance
	changed.ContentHash = "hash-2"

	upToDate, err = ingester.IsUpToDate(ctx, testSource, changed)
	if err != nil {
		t.Fatalf("IsUpToDate (changed content): %v", err)
	}

	if upToDate {
		t.Fatal("changed document reported up to date")
	}
}

// TestIsUpToDateIntegrationPinnedDimension exercises the $5 branch: a pinned
// dimension is compared, so a document stored at a different width is not up to
// date.
func TestIsUpToDateIntegrationPinnedDimension(t *testing.T) {
	ctx := context.Background()
	requireDatabase(t)

	resetTestDocument(t, ctx)

	ingester := newIngester(t)

	provenance := Provenance{
		ContentHash:   "hash-1",
		EmbedModel:    "fake",
		Dimension:     embeddingDim,
		ChunkerConfig: "size=50,overlap=20",
		IngestedAt:    time.Now().UTC(),
	}

	if err := ingester.ReplaceDocumentWithProvenance(ctx, testSource, []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "first chunk"},
	}, provenance); err != nil {
		t.Fatalf("seed replace: %v", err)
	}

	upToDate, err := ingester.IsUpToDate(ctx, testSource, provenance)
	if err != nil {
		t.Fatalf("IsUpToDate (pinned dimension): %v", err)
	}

	if !upToDate {
		t.Fatal("unchanged pinned-dimension document reported not up to date")
	}

	other := provenance
	other.Dimension = embeddingDim + 1

	upToDate, err = ingester.IsUpToDate(ctx, testSource, other)
	if err != nil {
		t.Fatalf("IsUpToDate (different dimension): %v", err)
	}

	if upToDate {
		t.Fatal("document with a different stored dimension reported up to date")
	}
}

// largeChunks builds count hand-built chunks named "chunk-<index>" so the >5461
// batching path can be exercised without touching the chunker.
func largeChunks(count int) []chunking.Chunk {
	chunks := make([]chunking.Chunk, 0, count)

	for index := 0; index < count; index++ {
		chunks = append(chunks, chunking.Chunk{
			Section: "Fees",
			Index:   index,
			Content: fmt.Sprintf("chunk-%d", index),
		})
	}

	return chunks
}

// TestReplaceDocumentIntegrationBatchesLargeDocument replaces testSource with
// more than 5461 chunks, the previous single-statement ceiling of
// floor(65535/12). Every chunk must be stored exactly once with its chunk_index,
// proving the batched inserts all run on the caller's transaction.
func TestReplaceDocumentIntegrationBatchesLargeDocument(t *testing.T) {
	ctx := context.Background()
	requireDatabase(t)

	resetTestDocument(t, ctx)

	ingester := newIngester(t)

	t.Cleanup(func() {
		resetTestDocument(t, ctx)

		// The large replace leaves thousands of dead tuples with one identical
		// embedding in the HNSW index; vacuum them so later tests' vector searches
		// are not starved before autovacuum runs.
		if _, err := testConn.Exec(ctx, "VACUUM documents"); err != nil {
			t.Errorf("vacuum documents: %v", err)
		}
	})

	total := chunksPerBatch + 1
	chunks := largeChunks(total)

	if err := ingester.ReplaceDocument(ctx, testSource, chunks); err != nil {
		t.Fatalf("replace a %d-chunk document: %v", total, err)
	}

	var stored int
	if err := testConn.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM documents WHERE source = $1",
		testSource,
	).Scan(&stored); err != nil {
		t.Fatalf("count the stored chunks: %v", err)
	}

	if stored != total {
		t.Fatalf("stored rows = %d, want %d", stored, total)
	}

	rows, err := testConn.Query(
		ctx,
		`
		SELECT chunk_index, content
		FROM documents
		WHERE source = $1
		ORDER BY chunk_index
		`,
		testSource,
	)
	if err != nil {
		t.Fatalf("query the batched chunks: %v", err)
	}
	defer rows.Close()

	seen := 0

	for rows.Next() {
		var (
			index   int
			content string
		)

		if err := rows.Scan(&index, &content); err != nil {
			t.Fatalf("scan a batched chunk: %v", err)
		}

		if index != seen {
			t.Fatalf("chunk_index = %d at position %d, want %d", index, seen, seen)
		}

		if want := fmt.Sprintf("chunk-%d", index); content != want {
			t.Fatalf("chunk_index %d content = %q, want %q", index, content, want)
		}

		seen++
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("iterate the batched chunks: %v", err)
	}

	if seen != total {
		t.Fatalf("iterated %d chunks, want %d", seen, total)
	}
}

// TestReplaceDocumentIntegrationLargeReplacementRollsBackOnLaterBatchFailure
// seeds a small document, then replaces it with a >5461-chunk document whose
// last chunk contains a NUL byte. The fake embedder accepts every chunk, so all
// chunks embed successfully and the first batch of chunksPerBatch rows inserts
// inside the replacement transaction. The second batch then fails because
// PostgreSQL rejects 0x00 in a text column (SQLSTATE 22021), so the transaction
// rolls back: the previous document's DELETE is undone and the first batch is
// rolled back, leaving the seeded document unchanged.
func TestReplaceDocumentIntegrationLargeReplacementRollsBackOnLaterBatchFailure(t *testing.T) {
	ctx := context.Background()
	requireDatabase(t)

	resetTestDocument(t, ctx)

	ingester := newIngester(t)

	t.Cleanup(func() {
		resetTestDocument(t, ctx)

		// The large replace leaves thousands of dead tuples with one identical
		// embedding in the HNSW index; vacuum them so later tests' vector searches
		// are not starved before autovacuum runs.
		if _, err := testConn.Exec(ctx, "VACUUM documents"); err != nil {
			t.Errorf("vacuum documents: %v", err)
		}
	})

	if err := ingester.ReplaceDocument(ctx, testSource, []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "keep me"},
	}); err != nil {
		t.Fatalf("seed replace: %v", err)
	}

	chunks := largeChunks(chunksPerBatch + 1)

	// The fake embedder accepts this, so every chunk embeds successfully and
	// the failure comes from the second INSERT batch rejecting the NUL byte.
	chunks[len(chunks)-1].Content = "poison\x00byte"

	err := ingester.ReplaceDocument(ctx, testSource, chunks)
	if err == nil {
		t.Fatal("expected the later insert batch to fail")
	}

	if !strings.Contains(err.Error(), "22021") {
		t.Fatalf("error = %q, want the invalid-byte-sequence SQLSTATE 22021", err)
	}

	want := []string{"Fees|0|keep me"}

	if got := storedChunks(t, ctx); !slices.Equal(got, want) {
		t.Fatalf("stored chunks after a failed later batch = %v, want %v", got, want)
	}
}
