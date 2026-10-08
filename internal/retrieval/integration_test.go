// PostgreSQL integration tests for the pgvector-backed queries.
//
// They are skipped unless RAG_INTEGRATION=1 is set, so `go test ./...` (and the
// pre-commit hook) stays fast. When the variable is set, TestMain connects to
// DATABASE_URL and applies every migration in migrations/, so the tests run against
// the docker-compose database (`make db-up`) and share one connection.
//
//	RAG_INTEGRATION=1 go test ./internal/retrieval/ -run Integration -v
//
// The tests truncate the documents table, so point DATABASE_URL at a throwaway
// database.

package retrieval

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"rag-template/internal/config"
)

// embeddingDim matches the vector(768) column in migrations/001_init.sql.
const embeddingDim = 768

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

// requireDatabase skips a test unless TestMain started a container.
func requireDatabase(t *testing.T) *pgx.Conn {
	t.Helper()

	if testConn == nil {
		t.Skip(
			"set RAG_INTEGRATION=1 (with Docker running) to run the " +
				"PostgreSQL integration tests",
		)
	}

	return testConn
}

// resetDocuments empties the table so a test sees only its own rows; the
// identity sequence restarts so inserted IDs are predictable per test.
func resetDocuments(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()

	if _, err := conn.Exec(ctx, "TRUNCATE documents RESTART IDENTITY"); err != nil {
		t.Fatalf("truncate documents: %v", err)
	}
}

// unitVector builds a 768-dimensional unit vector holding 1 at each index. One
// index is a basis vector, so two basis vectors are 1.0 apart from themselves
// and 0.0 from each other; two indexes sit at 45 degrees from either basis.
func unitVector(indexes ...int) []float64 {
	values := make([]float64, embeddingDim)

	for _, index := range indexes {
		values[index] = 1
	}

	var squared float64

	for _, value := range values {
		squared += value * value
	}

	scale := 1 / math.Sqrt(squared)

	for i := range values {
		values[i] *= scale
	}

	return values
}

// insertDocument stores one chunk and returns its generated ID.
func insertDocument(
	t *testing.T,
	ctx context.Context,
	conn *pgx.Conn,
	document Document,
	embedding []float64,
) int64 {
	t.Helper()

	var id int64

	err := conn.QueryRow(
		ctx,
		`
		INSERT INTO documents (content, source, section, chunk_index, embedding)
		VALUES ($1, $2, $3, $4, $5::vector)
		RETURNING id
		`,
		document.Content,
		document.Source,
		document.Section,
		document.ChunkIndex,
		VectorToString(embedding),
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert document: %v", err)
	}

	return id
}

// insertChunks stores count chunks of one section and returns their IDs in
// chunk order.
func insertChunks(
	t *testing.T,
	ctx context.Context,
	conn *pgx.Conn,
	source string,
	section string,
	count int,
	embedding []float64,
) []int64 {
	t.Helper()

	inserted := make([]int64, 0, count)

	for index := range count {
		inserted = append(inserted, insertDocument(t, ctx, conn, Document{
			Source:     source,
			Section:    section,
			ChunkIndex: index,
			Content:    fmt.Sprintf("%s chunk %d", section, index),
		}, embedding))
	}

	return inserted
}

func TestSearchIntegrationOrdersBySimilarity(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	resetDocuments(t, ctx, conn)

	query := unitVector(0)

	// Cosine similarity against query is 1.0, cos(45°) and 0.0 respectively.
	identical := insertDocument(t, ctx, conn, Document{
		Source:  "loan-policy.md",
		Section: "Missed Payments",
		Content: "the loan enters a 10-day grace period",
	}, query)

	tilted := insertDocument(t, ctx, conn, Document{
		Source:  "large-loan-policy.md",
		Section: "Missed Payments",
		Content: "the grace period lasts fifteen days",
	}, unitVector(0, 1))

	orthogonal := insertDocument(t, ctx, conn, Document{
		Source:  "large-loan-policy.md",
		Section: "Refunds",
		Content: "refunds are issued within thirty days",
	}, unitVector(1))

	retriever := New(conn)

	documents, err := retriever.Search(ctx, query, 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	wantIDs := []int64{identical, tilted, orthogonal}
	if got := ids(documents); !slices.Equal(got, wantIDs) {
		t.Fatalf("expected similarity order %v, got %v", wantIDs, got)
	}

	wantSimilarity := []float64{1, 1 / math.Sqrt2, 0}

	for i, want := range wantSimilarity {
		if got := documents[i].Similarity; math.Abs(got-want) > 1e-6 {
			t.Fatalf(
				"document %d: expected similarity %v, got %v",
				documents[i].ID,
				want,
				got,
			)
		}
	}

	// The scan must line up with the SELECT list: check the fields of a row that
	// is not the first.
	if documents[1].Source != "large-loan-policy.md" ||
		documents[1].Section != "Missed Payments" ||
		documents[1].ChunkIndex != 0 ||
		documents[1].Content != "the grace period lasts fifteen days" {
		t.Fatalf("scanned fields do not match the inserted row: %+v", documents[1])
	}

	limited, err := retriever.Search(ctx, query, 1)
	if err != nil {
		t.Fatalf("search with topK=1: %v", err)
	}

	wantLimited := []int64{identical}
	if got := ids(limited); !slices.Equal(got, wantLimited) {
		t.Fatalf("expected only the closest document, got %v", got)
	}
}

func TestKeywordSearchIntegrationRanksByTextRank(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	resetDocuments(t, ctx, conn)

	embedding := unitVector(0)

	grace := insertDocument(t, ctx, conn, Document{
		Source:  "loan-policy.md",
		Section: "Missed Payments",
		Content: "if a borrower misses a payment the loan enters a 10-day " +
			"grace period",
	}, embedding)

	fees := insertDocument(t, ctx, conn, Document{
		Source:  "loan-policy.md",
		Section: "Fees",
		Content: "a late fee is charged when a payment is overdue",
	}, embedding)

	refunds := insertDocument(t, ctx, conn, Document{
		Source:  "loan-policy.md",
		Section: "Refunds",
		Content: "refunds are issued within thirty days of a written request",
	}, embedding)

	retriever := New(conn)

	// Terms are ANDed, so only the section holding both words matches.
	documents, err := retriever.KeywordSearch(ctx, "grace period", 4)
	if err != nil {
		t.Fatalf("keyword search: %v", err)
	}

	wantIDs := []int64{grace}
	if got := ids(documents); !slices.Equal(got, wantIDs) {
		t.Fatalf("expected only the grace period chunk, got %v", got)
	}

	if documents[0].KeywordScore <= 0 {
		t.Fatalf(
			"expected a positive keyword score, got %v",
			documents[0].KeywordScore,
		)
	}

	if documents[0].Section != "Missed Payments" || documents[0].Content == "" {
		t.Fatalf("scanned fields do not match the inserted row: %+v", documents[0])
	}

	// A shared term returns every matching chunk and nothing else.
	documents, err = retriever.KeywordSearch(ctx, "payment", 4)
	if err != nil {
		t.Fatalf("keyword search for payment: %v", err)
	}

	got := ids(documents)

	if len(got) != 2 {
		t.Fatalf("expected two payment chunks, got %v", got)
	}

	if !slices.Contains(got, grace) || !slices.Contains(got, fees) {
		t.Fatalf("expected chunks %d and %d, got %v", grace, fees, got)
	}

	if slices.Contains(got, refunds) {
		t.Fatalf("the refund chunk should not match 'payment': %v", got)
	}

	for _, document := range documents {
		if document.KeywordScore <= 0 {
			t.Fatalf(
				"document %d: expected a positive keyword score, got %v",
				document.ID,
				document.KeywordScore,
			)
		}
	}
}

func TestSectionChunksIntegrationReturnsOrderedChunks(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	resetDocuments(t, ctx, conn)

	embedding := unitVector(0)

	missed := insertChunks(t, ctx, conn, "a.md", "Missed Payments", 3, embedding)
	fees := insertChunks(t, ctx, conn, "a.md", "Fees", 1, embedding)
	otherFees := insertChunks(t, ctx, conn, "b.md", "Fees", 2, embedding)

	retriever := New(conn)

	documents, err := retriever.SectionChunks(ctx, []SectionKey{
		{Source: "a.md", Section: "Missed Payments"},
	}, 20)
	if err != nil {
		t.Fatalf("section chunks: %v", err)
	}

	if got := ids(documents); !slices.Equal(got, missed) {
		t.Fatalf("expected every chunk of the section %v, got %v", missed, got)
	}

	for i, document := range documents {
		if document.ChunkIndex != i {
			t.Fatalf(
				"expected chunk index %d at position %d, got %d",
				i,
				i,
				document.ChunkIndex,
			)
		}
	}

	// Keys are ORed together and ordered by source, then section, then chunk.
	documents, err = retriever.SectionChunks(ctx, []SectionKey{
		{Source: "a.md", Section: "Missed Payments"},
		{Source: "b.md", Section: "Fees"},
	}, 20)
	if err != nil {
		t.Fatalf("section chunks with two keys: %v", err)
	}

	want := append(slices.Clone(missed), otherFees...)
	if got := ids(documents); !slices.Equal(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}

	// The limit is split across sections: with two keys and a limit of 2, each
	// section contributes one chunk instead of the first section taking both.
	documents, err = retriever.SectionChunks(ctx, []SectionKey{
		{Source: "a.md", Section: "Missed Payments"},
		{Source: "b.md", Section: "Fees"},
	}, 2)
	if err != nil {
		t.Fatalf("section chunks with limit: %v", err)
	}

	want = []int64{missed[0], otherFees[0]}
	if got := ids(documents); !slices.Equal(got, want) {
		t.Fatalf("expected one chunk per section %v, got %v", want, got)
	}

	// A single-chunk section still matches.
	documents, err = retriever.SectionChunks(ctx, []SectionKey{
		{Source: "a.md", Section: "Fees"},
	}, 20)
	if err != nil {
		t.Fatalf("section chunks for one chunk: %v", err)
	}

	if got := ids(documents); !slices.Equal(got, fees) {
		t.Fatalf("expected %v, got %v", fees, got)
	}

	// An unknown section matches nothing.
	documents, err = retriever.SectionChunks(ctx, []SectionKey{
		{Source: "a.md", Section: "Missing"},
	}, 20)
	if err != nil {
		t.Fatalf("section chunks for an unknown section: %v", err)
	}

	if len(documents) != 0 {
		t.Fatalf("expected no chunks for an unknown section, got %v", ids(documents))
	}

	// No keys means no query at all.
	documents, err = retriever.SectionChunks(ctx, nil, 20)
	if err != nil {
		t.Fatalf("section chunks with no keys: %v", err)
	}

	if documents != nil {
		t.Fatalf("expected nil for no keys, got %v", documents)
	}
}

// A long section must not spend the whole budget and starve a later section.
func TestSectionChunksIntegrationDoesNotStarveSections(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	resetDocuments(t, ctx, conn)

	embedding := unitVector(0)

	large := insertChunks(t, ctx, conn, "a.md", "Large", 30, embedding)
	small := insertChunks(t, ctx, conn, "b.md", "Small", 2, embedding)

	retriever := New(conn)

	documents, err := retriever.SectionChunks(ctx, []SectionKey{
		{Source: "a.md", Section: "Large"},
		{Source: "b.md", Section: "Small"},
	}, 20)
	if err != nil {
		t.Fatalf("section chunks: %v", err)
	}

	// Each section is capped at half the budget, so the small section survives
	// instead of being cut off by the large one.
	want := append(slices.Clone(large[:10]), small...)
	if got := ids(documents); !slices.Equal(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestHybridRetrieveIntegration(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	resetDocuments(t, ctx, conn)

	query := unitVector(0)

	// Three chunks of one section with distinct similarities: the first is the
	// closest, the second clears the similarity floor, and the third falls below
	// it, so only section expansion can recover it.
	embeddings := [][]float64{
		unitVector(0),
		unitVector(0, 1),
		unitVector(0, 1, 2),
	}

	missed := make([]int64, 0, len(embeddings))

	for index, embedding := range embeddings {
		missed = append(missed, insertDocument(t, ctx, conn, Document{
			Source:     "loan-policy.md",
			Section:    "Missed Payments",
			ChunkIndex: index,
			Content: fmt.Sprintf(
				"Missed Payments chunk %d",
				index,
			),
		}, embedding))
	}

	// Orthogonal to the query, so vector search drops it below the floor, but
	// keyword search for the question finds it.
	refund := insertDocument(t, ctx, conn, Document{
		Source:  "large-loan-policy.md",
		Section: "Refunds",
		Content: "refunds are issued within thirty days of a written request",
	}, unitVector(1))

	retriever := New(conn)

	result, err := HybridRetrieve(
		ctx,
		retriever,
		"refunds",
		query,
		PipelineOptions{
			CandidateK:    4,
			FinalK:        2,
			ExpandLimit:   20,
			MinSimilarity: 0.6,
		},
	)
	if err != nil {
		t.Fatalf("hybrid retrieve: %v", err)
	}

	// Vector search keeps the two chunks above the similarity floor, in distance
	// order, and drops the third chunk of the section along with the orthogonal
	// document.
	wantVector := []int64{missed[0], missed[1]}
	if got := ids(result.Vector); !slices.Equal(got, wantVector) {
		t.Fatalf("expected vector results %v, got %v", wantVector, got)
	}

	// Keyword search reaches the section vector search cannot.
	wantKeyword := []int64{refund}
	if got := ids(result.Keyword); !slices.Equal(got, wantKeyword) {
		t.Fatalf("expected keyword results %v, got %v", wantKeyword, got)
	}

	// Fusion ranks the closest chunk of the section first, then the keyword hit;
	// the sibling chunks do not make the cut.
	wantFused := []int64{missed[0], refund}
	if got := ids(result.Fused); !slices.Equal(got, wantFused) {
		t.Fatalf("expected fused results %v, got %v", wantFused, got)
	}

	// Expansion returns every chunk of the fused sections, ordered by source then
	// chunk index, so both the chunk fusion dropped and the chunk the similarity
	// floor rejected come back.
	wantExpanded := []int64{refund, missed[0], missed[1], missed[2]}
	if got := ids(result.Expanded); !slices.Equal(got, wantExpanded) {
		t.Fatalf("expected expanded results %v, got %v", wantExpanded, got)
	}
}

// insertDocumentWithMetadata stores one chunk with an explicit language and
// ingested_at, so the language and metadata-filter integration tests can control
// the columns migration 003 adds. An empty language is stored as NULL, matching
// what ingestion writes for an unset language.
func insertDocumentWithMetadata(
	t *testing.T,
	ctx context.Context,
	conn *pgx.Conn,
	document Document,
	embedding []float64,
	language string,
	ingestedAt time.Time,
) int64 {
	t.Helper()

	var id int64

	err := conn.QueryRow(
		ctx,
		`
		INSERT INTO documents
		    (content, source, section, chunk_index, embedding, language, ingested_at)
		VALUES ($1, $2, $3, $4, $5::vector, NULLIF($6, ''), $7)
		RETURNING id
		`,
		document.Content,
		document.Source,
		document.Section,
		document.ChunkIndex,
		VectorToString(embedding),
		language,
		ingestedAt,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert document with metadata: %v", err)
	}

	return id
}

// sortedIDs returns the document IDs in ascending order, so a test can compare
// result sets without depending on the tie order of ORDER BY.
func sortedIDs(documents []Document) []int64 {
	out := ids(documents)
	slices.Sort(out)

	return out
}

// TestKeywordSearchIntegrationMultilingual asserts a document retrieves under a
// language-appropriate full-text search configuration. German's stemmer matches
// the query "Mietvertrag" to the stored "Mietverträge"; the baseline 'english'
// configuration does not stem German, so it does not match. This is the
// difference the language selection exists to make.
func TestKeywordSearchIntegrationMultilingual(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	resetDocuments(t, ctx, conn)

	now := time.Now().UTC()

	german := insertDocumentWithMetadata(t, ctx, conn, Document{
		Source:  "mietvertrag.md",
		Section: "Zahlung",
		Content: "Mietverträge regeln die monatliche Zahlung",
	}, unitVector(0), "de", now)

	english := insertDocumentWithMetadata(t, ctx, conn, Document{
		Source:  "lease.md",
		Section: "Payment",
		Content: "Leases govern the monthly payment",
	}, unitVector(1), "en", now)

	retriever := New(conn)

	// German configuration stems the plural to the singular, so the query matches.
	germanHits, err := retriever.KeywordSearchFiltered(ctx, "Mietvertrag", 10, "de", Filter{})
	if err != nil {
		t.Fatalf("german keyword search: %v", err)
	}

	if !slices.Contains(ids(germanHits), german) {
		t.Fatalf("german query did not retrieve the german document: %v", ids(germanHits))
	}

	// The baseline english configuration does not stem German, so the same query
	// does not match the german document.
	englishHits, err := retriever.KeywordSearchFiltered(ctx, "Mietvertrag", 10, "en", Filter{})
	if err != nil {
		t.Fatalf("english keyword search: %v", err)
	}

	if slices.Contains(ids(englishHits), german) {
		t.Fatalf("english config unexpectedly stemmed the german document: %v", ids(englishHits))
	}

	// The english document still retrieves under the english configuration.
	enHits, err := retriever.KeywordSearchFiltered(ctx, "payment", 10, "en", Filter{})
	if err != nil {
		t.Fatalf("english keyword search: %v", err)
	}

	if !slices.Contains(ids(enHits), english) {
		t.Fatalf("english query did not retrieve the english document: %v", ids(enHits))
	}
}

// TestHybridRetrieveIntegrationThreadsLanguage asserts the pipeline applies the
// language to keyword retrieval: a german query finds the german document through
// HybridRetrieve with PipelineOptions.Language set, and not with it unset.
func TestHybridRetrieveIntegrationThreadsLanguage(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	resetDocuments(t, ctx, conn)

	now := time.Now().UTC()

	german := insertDocumentWithMetadata(t, ctx, conn, Document{
		Source:  "mietvertrag.md",
		Section: "Zahlung",
		Content: "Mietverträge regeln die monatliche Zahlung",
	}, unitVector(1), "de", now)

	retriever := New(conn)

	withLanguage, err := HybridRetrieve(
		ctx,
		retriever,
		"Mietvertrag",
		unitVector(0),
		PipelineOptions{CandidateK: 10, FinalK: 5, ExpandLimit: 20, MinSimilarity: 0.6, Language: "de"},
	)
	if err != nil {
		t.Fatalf("hybrid retrieve (de): %v", err)
	}

	if !slices.Contains(ids(withLanguage.Keyword), german) {
		t.Fatalf("german keyword stage missed the document: %v", ids(withLanguage.Keyword))
	}

	withoutLanguage, err := HybridRetrieve(
		ctx,
		retriever,
		"Mietvertrag",
		unitVector(0),
		PipelineOptions{CandidateK: 10, FinalK: 5, ExpandLimit: 20, MinSimilarity: 0.6},
	)
	if err != nil {
		t.Fatalf("hybrid retrieve (baseline): %v", err)
	}

	if slices.Contains(ids(withoutLanguage.Keyword), german) {
		t.Fatalf("baseline config unexpectedly matched the german document: %v", ids(withoutLanguage.Keyword))
	}
}

// TestSearchIntegrationFilterBySourcePrefix asserts a source-prefix filter
// narrows vector search to matching sources only.
func TestSearchIntegrationFilterBySourcePrefix(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	resetDocuments(t, ctx, conn)

	query := unitVector(0)

	policiesA := insertDocument(t, ctx, conn, Document{Source: "policies/a.md", Section: "Fees"}, query)
	policiesB := insertDocument(t, ctx, conn, Document{Source: "policies/b.md", Section: "Fees"}, unitVector(1))
	guides := insertDocument(t, ctx, conn, Document{Source: "guides/c.md", Section: "Fees"}, unitVector(2))

	retriever := New(conn)

	got, err := retriever.SearchFiltered(ctx, query, 10, Filter{SourcePrefix: "policies/"})
	if err != nil {
		t.Fatalf("source-prefix search: %v", err)
	}

	want := []int64{policiesA, policiesB}
	slices.Sort(want)

	if !slices.Equal(sortedIDs(got), want) {
		t.Fatalf("source-prefix search = %v, want %v (guides excluded)", ids(got), want)
	}

	// The unfiltered query is unchanged.
	all, err := retriever.Search(ctx, query, 10)
	if err != nil {
		t.Fatalf("unfiltered search: %v", err)
	}

	if len(all) != 3 {
		t.Fatalf("unfiltered search returned %d documents, want 3", len(all))
	}

	_ = guides
}

// TestSearchIntegrationSourcePrefixEscapesWildcards asserts LIKE metacharacters
// in a prefix are matched literally: a prefix containing "%" or "_" does not act
// as a wildcard and does not over-match.
func TestSearchIntegrationSourcePrefixEscapesWildcards(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	resetDocuments(t, ctx, conn)

	query := unitVector(0)

	literalPercent := insertDocument(t, ctx, conn, Document{Source: "100%_match.md", Section: "S"}, query)
	insertDocument(t, ctx, conn, Document{Source: "100percent.md", Section: "S"}, unitVector(1))

	literalUnderscore := insertDocument(t, ctx, conn, Document{Source: "a_b.md", Section: "S"}, unitVector(2))
	insertDocument(t, ctx, conn, Document{Source: "aXb.md", Section: "S"}, unitVector(3))

	retriever := New(conn)

	percentHits, err := retriever.SearchFiltered(ctx, query, 10, Filter{SourcePrefix: "100%"})
	if err != nil {
		t.Fatalf("percent search: %v", err)
	}

	if !slices.Equal(sortedIDs(percentHits), []int64{literalPercent}) {
		t.Fatalf("prefix %% matched %v, want only the literal match", ids(percentHits))
	}

	underscoreHits, err := retriever.SearchFiltered(ctx, query, 10, Filter{SourcePrefix: "a_"})
	if err != nil {
		t.Fatalf("underscore search: %v", err)
	}

	if !slices.Equal(sortedIDs(underscoreHits), []int64{literalUnderscore}) {
		t.Fatalf("prefix a_ matched %v, want only the literal match", ids(underscoreHits))
	}
}

// TestSearchIntegrationFilterByLanguage asserts a language filter matches the
// stored language exactly and excludes documents with no language.
func TestSearchIntegrationFilterByLanguage(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	resetDocuments(t, ctx, conn)

	query := unitVector(0)
	now := time.Now().UTC()

	german := insertDocumentWithMetadata(t, ctx, conn, Document{Source: "de.md", Section: "S"}, query, "de", now)
	english := insertDocumentWithMetadata(t, ctx, conn, Document{Source: "en.md", Section: "S"}, unitVector(1), "en", now)
	unset := insertDocumentWithMetadata(t, ctx, conn, Document{Source: "none.md", Section: "S"}, unitVector(2), "", now)

	retriever := New(conn)

	germanHits, err := retriever.SearchFiltered(ctx, query, 10, Filter{Language: "de"})
	if err != nil {
		t.Fatalf("language filter: %v", err)
	}

	if !slices.Equal(sortedIDs(germanHits), []int64{german}) {
		t.Fatalf("language=de matched %v, want only %d", ids(germanHits), german)
	}

	englishHits, err := retriever.SearchFiltered(ctx, query, 10, Filter{Language: "en"})
	if err != nil {
		t.Fatalf("language filter: %v", err)
	}

	if !slices.Equal(sortedIDs(englishHits), []int64{english}) {
		t.Fatalf("language=en matched %v, want only %d (unset excluded)", ids(englishHits), english)
	}

	_ = unset
}

// TestSearchIntegrationFilterByDateRange asserts an ingested-at range narrows
// results to the documents ingested within it.
func TestSearchIntegrationFilterByDateRange(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	resetDocuments(t, ctx, conn)

	query := unitVector(0)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	old := insertDocumentWithMetadata(t, ctx, conn, Document{Source: "old.md", Section: "S"}, query, "", base)
	recent := insertDocumentWithMetadata(t, ctx, conn, Document{Source: "new.md", Section: "S"}, unitVector(1), "", base.Add(time.Hour))

	midpoint := base.Add(30 * time.Minute)

	retriever := New(conn)

	from := Filter{IngestedFrom: &midpoint}
	fromHits, err := retriever.SearchFiltered(ctx, query, 10, from)
	if err != nil {
		t.Fatalf("date-from filter: %v", err)
	}

	if !slices.Equal(sortedIDs(fromHits), []int64{recent}) {
		t.Fatalf("ingested_from matched %v, want only %d", ids(fromHits), recent)
	}

	to := Filter{IngestedTo: &midpoint}
	toHits, err := retriever.SearchFiltered(ctx, query, 10, to)
	if err != nil {
		t.Fatalf("date-to filter: %v", err)
	}

	if !slices.Equal(sortedIDs(toHits), []int64{old}) {
		t.Fatalf("ingested_to matched %v, want only %d", ids(toHits), old)
	}
}

// TestSearchIntegrationFilterCombined asserts source, language, and date-range
// predicates combine (AND) so only a document satisfying all of them matches.
func TestSearchIntegrationFilterCombined(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	resetDocuments(t, ctx, conn)

	query := unitVector(0)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	match := insertDocumentWithMetadata(t, ctx, conn, Document{Source: "policies/de.md", Section: "S"}, query, "de", base.Add(time.Hour))
	insertDocumentWithMetadata(t, ctx, conn, Document{Source: "policies/en.md", Section: "S"}, unitVector(1), "en", base.Add(time.Hour))
	insertDocumentWithMetadata(t, ctx, conn, Document{Source: "guides/de.md", Section: "S"}, unitVector(2), "de", base.Add(time.Hour))
	insertDocumentWithMetadata(t, ctx, conn, Document{Source: "policies/de-old.md", Section: "S"}, unitVector(3), "de", base)

	midpoint := base.Add(30 * time.Minute)
	filter := Filter{SourcePrefix: "policies/", Language: "de", IngestedFrom: &midpoint}

	retriever := New(conn)

	got, err := retriever.SearchFiltered(ctx, query, 10, filter)
	if err != nil {
		t.Fatalf("combined filter: %v", err)
	}

	if !slices.Equal(sortedIDs(got), []int64{match}) {
		t.Fatalf("combined filter matched %v, want only %d", ids(got), match)
	}
}

// TestSearchIntegrationNoFilterMatchesBaseline asserts a zero-value filter yields
// exactly the same rows and order as the unfiltered query.
func TestSearchIntegrationNoFilterMatchesBaseline(t *testing.T) {
	ctx := context.Background()
	conn := requireDatabase(t)

	resetDocuments(t, ctx, conn)

	query := unitVector(0)

	insertDocument(t, ctx, conn, Document{Source: "a.md", Section: "S"}, query)
	insertDocument(t, ctx, conn, Document{Source: "b.md", Section: "S"}, unitVector(1))

	retriever := New(conn)

	baseline, err := retriever.Search(ctx, query, 10)
	if err != nil {
		t.Fatalf("baseline search: %v", err)
	}

	filtered, err := retriever.SearchFiltered(ctx, query, 10, Filter{})
	if err != nil {
		t.Fatalf("zero-filter search: %v", err)
	}

	if !slices.Equal(ids(baseline), ids(filtered)) {
		t.Fatalf("zero filter changed the result: %v vs %v", ids(baseline), ids(filtered))
	}
}
