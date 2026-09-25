// PostgreSQL integration tests for the pgvector-backed queries.
//
// They are skipped unless RAG_INTEGRATION=1 is set, so `go test ./...` (and the
// pre-commit hook) stays fast. When the variable is set, TestMain connects to
// DATABASE_URL and applies migrations/001_init.sql, so the tests run against the
// docker-compose database (`make db-up`) and share one connection.
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
