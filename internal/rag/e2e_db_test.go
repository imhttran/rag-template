package rag

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"rag-template/internal/chunking"
	"rag-template/internal/document"
	"rag-template/internal/ingestion"
	"rag-template/internal/retrieval"
)

// This file holds the RAG-025B end-to-end integration test. It is gated behind
// RAG_INTEGRATION=1 and a reachable *disposable* PostgreSQL+pgvector database,
// and it skips (never fails) when either is absent, so `go test ./...` stays
// green on a machine with no database.
//
// It exercises the real production path — migrations, ingestion (with a
// deterministic, network-free stub embedder), the production retrieval pipeline,
// and the production answer path (with a deterministic stub generator) — against
// a disposable database. It never touches the production `rag_db`, and it refuses
// to run if the configured database is named `rag_db`.
//
// The disposable database URL is read from RAG_TEST_DATABASE_URL (falling back to
// DATABASE_URL only when it is unset), matching the disposable-PostgreSQL harness
// documented in docs/guides/testing.md. The safety guard refuses `rag_db` before
// any migration or mutation runs, regardless of which variable supplied the URL.
//
// The in-memory tests in e2e_test.go remain as unit-level coverage.

// dbEmbeddingDim matches the vector(768) column in migrations/001_init.sql.
const dbEmbeddingDim = 768

// dbStubEmbedder is a deterministic, network-free embedding.Embedder used by the
// integration test. It returns a fixed-length, non-zero vector derived from the
// input text, so identical inputs always yield identical vectors and cosine
// distance stays well defined, with no I/O and no external model.
type dbStubEmbedder struct {
	dim int
}

func (s dbStubEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	vector := make([]float64, s.dim)

	for i := range vector {
		vector[i] = float64((len(text)+i)%7+1) / 7.0
	}

	return vector, nil
}

// integrationDatabaseURL resolves the disposable database URL. It prefers
// RAG_TEST_DATABASE_URL (the disposable-PostgreSQL harness variable documented in
// docs/guides/testing.md) and falls back to DATABASE_URL so an explicit dev-style
// override still works. An unset or blank value is reported as absent so the test
// can skip cleanly.
func integrationDatabaseURL() (string, bool) {
	if raw := strings.TrimSpace(os.Getenv("RAG_TEST_DATABASE_URL")); raw != "" {
		return raw, true
	}

	if raw := strings.TrimSpace(os.Getenv("DATABASE_URL")); raw != "" {
		return raw, true
	}

	return "", false
}

// requireIntegrationDB skips unless RAG_INTEGRATION=1, refuses the production
// database, then connects and applies the migrations. The returned connection is
// closed via t.Cleanup.
func requireIntegrationDB(t *testing.T) *pgx.Conn {
	t.Helper()

	if os.Getenv("RAG_INTEGRATION") == "" {
		t.Skip(
			"set RAG_INTEGRATION=1 with a disposable PostgreSQL database to run " +
				"the RAG-025B integration test",
		)
	}

	databaseURL, ok := integrationDatabaseURL()
	if !ok {
		t.Skip(
			"set RAG_TEST_DATABASE_URL to a disposable PostgreSQL database to run " +
				"the RAG-025B integration test",
		)
	}

	if name := databaseName(databaseURL); name == "rag_db" {
		t.Fatalf(
			"refusing to run the destructive integration test against the "+
				"production database %q; point RAG_TEST_DATABASE_URL at a disposable "+
				"database (for example rag_test)",
			name,
		)
	}

	connectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(connectCtx, databaseURL)
	if err != nil {
		t.Skipf("disposable database unreachable at %s: %v", redactDatabaseURL(databaseURL), err)
	}

	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	applyMigrations(t, conn)

	return conn
}

// databaseName extracts the database name from a connection URL.
func databaseName(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	return strings.TrimPrefix(parsed.Path, "/")
}

// redactDatabaseURL removes any password from a connection URL so it is safe to
// include in a test message.
func redactDatabaseURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "<unparseable>"
	}

	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(parsed.User.Username(), "REDACTED")
		}
	}

	return parsed.String()
}

// applyMigrations runs every migrations/*.sql in name order, idempotently, so a
// fresh disposable database works and later migrations are picked up.
func applyMigrations(t *testing.T, conn *pgx.Conn) {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}

	slices.Sort(paths)

	for _, path := range paths {
		migration, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", path, err)
		}

		if _, err := conn.Exec(context.Background(), string(migration)); err != nil {
			t.Fatalf("apply migration %s: %v", path, err)
		}
	}
}

// TestE2EIngestRetrieveGenerateDBIntegration proves the full
// ingest -> retrieve -> generate path over a disposable PostgreSQL/pgvector
// database, using deterministic network-free stubs and production components.
func TestE2EIngestRetrieveGenerateDBIntegration(t *testing.T) {
	ctx := context.Background()
	conn := requireIntegrationDB(t)

	const source = "rag025b-e2e.md"

	cleanup := func() {
		if _, err := conn.Exec(ctx, "DELETE FROM documents WHERE source = $1", source); err != nil {
			t.Errorf("cleanup %s: %v", source, err)
		}
	}

	cleanup()
	t.Cleanup(cleanup)

	// 3. Ingest a small synthetic document with a deterministic stub embedder.
	ingester := ingestion.NewWithOptions(conn, dbStubEmbedder{dim: dbEmbeddingDim}, 2, 1)
	sections := []document.Section{{
		Title:   "Late Fees",
		Content: "A late fee of 25 dollars may be charged after the grace period has expired.",
	}}
	chunks := chunking.FromSections(sections, 50, 20)

	if err := ingester.ReplaceDocument(ctx, source, chunks); err != nil {
		t.Fatalf("ingest %s: %v", source, err)
	}

	// 4. Verify the document rows and embeddings were persisted (pgvector).
	var rows, embedded int

	if err := conn.QueryRow(
		ctx,
		"SELECT count(*), count(embedding) FROM documents WHERE source = $1",
		source,
	).Scan(&rows, &embedded); err != nil {
		t.Fatalf("count stored rows: %v", err)
	}

	if rows == 0 {
		t.Fatalf("no rows stored for %s", source)
	}

	if embedded != rows {
		t.Fatalf("stored rows=%d but embedded=%d; pgvector column not populated", rows, embedded)
	}

	// 5. Retrieve through the production retrieval pipeline.
	queryVector, err := dbStubEmbedder{dim: dbEmbeddingDim}.Embed(ctx, "late fee")
	if err != nil {
		t.Fatalf("embed query: %v", err)
	}

	result, err := retrieval.HybridRetrieve(
		ctx,
		retrieval.New(conn),
		"what is the late fee",
		queryVector,
		retrieval.PipelineOptions{CandidateK: 4, FinalK: 3, ExpandLimit: 5, MinSimilarity: 0},
	)
	if err != nil {
		t.Fatalf("hybrid retrieve: %v", err)
	}

	if len(result.Fused) == 0 {
		t.Fatal("retrieval returned no fused documents from the ingested corpus")
	}

	foundSource := false

	var retrieved retrieval.Document

	for _, doc := range result.Fused {
		if doc.Source == source {
			foundSource = true
			retrieved = doc

			break
		}
	}

	if !foundSource {
		t.Fatalf("expected source %q in the fused results, got %d documents", source, len(result.Fused))
	}

	// 5b. Provenance: the retrieved source/section survives into the assembled
	// context the model is shown.
	assembled := FormatContext([]retrieval.Document{retrieved})

	if !strings.Contains(assembled, "Source: "+source) {
		t.Fatalf("assembled context missing provenance source %q: %q", source, assembled)
	}

	if !strings.Contains(assembled, "Section: ") {
		t.Fatalf("assembled context missing provenance section: %q", assembled)
	}

	// 6. Generate an answer with a deterministic stub generator via the production
	// answer path (no live model, no network).
	stub := stubGenerator{answer: fmt.Sprintf("The late fee is 25 dollars. [%s - Late Fees]", source)}

	answer, err := Answer(ctx, stub, "what is the late fee", result.Fused)
	if err != nil {
		t.Fatalf("answer: %v", err)
	}

	// 7. Verify the expected answer and the source reference (provenance
	// recorded on the generated answer).
	if !strings.Contains(answer, "25 dollars") {
		t.Fatalf("answer = %q, want it to contain the expected fee", answer)
	}

	if !strings.Contains(answer, source) {
		t.Fatalf("answer = %q, want the source reference %q", answer, source)
	}
}

// TestE2EIngestReplacementIsAtomicDBIntegration proves the ingest step is
// atomic: a replacement that fails on a later chunk leaves the previously stored
// document unchanged (the delete-then-insert replacement rolls back).
func TestE2EIngestReplacementIsAtomicDBIntegration(t *testing.T) {
	ctx := context.Background()
	conn := requireIntegrationDB(t)

	const source = "rag025b-atomic.md"

	cleanup := func() {
		if _, err := conn.Exec(ctx, "DELETE FROM documents WHERE source = $1", source); err != nil {
			t.Errorf("cleanup %s: %v", source, err)
		}
	}

	cleanup()
	t.Cleanup(cleanup)

	ingester := ingestion.NewWithOptions(conn, dbStubEmbedder{dim: dbEmbeddingDim}, 2, 1)

	// Seed a good document.
	if err := ingester.ReplaceDocument(ctx, source, []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "keep me"},
	}); err != nil {
		t.Fatalf("seed replace: %v", err)
	}

	// A replacement whose last chunk cannot be stored (NUL byte) must fail.
	err := ingester.ReplaceDocument(ctx, source, []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "new one"},
		{Section: "Fees", Index: 1, Content: "poison\x00byte"},
	})
	if err == nil {
		t.Fatal("expected the replacement to fail on an unstorable chunk")
	}

	// The seeded document must be unchanged (atomic rollback).
	var stored int
	if err := conn.QueryRow(
		ctx,
		"SELECT count(*) FROM documents WHERE source = $1 AND content = 'keep me'",
		source,
	).Scan(&stored); err != nil {
		t.Fatalf("count after failed replace: %v", err)
	}

	if stored != 1 {
		t.Fatalf("after a failed replacement the seed row count = %d, want 1 (atomic rollback)", stored)
	}
}
