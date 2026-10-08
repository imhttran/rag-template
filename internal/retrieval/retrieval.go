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

// DefaultFTSConfig is the full-text search configuration used when a document
// has no language set. It is the baseline behavior, so unset-language retrieval
// is byte-identical to the pre-language implementation.
const DefaultFTSConfig = "english"

// FallbackFTSConfig is the configuration used for languages that have no
// dedicated PostgreSQL text search configuration, so retrieval still works.
const FallbackFTSConfig = "simple"

// ftsConfigByLanguage maps a BCP-47 language tag (or its primary subtag) to the
// PostgreSQL text search configuration that matches it. Only configurations
// known to be installed in a stock PostgreSQL are listed; anything else falls
// back to FallbackFTSConfig.
var ftsConfigByLanguage = map[string]string{
	"ar": "arabic",
	"hy": "armenian",
	"eu": "basque",
	"ca": "catalan",
	"da": "danish",
	"nl": "dutch",
	"en": "english",
	"fi": "finnish",
	"fr": "french",
	"de": "german",
	"el": "greek",
	"hi": "hindi",
	"hu": "hungarian",
	"id": "indonesian",
	"ga": "irish",
	"it": "italian",
	"lt": "lithuanian",
	"ne": "nepali",
	"no": "norwegian",
	"pt": "portuguese",
	"ro": "romanian",
	"ru": "russian",
	"sr": "serbian",
	"es": "spanish",
	"sv": "swedish",
	"ta": "tamil",
	"tr": "turkish",
	"yi": "yiddish",
}

// NormalizeFTSConfig maps a language to a PostgreSQL full-text search
// configuration. An unset/empty language uses the baseline DefaultFTSConfig
// ('english'); a supported language uses its language-appropriate
// configuration; any unsupported or invalid language falls back to
// FallbackFTSConfig ('simple'). It never errors, so an invalid configuration
// can never be bound.
func NormalizeFTSConfig(language string) string {
	tag := strings.ToLower(strings.TrimSpace(language))
	if tag == "" {
		return DefaultFTSConfig
	}

	// Accept both "de" and "de-DE" / "de_DE" by matching the primary subtag.
	primary := tag
	if index := strings.IndexAny(tag, "-_"); index >= 0 {
		primary = tag[:index]
	}

	if config, ok := ftsConfigByLanguage[primary]; ok {
		return config
	}

	return FallbackFTSConfig
}

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
	return r.SearchFiltered(ctx, vector, topK, Filter{})
}

// SearchFiltered is Search with an optional metadata filter. The candidate
// LIMIT applies to the filtered set: the filter predicate narrows the rows that
// participate in the nearest-neighbour ordering before topK is applied. With a
// zero Filter the query is byte-identical to Search.
func (r *Retriever) SearchFiltered(
	ctx context.Context,
	vector []float64,
	topK int,
	filter Filter,
) ([]Document, error) {
	// The vector is $1; when a filter is present its fragment follows it; the
	// topK placeholder is emitted last so its index is correct either way.
	fragment, filterArgs := filterFragment(filter, 2)

	where := ""
	if fragment != "" {
		where = "WHERE " + fragment + "\n"
	}

	args := make([]any, 0, 2+len(filterArgs))
	args = append(args, VectorToString(vector))
	args = append(args, filterArgs...)
	args = append(args, topK)

	limitIndex := 2 + len(filterArgs)

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
		`+where+`ORDER BY embedding <=> $1::vector
		LIMIT $`+strconv.Itoa(limitIndex)+`
		`,
		args...,
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
// PostgreSQL full-text search with the baseline (unset-language) configuration.
func (r *Retriever) KeywordSearch(
	ctx context.Context,
	query string,
	topK int,
) ([]Document, error) {
	return r.KeywordSearchLanguage(ctx, query, topK, "")
}

// KeywordSearchLanguage is KeywordSearch with an explicit language. The
// language is normalized to a full-text search configuration (unset ->
// 'english', supported -> its configuration, unsupported -> 'simple') and the
// configuration is always bound as a query parameter, never interpolated.
func (r *Retriever) KeywordSearchLanguage(
	ctx context.Context,
	query string,
	topK int,
	language string,
) ([]Document, error) {
	return r.KeywordSearchFiltered(ctx, query, topK, language, Filter{})
}

// KeywordSearchFiltered is KeywordSearchLanguage with an optional metadata
// filter. The full-text predicate and the metadata predicates combine in the
// WHERE clause; the FTS config and query parameters stay bound at their existing
// indexes, and any filter fragment is appended after them. With a zero Filter
// the query is byte-identical to KeywordSearchLanguage.
func (r *Retriever) KeywordSearchFiltered(
	ctx context.Context,
	query string,
	topK int,
	language string,
	filter Filter,
) ([]Document, error) {
	config := NormalizeFTSConfig(language)

	// Existing parameters: $1 query, $2 topK, $3 config. The filter fragment
	// starts at $4 so those indexes never shift.
	fragment, filterArgs := filterFragment(filter, 4)

	where := `
		WHERE
		    to_tsvector(
		        $3::regconfig,
		        COALESCE(section, '') || ' ' || content
		    ) @@ websearch_to_tsquery($3::regconfig, $1)`

	if fragment != "" {
		where += "\n		    AND " + fragment
	}

	args := make([]any, 0, 3+len(filterArgs))
	args = append(args, query, topK, config)
	args = append(args, filterArgs...)

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
		            $3::regconfig,
		            COALESCE(section, '') || ' ' || content
		        ),
		        websearch_to_tsquery($3::regconfig, $1)
		    )::float8 AS rank
		FROM documents`+where+`
		ORDER BY rank DESC
		LIMIT $2
		`,
		args...,
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
	return r.SectionChunksFiltered(ctx, keys, limit, Filter{})
}

// SectionChunksFiltered is SectionChunks with an optional metadata filter.
// Expansion only returns chunks whose metadata satisfies the filter, so chunks
// from excluded sources/languages/dates are never returned. The per-section
// limit split is unchanged, and the limit placeholder is rebased after the
// filter arguments. With a zero Filter the query is byte-identical to
// SectionChunks, and empty keys still return (nil, nil).
func (r *Retriever) SectionChunksFiltered(
	ctx context.Context,
	keys []SectionKey,
	limit int,
	filter Filter,
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

	// The section-key placeholders occupy $1..$2n; any filter fragment follows.
	fragment, filterArgs := filterFragment(filter, len(keys)*2+1)

	where := `(source, section) IN (` + strings.Join(placeholders, ", ") + ")"
	if fragment != "" {
		where += "\n\t\t\t\tAND " + fragment
	}

	args = append(args, filterArgs...)
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
			WHERE `+where+`
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
