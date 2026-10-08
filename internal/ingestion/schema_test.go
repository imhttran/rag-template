package ingestion

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// repoRoot returns the repository root, derived from this test file's location
// (internal/ingestion) so the relative migrationFile path can be resolved
// regardless of the directory go test runs the package in.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// TestMigrationFilePointsAtProcedureDocument asserts the guard's referenced
// file is the operator-run procedure under docs/operations/, and that it is not
// matched by the automatic migration glob (migrations/*.sql).
func TestMigrationFilePointsAtProcedureDocument(t *testing.T) {
	if got, want := migrationFile, "docs/operations/embedding-dimension.md"; got != want {
		t.Fatalf("migrationFile = %q, want %q", got, want)
	}

	if strings.HasPrefix(migrationFile, "migrations/") || strings.HasSuffix(migrationFile, ".sql") {
		t.Fatalf("migrationFile = %q, must not be matched by migrations/*.sql", migrationFile)
	}

	path := filepath.Join(repoRoot(t), filepath.FromSlash(migrationFile))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("os.Stat(%q) error: %v", path, err)
	}
}

// TestCheckEmbeddingDimMatching asserts the guard is silent when the catalog
// dimension equals the configured dimension (768 against a 001_init schema).
func TestCheckEmbeddingDimMatching(t *testing.T) {
	restore := stubLookup(t, 768, nil)
	defer restore()

	if err := CheckEmbeddingDim(context.Background(), nil, 768); err != nil {
		t.Fatalf("CheckEmbeddingDim() = %v, want nil", err)
	}
}

// TestCheckEmbeddingDimMismatch asserts a mismatch names both dimensions and
// the operator-run procedure/re-ingest remedy, and does not point at a fixed
// dimension.
func TestCheckEmbeddingDimMismatch(t *testing.T) {
	restore := stubLookup(t, 768, nil)
	defer restore()

	err := CheckEmbeddingDim(context.Background(), nil, 1024)
	if err == nil {
		t.Fatal("CheckEmbeddingDim() = nil, want a mismatch error")
	}

	message := err.Error()

	for _, want := range []string{"1024", "768", migrationFile, "re-ingest", "EMBED_DIM=1024"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error = %q, want it to contain %q", message, want)
		}
	}

	if !strings.Contains(message, "docs/operations/embedding-dimension.md") {
		t.Fatalf("error = %q, want it to name the procedure document", message)
	}

	if strings.Contains(message, "migrations/003_embedding_dim.sql.example") {
		t.Fatalf("error = %q, must not name the removed migration example", message)
	}
}

// TestCheckEmbeddingDimMismatchNarrowerColumn asserts the remedy names the
// configured dimension, not a hardcoded one, when EMBED_DIM is narrower than the
// stored column (the case that would otherwise send the operator to 1024).
func TestCheckEmbeddingDimMismatchNarrowerColumn(t *testing.T) {
	restore := stubLookup(t, 1024, nil)
	defer restore()

	err := CheckEmbeddingDim(context.Background(), nil, 512)
	if err == nil {
		t.Fatal("CheckEmbeddingDim() = nil, want a mismatch error")
	}

	message := err.Error()

	if !strings.Contains(message, "EMBED_DIM=512") {
		t.Fatalf("error = %q, want it to point at EMBED_DIM=512", message)
	}

	if strings.Contains(message, "EMBED_DIM=1024") {
		t.Fatalf("error = %q, must not point at a different dimension than configured", message)
	}
}

// TestCheckEmbeddingDimMissingColumn asserts a missing column surfaces the
// lookup error, which names the migration instead of a dimension mismatch.
func TestCheckEmbeddingDimMissingColumn(t *testing.T) {
	restore := stubLookup(t, 0, errors.New(
		"documents.embedding does not exist: is the schema migrated?",
	))
	defer restore()

	err := CheckEmbeddingDim(context.Background(), nil, 768)
	if err == nil {
		t.Fatal("CheckEmbeddingDim() = nil, want a missing-column error")
	}

	if !strings.Contains(err.Error(), "documents.embedding does not exist") {
		t.Fatalf("error = %q, want it to report the missing column", err)
	}
}

// TestCheckEmbeddingDimMissingTable asserts a missing table surfaces a lookup
// error rather than a silent pass.
func TestCheckEmbeddingDimMissingTable(t *testing.T) {
	restore := stubLookup(t, 0, errors.New(
		"documents.embedding does not exist: is the schema migrated?",
	))
	defer restore()

	err := CheckEmbeddingDim(context.Background(), nil, 768)
	if err == nil {
		t.Fatal("CheckEmbeddingDim() = nil, want a missing-table error")
	}
}

// TestVectorFromTypmod pins the pgvector typmod convention: vector(n) records
// atttypmod equal to n, so the stored width is the typmod itself. It covers the
// 768 column from migrations/001_init.sql and the 1024 width the acceptance
// criteria require.
func TestVectorFromTypmod(t *testing.T) {
	cases := []struct {
		name   string
		typmod int
		want   int
	}{
		{"768", 768, 768},
		{"1024", 1024, 1024},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := vectorFromTypmod(tc.typmod)
			if err != nil {
				t.Fatalf("vectorFromTypmod(%d) error: %v", tc.typmod, err)
			}

			if got != tc.want {
				t.Fatalf(
					"vectorFromTypmod(%d) = %d, want %d",
					tc.typmod,
					got,
					tc.want,
				)
			}
		})
	}
}

// TestVectorFromTypmodRejectsNonVector asserts a typmod that is not a pgvector
// vector(n) is reported as a type error, not a negative dimension, and that the
// error points at the procedure document.
func TestVectorFromTypmodRejectsNonVector(t *testing.T) {
	for _, typmod := range []int{-1, 0, 3} {
		_, err := vectorFromTypmod(typmod)
		if err == nil {
			t.Fatalf("vectorFromTypmod(%d) = nil error, want a non-vector error", typmod)
		}

		if !strings.Contains(err.Error(), "docs/operations/embedding-dimension.md") {
			t.Fatalf("error = %q, want it to name the procedure document", err)
		}
	}
}

// TestLookupEmbeddingDimNoRows asserts the real lookup maps pgx.ErrNoRows to
// the missing-column message, using a stub querier that returns no rows, and
// that the message points at the procedure document.
func TestLookupEmbeddingDimNoRows(t *testing.T) {
	conn := stubConn{row: stubRow{err: pgx.ErrNoRows}}

	_, err := lookupEmbeddingDim(context.Background(), conn)
	if err == nil {
		t.Fatal("lookupEmbeddingDim() = nil, want a missing-column error")
	}

	if !strings.Contains(err.Error(), "documents.embedding does not exist") {
		t.Fatalf("error = %q, want it to report the missing column", err)
	}

	if !strings.Contains(err.Error(), "docs/operations/embedding-dimension.md") {
		t.Fatalf("error = %q, want it to name the procedure document", err)
	}
}

// TestLookupEmbeddingDimReadsTypmod asserts the real lookup converts atttypmod
// to the vector dimension, using a stub querier that returns 768.
func TestLookupEmbeddingDimReadsTypmod(t *testing.T) {
	conn := stubConn{row: stubRow{typmod: 768}}

	dim, err := lookupEmbeddingDim(context.Background(), conn)
	if err != nil {
		t.Fatalf("lookupEmbeddingDim() error: %v", err)
	}

	if dim != 768 {
		t.Fatalf("lookupEmbeddingDim() = %d, want 768", dim)
	}
}

// TestLookupEmbeddingDimNonVectorTypmod asserts the real lookup surfaces the
// non-vector error when the column's typmod is not a pgvector vector(n).
func TestLookupEmbeddingDimNonVectorTypmod(t *testing.T) {
	conn := stubConn{row: stubRow{typmod: -1}}

	_, err := lookupEmbeddingDim(context.Background(), conn)
	if err == nil {
		t.Fatal("lookupEmbeddingDim() = nil, want a non-vector error")
	}

	if !strings.Contains(err.Error(), "not a pgvector vector(n) column") {
		t.Fatalf("error = %q, want it to report the column type", err)
	}
}

// stubLookup replaces the lookup seam for the duration of a test and returns a
// restore function.
func stubLookup(t *testing.T, dim int, err error) func() {
	t.Helper()

	previous := lookupEmbeddingDimFunc

	lookupEmbeddingDimFunc = func(context.Context, embeddingDimQuerier) (int, error) {
		return dim, err
	}

	return func() { lookupEmbeddingDimFunc = previous }
}

// stubConn is an embeddingDimQuerier returning a fixed row.
type stubConn struct {
	row pgx.Row
}

func (c stubConn) QueryRow(context.Context, string, ...any) pgx.Row {
	return c.row
}

// stubRow is a pgx.Row returning fixed values from Scan.
type stubRow struct {
	typmod int
	err    error
}

func (r stubRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}

	if len(dest) > 0 {
		if target, ok := dest[0].(*int); ok {
			*target = r.typmod
		}
	}

	return nil
}
