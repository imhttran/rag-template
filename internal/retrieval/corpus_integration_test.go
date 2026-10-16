package retrieval

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Corpus-isolation integration tests (RAG-SOP-002). They require
// RAG_INTEGRATION=1 and DATABASE_URL (see integration_test.go's TestMain) and
// run against the isolated schema. Every test asserts against real pgvector
// rows; none is allowed to pass by skipping.

// insertCorpusDocument stores one chunk carrying corpus and content_hash. An
// empty corpus or hash is stored as NULL (a legacy/unscoped row).
func insertCorpusDocument(
	t *testing.T,
	ctx context.Context,
	conn *pgx.Conn,
	content, source, section, corpus, hash string,
	embedding []float64,
) int64 {
	t.Helper()

	var corpusArg, hashArg any
	if corpus != "" {
		corpusArg = corpus
	}
	if hash != "" {
		hashArg = hash
	}

	var id int64
	err := conn.QueryRow(
		ctx,
		`INSERT INTO documents (content, source, section, chunk_index, embedding, corpus, content_hash)
		 VALUES ($1, $2, $3, $4, $5::vector, $6, $7) RETURNING id`,
		content, source, section, 0, VectorToString(embedding), corpusArg, hashArg,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert corpus document: %v", err)
	}
	return id
}

// seedCorpus inserts two scoped rows (corpus "kb"), one other-corpus row
// ("other"), and one legacy NULL-corpus row, all matching the same query.
func seedCorpus(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	resetDocuments(t, ctx, conn)
	v := unitVector(0)
	insertCorpusDocument(t, ctx, conn, "retrieval fallback architecture decision", "a.md", "S", "kb", "hashA", v)
	insertCorpusDocument(t, ctx, conn, "retrieval fallback design", "b.md", "S", "kb", "hashB", v)
	insertCorpusDocument(t, ctx, conn, "retrieval fallback other", "c.md", "S", "other", "hashC", v)
	insertCorpusDocument(t, ctx, conn, "retrieval fallback legacy", "d.md", "S", "", "", v)
}

// TestCorpusVectorIsolation: scoped vector search returns only the requested
// corpus, never another corpus or a NULL-corpus (legacy) row, and projects the
// content hash and corpus.
func TestCorpusVectorIsolation(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)
	seedCorpus(t, ctx, conn)

	docs, err := New(conn).SearchFiltered(ctx, unitVector(0), 10, Filter{Corpus: "kb"})
	if err != nil {
		t.Fatalf("scoped vector search: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("scoped vector docs = %d, want 2 (isolation failure)", len(docs))
	}
	for _, d := range docs {
		if d.Corpus != "kb" {
			t.Fatalf("cross-corpus leak: got corpus %q", d.Corpus)
		}
		if d.ContentHash == "" {
			t.Fatalf("content_hash not projected for %s", d.Source)
		}
	}

	// Unscoped (migration compatibility): a zero filter still sees legacy rows.
	all, err := New(conn).SearchFiltered(ctx, unitVector(0), 10, Filter{})
	if err != nil {
		t.Fatalf("unscoped vector search: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("unscoped vector docs = %d, want 4", len(all))
	}
}

// TestCorpusKeywordIsolation: scoped keyword (full-text) search is isolated.
func TestCorpusKeywordIsolation(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)
	seedCorpus(t, ctx, conn)

	docs, err := New(conn).KeywordSearchFiltered(ctx, "retrieval fallback", 10, "english", Filter{Corpus: "kb"})
	if err != nil {
		t.Fatalf("scoped keyword search: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("scoped keyword docs = %d, want 2 (isolation failure)", len(docs))
	}
	for _, d := range docs {
		if d.Corpus != "kb" {
			t.Fatalf("cross-corpus leak in keyword path: corpus %q", d.Corpus)
		}
	}
}

// TestCorpusSectionExpansionIsolation: section expansion is filtered too.
func TestCorpusSectionExpansionIsolation(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)
	resetDocuments(t, ctx, conn)
	v := unitVector(0)
	insertChunks2 := func(source, corpus string) {
		insertCorpusDocument(t, ctx, conn, source+" chunk 0", source, "S", corpus, "h", v)
		insertCorpusDocument(t, ctx, conn, source+" chunk 1", source, "S", corpus, "h", v)
	}
	insertChunks2("kb.md", "kb")
	insertChunks2("other.md", "other")

	keys := []SectionKey{{Source: "kb.md", Section: "S"}, {Source: "other.md", Section: "S"}}
	docs, err := New(conn).SectionChunksFiltered(ctx, keys, 10, Filter{Corpus: "kb"})
	if err != nil {
		t.Fatalf("scoped section expansion: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("expected kb chunks")
	}
	for _, d := range docs {
		if d.Corpus != "kb" {
			t.Fatalf("cross-corpus leak in section expansion: %q", d.Corpus)
		}
	}
}

// TestCorpusStableRanking: identical input+corpus yields identical ids.
func TestCorpusStableRanking(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)
	seedCorpus(t, ctx, conn)

	r := New(conn)
	first, err := r.SearchFiltered(ctx, unitVector(0), 10, Filter{Corpus: "kb"})
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	second, err := r.SearchFiltered(ctx, unitVector(0), 10, Filter{Corpus: "kb"})
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("length drift: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("ranking not stable at %d: %d vs %d", i, first[i].ID, second[i].ID)
		}
	}
}

// TestCorpusMigrationCompatibility: the corpus column exists, is nullable, and
// legacy NULL rows are excluded from scoped retrieval but visible unscoped.
func TestCorpusMigrationCompatibility(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	var nullable string
	if err := conn.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns WHERE table_name='documents' AND column_name='corpus'`).Scan(&nullable); err != nil {
		t.Fatalf("corpus column missing (migration 005 not applied): %v", err)
	}
	if nullable != "YES" {
		t.Fatalf("corpus column is_nullable = %q, want YES", nullable)
	}
}
