// Package ingestion stores document chunks and their embeddings in PostgreSQL.
package ingestion

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"rag-template/internal/chunking"
	"rag-template/internal/embedding"
	"rag-template/internal/retrieval"
)

const (
	// DefaultEmbedWorkers is the modest default number of chunks embedded
	// concurrently. Ollama serves one model instance, so a larger number mostly
	// queues server side.
	DefaultEmbedWorkers = 4

	// DefaultEmbedRetries is how many times a transient embedding failure is
	// retried before the ingest fails.
	DefaultEmbedRetries = 3

	// defaultRetryBackoff is the base delay between retries. It doubles on each
	// attempt (exponential backoff): 200ms, 400ms, 800ms for the default bound.
	defaultRetryBackoff = 200 * time.Millisecond

	// maxRetryBackoff caps a single backoff delay.
	maxRetryBackoff = 5 * time.Second
)

// Provenance records where a chunk came from and how it was produced. It is
// stored alongside every chunk so "is this document up to date?" is answerable
// from the database and an unchanged file can be skipped without re-embedding.
type Provenance struct {
	// ContentHash is a fingerprint of the file's bytes combined with the
	// chunker configuration, so a content or chunker-config change invalidates
	// it.
	ContentHash string

	// EmbedModel is the embedding model that produced the vectors.
	EmbedModel string

	// Dimension is the width of the embedding vectors.
	Dimension int

	// ChunkerConfig is a canonical serialization of the chunker settings.
	ChunkerConfig string

	// Language is the document's language as a BCP-47 tag (for example "en",
	// "de"). An empty value is stored as NULL (unset); retrieval treats unset
	// as the baseline configuration, so no default changes.
	Language string

	// IngestedAt is when the chunks were written. It is set by the caller so a
	// batch shares one timestamp; a zero value is replaced with the current
	// time.
	IngestedAt time.Time
}

// Ingester writes documents and their embeddings to PostgreSQL.
type Ingester struct {
	conn     *pgx.Conn
	embedder embedding.Embedder

	// embedWorkers bounds how many embedding calls run concurrently. It is
	// always at least 1.
	embedWorkers int

	// embedRetries is how many times a transient embedding failure is retried
	// before the ingest gives up. It is always at least 0.
	embedRetries int

	// retryBackoff is the base delay between retries; it doubles per retry.
	retryBackoff time.Duration
}

// New returns an Ingester that writes to conn using embedder with the default
// concurrency and retry settings.
func New(conn *pgx.Conn, embedder embedding.Embedder) *Ingester {
	return NewWithOptions(conn, embedder, DefaultEmbedWorkers, DefaultEmbedRetries)
}

// NewWithOptions returns an Ingester with an explicit worker count and retry
// bound. workers is the maximum number of concurrent embedding calls; retries
// is how many times a transient embedding failure is retried. Values below
// their minimums are clamped (workers to 1, retries to 0).
func NewWithOptions(
	conn *pgx.Conn,
	embedder embedding.Embedder,
	workers int,
	retries int,
) *Ingester {
	if workers < 1 {
		workers = 1
	}

	if retries < 0 {
		retries = 0
	}

	return &Ingester{
		conn:         conn,
		embedder:     embedder,
		embedWorkers: workers,
		embedRetries: retries,
		retryBackoff: defaultRetryBackoff,
	}
}

// embeddedChunk pairs a chunk with its embedding vector.
type embeddedChunk struct {
	chunk  chunking.Chunk
	vector []float64
}

// ReplaceDocument replaces all stored chunks for source with no provenance.
//
// It is a thin wrapper over ReplaceDocumentWithProvenance kept for callers that
// do not need the provenance columns populated.
func (i *Ingester) ReplaceDocument(
	ctx context.Context,
	source string,
	chunks []chunking.Chunk,
) error {
	return i.ReplaceDocumentWithProvenance(ctx, source, chunks, Provenance{})
}

// ReplaceDocumentWithProvenance replaces all stored chunks for source and
// records provenance alongside every chunk.
//
// Embeddings are generated before the transaction starts. The database
// replacement itself is atomic: either every new chunk is stored or the
// previous document remains unchanged.
//
// Embedding calls run concurrently, up to Ingester's configured worker count,
// but each result is written back into its chunk's position so the stored order
// matches the input order regardless of completion order. Transient embedding
// failures are retried with backoff up to the configured bound; a terminal
// failure returns before the transaction begins, so the stored document is left
// untouched.
func (i *Ingester) ReplaceDocumentWithProvenance(
	ctx context.Context,
	source string,
	chunks []chunking.Chunk,
	provenance Provenance,
) error {
	embedded, err := i.embedAll(ctx, chunks)
	if err != nil {
		return err
	}

	dimension := provenance.Dimension
	if dimension == 0 && len(embedded) > 0 {
		dimension = len(embedded[0].vector)
	}

	stored := provenance
	stored.Dimension = dimension

	if stored.IngestedAt.IsZero() {
		stored.IngestedAt = time.Now().UTC()
	}

	tx, err := i.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// Rollback is safe even after Commit. We defer it so every error path
	// automatically cleans up the transaction.
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(
		ctx,
		`DELETE FROM documents WHERE source = $1`,
		source,
	); err != nil {
		return fmt.Errorf("delete existing document: %w", err)
	}

	if err := i.insertChunks(ctx, tx, source, stored, embedded); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// IsUpToDate reports whether the stored chunks for source already match the
// supplied provenance, so the caller can skip re-embedding entirely.
//
// It returns true only when the source has at least one stored row and every
// stored row carries the same content hash, chunker config, embedding model,
// and (when supplied) embedding dimension as the supplied provenance. A missing
// source, a content or chunker-config change, or a model or dimension change all
// report false, which routes the caller through the full replacement path.
func (i *Ingester) IsUpToDate(
	ctx context.Context,
	source string,
	provenance Provenance,
) (bool, error) {
	var (
		total   int
		matches int
	)

	// A zero dimension means the caller did not pin one, so the dimension check
	// is skipped rather than matching against a stored NULL.
	dimensionMatches := "TRUE"
	if provenance.Dimension > 0 {
		dimensionMatches = "embedding_dim = $5"
	}

	query := fmt.Sprintf(
		`
		SELECT
		    COUNT(*),
		    COUNT(*) FILTER (
		        WHERE content_hash = $2
		          AND chunker_config = $3
		          AND embed_model = $4
		          AND %s
		    )
		FROM documents
		WHERE source = $1
		`,
		dimensionMatches,
	)

	// Bind only the placeholders the query actually references: with no dimension
	// the FILTER uses TRUE and there is no $5, so passing a fifth argument fails
	// the bind ("expected 4 arguments, got 5").
	args := []any{
		source,
		provenance.ContentHash,
		provenance.ChunkerConfig,
		provenance.EmbedModel,
	}

	if provenance.Dimension > 0 {
		args = append(args, provenance.Dimension)
	}

	err := i.conn.QueryRow(ctx, query, args...).Scan(&total, &matches)
	if err != nil {
		return false, fmt.Errorf("query stored provenance: %w", err)
	}

	if total == 0 {
		return false, nil
	}

	return matches == total, nil
}

// embedAll embeds every chunk with bounded concurrency and returns the results
// in input order. The first terminal error aborts the remaining work and is
// returned wrapped with the chunk's index and section. No database write
// happens here, so a failure leaves stored documents unchanged.
func (i *Ingester) embedAll(
	ctx context.Context,
	chunks []chunking.Chunk,
) ([]embeddedChunk, error) {
	embed := i.embedder.Embed

	return mapOrdered(
		ctx,
		chunks,
		i.embedWorkers,
		func(callCtx context.Context, _ int, chunk chunking.Chunk) (embeddedChunk, error) {
			vector, err := retryEmbed(
				callCtx,
				embed,
				chunk,
				i.embedRetries,
				i.retryBackoff,
			)
			if err != nil {
				return embeddedChunk{}, err
			}

			return embeddedChunk{
				chunk:  chunk,
				vector: vector,
			}, nil
		},
	)
}

// insertChunks writes every embedded chunk with one batched statement inside tx.
// It constructs a single multi-row INSERT so the delete-then-insert replacement
// stays atomic and the round trips do not scale with the chunk count.
func (i *Ingester) insertChunks(
	ctx context.Context,
	tx pgx.Tx,
	source string,
	provenance Provenance,
	embedded []embeddedChunk,
) error {
	if len(embedded) == 0 {
		return nil
	}

	var builder strings.Builder
	builder.WriteString(
		`INSERT INTO documents (source, section, chunk_index, content, ` +
			`embedding, content_hash, embed_model, embedding_dim, ` +
			`chunker_config, ingested_at, language) VALUES `,
	)

	args := make([]any, 0, len(embedded)*11)

	for index, item := range embedded {
		if index > 0 {
			builder.WriteString(", ")
		}

		base := index * 11

		fmt.Fprintf(
			&builder,
			"($%d, $%d, $%d, $%d, $%d::vector, $%d, $%d, $%d, $%d, $%d, $%d)",
			base+1,
			base+2,
			base+3,
			base+4,
			base+5,
			base+6,
			base+7,
			base+8,
			base+9,
			base+10,
			base+11,
		)

		args = append(
			args,
			source,
			item.chunk.Section,
			item.chunk.Index,
			item.chunk.Content,
			retrieval.VectorToString(item.vector),
			nullableString(provenance.ContentHash),
			nullableString(provenance.EmbedModel),
			nullableInt(provenance.Dimension),
			nullableString(provenance.ChunkerConfig),
			provenance.IngestedAt,
			nullableString(provenance.Language),
		)
	}

	if _, err := tx.Exec(ctx, builder.String(), args...); err != nil {
		return fmt.Errorf("insert %d chunks: %w", len(embedded), err)
	}

	return nil
}

// nullableString returns nil for an empty string so the provenance column is
// stored as NULL rather than ”, keeping "not recorded" distinct from "recorded
// empty".
func nullableString(value string) any {
	if value == "" {
		return nil
	}

	return value
}

// nullableInt returns nil for a zero dimension.
func nullableInt(value int) any {
	if value == 0 {
		return nil
	}

	return value
}
