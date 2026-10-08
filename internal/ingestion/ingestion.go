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

// Ingester writes documents and their embeddings to PostgreSQL.
type Ingester struct {
	conn     *pgx.Conn
	embedder *embedding.Embedder

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
func New(conn *pgx.Conn, embedder *embedding.Embedder) *Ingester {
	return NewWithOptions(conn, embedder, DefaultEmbedWorkers, DefaultEmbedRetries)
}

// NewWithOptions returns an Ingester with an explicit worker count and retry
// bound. workers is the maximum number of concurrent embedding calls; retries
// is how many times a transient embedding failure is retried. Values below
// their minimums are clamped (workers to 1, retries to 0).
func NewWithOptions(
	conn *pgx.Conn,
	embedder *embedding.Embedder,
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

// ReplaceDocument replaces all stored chunks for source.
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
func (i *Ingester) ReplaceDocument(
	ctx context.Context,
	source string,
	chunks []chunking.Chunk,
) error {
	embedded, err := i.embedAll(ctx, chunks)
	if err != nil {
		return err
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

	if err := i.insertChunks(ctx, tx, source, embedded); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
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
	embedded []embeddedChunk,
) error {
	if len(embedded) == 0 {
		return nil
	}

	var builder strings.Builder
	builder.WriteString(
		`INSERT INTO documents (source, section, chunk_index, content, ` +
			`embedding) VALUES `,
	)

	args := make([]any, 0, len(embedded)*5)

	for index, item := range embedded {
		if index > 0 {
			builder.WriteString(", ")
		}

		base := index * 5

		fmt.Fprintf(
			&builder,
			"($%d, $%d, $%d, $%d, $%d::vector)",
			base+1,
			base+2,
			base+3,
			base+4,
			base+5,
		)

		args = append(
			args,
			source,
			item.chunk.Section,
			item.chunk.Index,
			item.chunk.Content,
			retrieval.VectorToString(item.vector),
		)
	}

	if _, err := tx.Exec(ctx, builder.String(), args...); err != nil {
		return fmt.Errorf("insert %d chunks: %w", len(embedded), err)
	}

	return nil
}
