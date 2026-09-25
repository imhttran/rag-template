// Package retrieval finds the documents closest to a query vector using
// PostgreSQL and the pgvector extension.
package retrieval

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Document is a stored chunk returned by a search. Similarity is the cosine
// score from vector search, KeywordScore the full-text rank, and FusionScore the
// reciprocal-rank score; each is zero unless that retrieval path set it.
type Document struct {
	ID           int64
	Source       string
	Section      string
	ChunkIndex   int
	Content      string
	Similarity   float64
	KeywordScore float64
	FusionScore  float64
}

// Retriever searches a pgvector-backed documents table.
type Retriever struct {
	conn *pgx.Conn
}

// New returns a Retriever backed by conn.
func New(conn *pgx.Conn) *Retriever {
	return &Retriever{conn: conn}
}

// Search returns the topK documents most similar to vector.
func (r *Retriever) Search(ctx context.Context, vector []float64, topK int) ([]Document, error) {
	rows, err := r.conn.Query(
		ctx,
		`
		SELECT
		    id,
		    COALESCE(source, ''),
		    COALESCE(section, ''),
		    chunk_index,
		    content,
		    1 - (embedding <=> $1::vector) AS similarity
		FROM documents
		ORDER BY embedding <=> $1::vector
		LIMIT $2
		`,
		VectorToString(vector),
		topK,
	)
	if err != nil {
		return nil, fmt.Errorf("query documents: %w", err)
	}
	defer rows.Close()

	return scanDocuments(rows, func(doc *Document) []any {
		return []any{
			&doc.ID,
			&doc.Source,
			&doc.Section,
			&doc.ChunkIndex,
			&doc.Content,
			&doc.Similarity,
		}
	})
}

// VectorToString renders a vector in the literal format pgvector expects.
func VectorToString(vector []float64) string {
	values := make([]string, len(vector))

	for i, value := range vector {
		values[i] = strconv.FormatFloat(value, 'f', -1, 64)
	}

	return "[" + strings.Join(values, ",") + "]"
}

// KeepSimilar returns the documents whose similarity is at least minSimilarity.
func KeepSimilar(documents []Document, minSimilarity float64) []Document {
	kept := make([]Document, 0, len(documents))

	for _, document := range documents {
		if document.Similarity >= minSimilarity {
			kept = append(kept, document)
		}
	}

	return kept
}

// DeduplicateSections keeps the first document for each source/section pair.
//
// Because retrieval results are ranked best-first, the first document kept is
// the highest-ranked one for that section.
func DeduplicateSections(documents []Document) []Document {
	seen := make(map[string]bool)

	kept := make([]Document, 0, len(documents))

	for _, document := range documents {
		key := document.Source + "|" + document.Section

		if seen[key] {
			continue
		}

		seen[key] = true
		kept = append(kept, document)
	}

	return kept
}

// FormatDocuments renders documents as prompt blocks. A non-empty label prefixes
// each block with "<label><index>" (zero-based), e.g. "ID: 0"; an empty label
// omits the prefix.
func FormatDocuments(documents []Document, label string) string {
	var builder strings.Builder

	for index, document := range documents {
		if label != "" {
			fmt.Fprintf(&builder, "%s%d\n", label, index)
		}

		fmt.Fprintf(
			&builder,
			"Source: %s\nSection: %s\nContent: %s\n\n",
			document.Source,
			document.Section,
			document.Content,
		)
	}

	return builder.String()
}

// KeywordSearch returns the topK documents that best match query using
// PostgreSQL full-text search.
func (r *Retriever) KeywordSearch(
	ctx context.Context,
	query string,
	topK int,
) ([]Document, error) {
	rows, err := r.conn.Query(
		ctx,
		`
		SELECT
		    id,
		    COALESCE(source, ''),
		    COALESCE(section, ''),
		    chunk_index,
		    content,
		    ts_rank(
		        to_tsvector(
		            'english',
		            COALESCE(section, '') || ' ' || content
		        ),
		        websearch_to_tsquery('english', $1)
		    )::float8 AS rank
		FROM documents
		WHERE
		    to_tsvector(
		        'english',
		        COALESCE(section, '') || ' ' || content
		    ) @@ websearch_to_tsquery('english', $1)
		ORDER BY rank DESC
		LIMIT $2
		`,
		query,
		topK,
	)
	if err != nil {
		return nil, fmt.Errorf("keyword search documents: %w", err)
	}
	defer rows.Close()

	return scanDocuments(rows, func(doc *Document) []any {
		return []any{
			&doc.ID,
			&doc.Source,
			&doc.Section,
			&doc.ChunkIndex,
			&doc.Content,
			&doc.KeywordScore,
		}
	})
}

// scanDocuments reads Document rows, using columns to choose which fields a given
// query populates.
func scanDocuments(rows pgx.Rows, columns func(*Document) []any) ([]Document, error) {
	var documents []Document

	for rows.Next() {
		var doc Document

		if err := rows.Scan(columns(&doc)...); err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}

		documents = append(documents, doc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documents: %w", err)
	}

	return documents, nil
}

// SectionKey identifies a source/section pair.
type SectionKey struct {
	Source  string
	Section string
}

// SectionKeys returns the distinct source/section pairs of documents, in order
// of first appearance.
func SectionKeys(documents []Document) []SectionKey {
	kept := DeduplicateSections(documents)

	keys := make([]SectionKey, 0, len(kept))

	for _, document := range kept {
		keys = append(keys, SectionKey{
			Source:  document.Source,
			Section: document.Section,
		})
	}

	return keys
}

// SectionChunks returns chunks of the given sections in source, section, and
// chunk order. The limit is split across the sections, so one long section
// cannot starve the others.
func (r *Retriever) SectionChunks(
	ctx context.Context,
	keys []SectionKey,
	limit int,
) ([]Document, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	perSection := max(1, limit/len(keys))

	placeholders := make([]string, len(keys))
	args := make([]any, 0, len(keys)*2+1)

	for i, key := range keys {
		placeholders[i] = fmt.Sprintf("($%d, $%d)", 2*i+1, 2*i+2)
		args = append(args, key.Source, key.Section)
	}

	args = append(args, perSection)

	rows, err := r.conn.Query(
		ctx,
		`
		SELECT
			id,
			COALESCE(source, '') AS source,
			COALESCE(section, '') AS section,
			chunk_index,
			content
		FROM (
			SELECT
				id,
				source,
				section,
				chunk_index,
				content,
				ROW_NUMBER() OVER (
					PARTITION BY source, section
					ORDER BY chunk_index
				) AS rank
			FROM documents
			WHERE (source, section) IN (`+strings.Join(placeholders, ", ")+`)
		) AS ranked
		WHERE rank <= $`+strconv.Itoa(len(args))+`
		ORDER BY source, section, chunk_index
		`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query section chunks: %w", err)
	}
	defer rows.Close()

	return scanDocuments(rows, func(doc *Document) []any {
		return []any{
			&doc.ID,
			&doc.Source,
			&doc.Section,
			&doc.ChunkIndex,
			&doc.Content,
		}
	})
}
