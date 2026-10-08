// End-to-end integration test for a non-768 (dimension 3) embedding provider in
// an isolated PostgreSQL schema.
//
// It proves three things the default 768 test suite does not:
//
//  1. A dimension-3 embedding.Embedder drives ingestion and retrieval end to
//     end against a documents table whose embedding column is vector(3).
//  2. The default schema's documents.embedding stays vector(768): the test
//     provisions its own schema and never touches public.
//  3. A dimension mismatch is rejected before any destructive write, leaving
//     previously stored rows byte-for-byte unchanged.
//
// Isolation works by creating a uniquely named schema, applying every
// migrations/*.sql into it, and pointing a dedicated connection at it with
// SET search_path. schema.go's embeddingDimQuery resolves documents via
// current_schemas(false)/current_schema(), and the runtime SQL uses unqualified
// documents, so search_path-based isolation is honoured throughout.
//
// The file reuses the package TestMain + applyMigrations harness from
// integration_test.go and is skipped unless RAG_INTEGRATION=1 is set:
//
//	RAG_INTEGRATION=1 go test ./internal/ingestion/ -run SchemaE2E -p 1
package ingestion

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"rag-template/internal/chunking"
	"rag-template/internal/config"
	"rag-template/internal/retrieval"
)

// schemaE2EDim is the non-default embedding dimension this test proves.
const schemaE2EDim = 3

// schemaE2EDefaultDim is the dimension migrations/001_init.sql declares for the
// default schema's documents.embedding column.
const schemaE2EDefaultDim = 768

// schemaE2ESource is the source this file owns in the isolated, non-768 schema.
const schemaE2ESource = "schema-e2e-dim3.md"

// schemaE2EDim3Embedder is an in-memory embedding.Embedder returning a fixed
// dimension-3 vector. It keeps the non-768 path free of any HTTP or vendor
// dependency, matching the stub-provider seam proven by
// provider_pipeline_integration_test.go.
type schemaE2EDim3Embedder struct {
	vector []float64
}

// Embed returns the fixed dimension-3 stub vector, ignoring the input text.
func (e schemaE2EDim3Embedder) Embed(
	_ context.Context,
	_ string,
) ([]float64, error) {
	return e.vector, nil
}

// schemaE2EDim3Vector is a deterministic, non-trivial dimension-3 vector. Every
// component is non-zero so cosine similarity is well defined and equals 1 when
// the same vector round-trips through pgvector.
func schemaE2EDim3Vector() []float64 {
	return []float64{0.25, 0.5, 0.75}
}

// schemaE2EConfig is the runtime config the isolated test resolves once: it
// reuses config.Load() so DATABASE_URL parsing matches openTestDatabase without
// duplicating the environment contract.
func schemaE2EConfig(t *testing.T) config.Config {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	return cfg
}

// openSchemaE2EConn opens a dedicated connection and points it at a freshly
// created schema named schemaName, into which every migrations/*.sql file is
// applied. The returned cleanup drops the schema and closes the connection.
//
// A dedicated connection is used because search_path is per-session: setting it
// on testConn would leak the isolated schema into the other integration tests.
func openSchemaE2EConn(
	t *testing.T,
	ctx context.Context,
	schemaName string,
) *pgx.Conn {
	t.Helper()

	cfg := schemaE2EConfig(t)

	conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("connect to the isolated schema database: %v", err)
	}

	// The schema itself is dropped by the cleanup below; CREATE SCHEMA has no
	// IF NOT EXISTS because a collision would mean two tests raced, which the
	// name's uniqueness is meant to prevent.
	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		_ = conn.Close(ctx)

		t.Fatalf("create isolated schema %s: %v", schemaName, err)
	}

	t.Cleanup(func() {
		if _, err := conn.Exec(
			ctx,
			fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName),
		); err != nil {
			t.Errorf("drop isolated schema %s: %v", schemaName, err)
		}

		if err := conn.Close(ctx); err != nil {
			t.Errorf("close isolated schema connection: %v", err)
		}
	})

	if err := applyMigrationsToSchema(ctx, conn, schemaName); err != nil {
		t.Fatalf("apply migrations to isolated schema %s: %v", schemaName, err)
	}

	// public is appended so the pgvector `vector` type (installed there) stays
	// visible, while the isolated schema remains first so the unqualified
	// documents table still lands in schemaName.
	if _, err := conn.Exec(
		ctx,
		fmt.Sprintf("SET search_path TO %s, public", schemaName),
	); err != nil {
		t.Fatalf("set search_path to %s: %v", schemaName, err)
	}

	return conn
}

// applyMigrationsToSchema runs every migrations/*.sql file in name order on
// conn with search_path already pointed at schemaName, so the unqualified
// documents table the migrations create lands in the isolated schema. It
// mirrors applyMigrations from integration_test.go without touching the shared
// harness.
func applyMigrationsToSchema(
	ctx context.Context,
	conn *pgx.Conn,
	schemaName string,
) error {
	// Keep the isolated schema first so the unqualified documents table lands
	// there, and append public so the pgvector `vector` type stays visible.
	if _, err := conn.Exec(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		return fmt.Errorf("set search_path to %s: %w", schemaName, err)
	}

	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}

	slices.Sort(paths)

	for _, path := range paths {
		migration, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", path, err)
		}

		// Apply the migration with the default embedding dimension replaced by the
		// isolated test dimension, so documents.embedding is vector(schemaE2EDim)
		// from the start. The production migrations are unchanged.
		sql := strings.ReplaceAll(
			string(migration),
			fmt.Sprintf("vector(%d)", schemaE2EDefaultDim),
			fmt.Sprintf("vector(%d)", schemaE2EDim),
		)

		if _, err := conn.Exec(ctx, sql); err != nil {
			return fmt.Errorf("apply migration %s: %w", path, err)
		}
	}

	return nil
}

// resetSchemaE2EDocument removes the rows this file owns in the isolated
// schema selected by conn's search_path.
func resetSchemaE2EDocument(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()

	if _, err := conn.Exec(
		ctx,
		"DELETE FROM documents WHERE source = $1",
		schemaE2ESource,
	); err != nil {
		t.Fatalf("delete the isolated test document: %v", err)
	}
}

// schemaE2EStoredChunks returns the stored chunks of schemaE2ESource in the
// isolated schema selected by conn's search_path, as "section|index|content".
func schemaE2EStoredChunks(
	t *testing.T,
	ctx context.Context,
	conn *pgx.Conn,
) []string {
	t.Helper()

	rows, err := conn.Query(
		ctx,
		`
		SELECT section, chunk_index, content
		FROM documents
		WHERE source = $1
		ORDER BY section, chunk_index
		`,
		schemaE2ESource,
	)
	if err != nil {
		t.Fatalf("query the isolated stored chunks: %v", err)
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
			t.Fatalf("scan an isolated stored chunk: %v", err)
		}

		chunks = append(chunks, fmt.Sprintf("%s|%d|%s", section, index, content))
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("iterate the isolated stored chunks: %v", err)
	}

	return chunks
}

// schemaE2EStoredColumnDim reads the declared dimension of documents.embedding
// in the schema selected by conn's search_path directly from the catalog, so
// the assertion does not depend on CheckEmbeddingDim's own behavior.
func schemaE2EStoredColumnDim(
	t *testing.T,
	ctx context.Context,
	conn *pgx.Conn,
) int {
	t.Helper()

	var dim int

	if err := conn.QueryRow(ctx, `
		SELECT a.atttypmod
		FROM pg_attribute AS a
		WHERE a.attrelid = (
			SELECT c.oid
			FROM pg_class AS c
			JOIN pg_namespace AS n ON n.oid = c.relnamespace
			WHERE c.relname = 'documents'
			  AND n.nspname = ANY (current_schemas(false))
			ORDER BY array_position(current_schemas(false), n.nspname)
			LIMIT 1
		)
		  AND a.attname = 'embedding'
		  AND NOT a.attisdropped
	`).Scan(&dim); err != nil {
		t.Fatalf("read the isolated documents.embedding dimension: %v", err)
	}

	return dim
}

// TestSchemaE2EDimension3Integration proves a dimension-3 provider drives
// ingestion and retrieval end to end in an isolated schema, that the isolated
// column really is vector(3), and that a dimension mismatch fails before any
// destructive write.
func TestSchemaE2EDimension3Integration(t *testing.T) {
	if os.Getenv("RAG_INTEGRATION") == "" {
		t.Skip("set RAG_INTEGRATION=1 (with the database up) to run the isolated schema e2e test")
	}

	ctx := context.Background()

	conn := openSchemaE2EConn(t, ctx, "rag_005c_dim3")
	resetSchemaE2EDocument(t, ctx, conn)

	// The isolated schema's documents.embedding must be exactly dimension 3.
	if got := schemaE2EStoredColumnDim(t, ctx, conn); got != schemaE2EDim {
		t.Fatalf("isolated documents.embedding dimension = %d, want %d", got, schemaE2EDim)
	}

	if err := CheckEmbeddingDim(ctx, conn, schemaE2EDim); err != nil {
		t.Fatalf("CheckEmbeddingDim(3) against the isolated schema = %v, want nil", err)
	}

	vector := schemaE2EDim3Vector()
	embedder := schemaE2EDim3Embedder{vector: vector}
	ingester := New(conn, embedder)

	chunks := []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "dimension three first chunk"},
		{Section: "Fees", Index: 1, Content: "dimension three second chunk"},
		{Section: "Refunds", Index: 0, Content: "dimension three third chunk"},
	}

	if err := ingester.ReplaceDocument(ctx, schemaE2ESource, chunks); err != nil {
		t.Fatalf("replace document with a dimension-3 provider: %v", err)
	}

	want := []string{
		"Fees|0|dimension three first chunk",
		"Fees|1|dimension three second chunk",
		"Refunds|0|dimension three third chunk",
	}

	if got := schemaE2EStoredChunks(t, ctx, conn); !slices.Equal(got, want) {
		t.Fatalf("isolated stored chunks = %v, want %v", got, want)
	}

	// The dimension-3 vector must round-trip through the $5::vector cast and be
	// retrieved with cosine similarity 1.
	documents, err := retrieval.New(conn).Search(ctx, vector, 1)
	if err != nil {
		t.Fatalf("search the isolated schema with the dimension-3 vector: %v", err)
	}

	if len(documents) == 0 {
		t.Fatal("expected the dimension-3 document to be retrievable")
	}

	got := documents[0]
	if got.Source != schemaE2ESource {
		t.Fatalf("retrieved source = %q, want %q", got.Source, schemaE2ESource)
	}

	if math.Abs(got.Similarity-1) > 1e-6 {
		t.Fatalf("expected cosine similarity 1 for the round-tripped dimension-3 vector, got %v", got.Similarity)
	}
}

// TestSchemaE2EMismatchBeforeWriteIntegration proves a dimension mismatch is
// rejected before any destructive write: the guard fires at the wrong dimension
// with an actionable message, an ingestion whose vector width conflicts with
// the isolated column fails, and the previously seeded rows are unchanged.
func TestSchemaE2EMismatchBeforeWriteIntegration(t *testing.T) {
	if os.Getenv("RAG_INTEGRATION") == "" {
		t.Skip("set RAG_INTEGRATION=1 (with the database up) to run the isolated schema e2e test")
	}

	ctx := context.Background()

	conn := openSchemaE2EConn(t, ctx, "rag_005c_mismatch")
	resetSchemaE2EDocument(t, ctx, conn)

	// Seed through the dimension-3 path.
	vector := schemaE2EDim3Vector()
	ingester := New(conn, schemaE2EDim3Embedder{vector: vector})

	if err := ingester.ReplaceDocument(ctx, schemaE2ESource, []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "keep me"},
	}); err != nil {
		t.Fatalf("seed the isolated document: %v", err)
	}

	seeded := schemaE2EStoredChunks(t, ctx, conn)

	// A guard at the wrong dimension must fail fast with an actionable message
	// and perform no write.
	err := CheckEmbeddingDim(ctx, conn, 1024)
	if err == nil {
		t.Fatal("CheckEmbeddingDim(1024) = nil, want a mismatch error")
	}

	for _, want := range []string{"1024", "3", migrationFile, "re-ingest"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("guard error = %q, want it to contain %q", err, want)
		}
	}

	// An ingestion whose vector width conflicts with the isolated vector(3)
	// column must fail, and the transaction must roll back the DELETE+INSERT so
	// the seeded rows survive byte-for-byte.
	badEmbedder := schemaE2EMismatchEmbedder{}
	badErr := New(conn, badEmbedder).ReplaceDocument(ctx, schemaE2ESource, []chunking.Chunk{
		{Section: "Fees", Index: 0, Content: "should not land"},
	})
	if badErr == nil {
		t.Fatal("expected the mismatched ingestion to fail")
	}

	if got := schemaE2EStoredChunks(t, ctx, conn); !slices.Equal(got, seeded) {
		t.Fatalf("seeded chunks after the mismatched attempt = %v, want %v", got, seeded)
	}
}

// schemaE2EMismatchEmbedder returns a vector wider than the isolated vector(3)
// column, so the insert's $N::vector cast fails inside the transaction and the
// deferred rollback restores the previous rows.
type schemaE2EMismatchEmbedder struct{}

// Embed returns a 4-dimensional vector against the isolated vector(3) column.
func (schemaE2EMismatchEmbedder) Embed(
	_ context.Context,
	_ string,
) ([]float64, error) {
	return []float64{0.1, 0.2, 0.3, 0.4}, nil
}

// TestSchemaE2EDefaultDimUnchangedIntegration proves the isolated dimension-3
// run left the default public schema's documents.embedding at vector(768): the
// guard passes at 768 against the shared connection and the catalog reports 768
// there.
func TestSchemaE2EDefaultDimUnchangedIntegration(t *testing.T) {
	ctx := context.Background()
	requireDatabase(t)

	if err := CheckEmbeddingDim(ctx, testConn, schemaE2EDefaultDim); err != nil {
		t.Fatalf("CheckEmbeddingDim(768) against the default schema = %v, want nil", err)
	}

	if got := schemaE2EStoredColumnDim(t, ctx, testConn); got != schemaE2EDefaultDim {
		t.Fatalf("default documents.embedding dimension = %d, want %d", got, schemaE2EDefaultDim)
	}

	// The isolated schema's source must not have leaked into the default table.
	var count int

	if err := testConn.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM documents WHERE source = $1",
		schemaE2ESource,
	).Scan(&count); err != nil {
		t.Fatalf("count the isolated source in the default schema: %v", err)
	}

	if count != 0 {
		t.Fatalf("isolated source %q has %d rows in the default schema, want 0", schemaE2ESource, count)
	}
}
