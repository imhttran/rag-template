// Package ingestion stores document chunks and their embeddings in PostgreSQL.
package ingestion

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"rag-template/internal/chunking"
	"rag-template/internal/embedding"
	"rag-template/internal/retrieval"
)

// Ingester writes documents and their embeddings to PostgreSQL.
type Ingester struct {
	conn     *pgx.Conn
	embedder *embedding.Embedder
}

// New returns an Ingester that writes to conn using embedder.
func New(conn *pgx.Conn, embedder *embedding.Embedder) *Ingester {
	return &Ingester{
		conn:     conn,
		embedder: embedder,
	}
}

// ReplaceDocument replaces all stored chunks for source.
//
// Embeddings are generated before the transaction starts. The database
// replacement itself is atomic: either every new chunk is stored or the
// previous document remains unchanged.
func (i *Ingester) ReplaceDocument(
	ctx context.Context,
	source string,
	chunks []chunking.Chunk,
) error {
	type embeddedChunk struct {
		chunk  chunking.Chunk
		vector []float64
	}

	embedded := make([]embeddedChunk, 0, len(chunks))

	// Generate embeddings before opening the transaction. Ollama may take
	// significantly longer than the database work, so we do not want to keep
	// a PostgreSQL transaction open while waiting for it.
	for _, chunk := range chunks {
		vector, err := i.embedder.Embed(ctx, chunk.Content)
		if err != nil {
			return fmt.Errorf(
				"embed chunk %d in section %q: %w",
				chunk.Index,
				chunk.Section,
				err,
			)
		}

		embedded = append(embedded, embeddedChunk{
			chunk:  chunk,
			vector: vector,
		})
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

	for _, item := range embedded {
		_, err := tx.Exec(
			ctx,
			`
			INSERT INTO documents (
				source,
				section,
				chunk_index,
				content,
				embedding
			)
			VALUES ($1, $2, $3, $4, $5::vector)
			`,
			source,
			item.chunk.Section,
			item.chunk.Index,
			item.chunk.Content,
			retrieval.VectorToString(item.vector),
		)
		if err != nil {
			return fmt.Errorf(
				"insert chunk %d in section %q: %w",
				item.chunk.Index,
				item.chunk.Section,
				err,
			)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
