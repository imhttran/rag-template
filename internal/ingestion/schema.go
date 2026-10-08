package ingestion

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// migrationFile names the documented procedure operators apply to change the
// stored embedding dimension. It is referenced from the guard's error so the
// fix is one file away. The .sql.example suffix keeps it out of the automatic
// migration glob (migrations/*.sql and the docker-entrypoint-initdb.d mount), so
// applying it is always an explicit, operator-run step.
const migrationFile = "migrations/003_embedding_dim.sql.example"

// embeddingDimQuerier is the single method the guard needs from a database
// handle. *pgx.Conn satisfies it in production; a fake satisfies it in tests,
// so the guard is unit-testable without a live database.
type embeddingDimQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// lookupEmbeddingDimFunc reads the declared dimension of documents.embedding
// from the database catalog. It is a package-level seam so tests can substitute
// a stub without a live database.
var lookupEmbeddingDimFunc = lookupEmbeddingDim

// CheckEmbeddingDim verifies that the dimension pgvector stores for
// documents.embedding matches the configured embedding dimension.
//
// It is a read-only catalog query. It returns nil when the declared dimension
// equals dim, and an actionable error otherwise: a mismatch names both the
// configured and the stored dimension and points at the migration and
// re-ingest that resolve it, while a missing table or column says so directly.
// Running the guard before ingest or retrieval turns what would otherwise be an
// opaque pgvector error into a fail-fast message.
func CheckEmbeddingDim(ctx context.Context, conn embeddingDimQuerier, dim int) error {
	stored, err := lookupEmbeddingDimFunc(ctx, conn)
	if err != nil {
		return err
	}

	if stored == dim {
		return nil
	}

	return fmt.Errorf(
		"EMBED_DIM (%d) does not match the documents.embedding column (vector(%d)): "+
			"apply %s to set the column to the dimension you want, re-ingest the corpus, "+
			"then set EMBED_DIM=%d to match the column",
		dim,
		stored,
		migrationFile,
		dim,
	)
}

// embeddingDimQuery selects the declared width of the documents.embedding
// column. pgvector stores vector(n) in pg_attribute.atttypmod, so the query
// joins the column to its table by OID and reads that attribute.
//
// The table is resolved to a single OID with a deterministic schema order
// (current_schema() first, then search_path order) and the column is joined by
// attrelid, so the query reads the same documents.embedding the runtime SQL
// writes to rather than an arbitrary same-named table in another schema.
const embeddingDimQuery = `
	WITH target AS (
		SELECT c.oid
		FROM pg_class AS c
		JOIN pg_namespace AS n ON n.oid = c.relnamespace
		WHERE c.relname = 'documents'
		  AND n.nspname = ANY (current_schemas(false))
		ORDER BY (n.nspname = current_schema()) DESC, array_position(current_schemas(false), n.nspname)
		LIMIT 1
	)
	SELECT a.atttypmod
	FROM pg_attribute AS a
	JOIN target ON target.oid = a.attrelid
	WHERE a.attname = 'embedding'
	  AND NOT a.attisdropped
`

// lookupEmbeddingDim reads the declared dimension of documents.embedding from
// the PostgreSQL catalog. A missing table or column is reported as an error, not
// as dimension zero.
func lookupEmbeddingDim(ctx context.Context, conn embeddingDimQuerier) (int, error) {
	var typmod int

	if err := conn.QueryRow(ctx, embeddingDimQuery).Scan(&typmod); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf(
				"documents.embedding does not exist: is the schema migrated? "+
					"apply migrations/001_init.sql (and %s for a non-768 model)",
				migrationFile,
			)
		}

		return 0, fmt.Errorf("read documents.embedding dimension: %w", err)
	}

	return vectorFromTypmod(typmod)
}

// vectorFromTypmod converts pg_attribute.atttypmod to the pgvector dimension.
// pgvector records vector(n) with atttypmod equal to n, so the stored width is the
// typmod itself -- unlike varchar/bpchar, which add a length header. A typmod
// pgvector would not produce is rejected instead of being reported as a nonsense
// dimension.
func vectorFromTypmod(typmod int) (int, error) {
	if typmod < 4 {
		return 0, fmt.Errorf(
			"documents.embedding is not a pgvector vector(n) column: "+
				"apply migrations/001_init.sql to create it (or %s to change its dimension)",
			migrationFile,
		)
	}

	return typmod, nil
}
