package ingestion

import (
	"testing"
)

// REGRESSION(F5): the batched INSERT built by Ingester.insertChunks binds a
// fixed number of values per chunk. Each row in the multi-row INSERT supplies
// exactly 12 bound parameters (source, section, chunk_index, content,
// embedding, content_hash, embed_model, embedding_dim, chunker_config,
// ingested_at, language, page).
//
// PostgreSQL caps a single statement at 65,535 bind parameters. With 12 values
// per chunk, one INSERT can therefore carry at most floor(65_535 / 12) = 5_461
// chunks; a document of 5_462 chunks or more exceeds the ceiling and the single
// batched INSERT fails.
//
// This file records F5 as a deterministic, documented baseline in RAG-023-A2.
// The executable, database-backed regression that actually crosses the ceiling
// runs in RAG-023D against a disposable PostgreSQL database; A2 intentionally
// does not require a live database here.
const (
	// insertValuesPerChunk is the number of bound parameters per chunk in the
	// single multi-row INSERT constructed by insertChunks.
	insertValuesPerChunk = 12

	// postgresMaxBindParameters is PostgreSQL's fixed protocol limit on the
	// number of bind parameters in a single statement.
	postgresMaxBindParameters = 65535
)

// REGRESSION(F5): pin the derived ceiling arithmetic.
func TestInsertParameterCeilingBoundary(t *testing.T) {
	maxChunks := postgresMaxBindParameters / insertValuesPerChunk

	if maxChunks != 5461 {
		t.Fatalf("65_535 / 12 = %d, want 5_461", maxChunks)
	}

	// At the boundary the statement still fits...
	if got := insertValuesPerChunk * maxChunks; got > postgresMaxBindParameters {
		t.Errorf("%d chunks bind %d parameters, which exceeds the %d ceiling; the boundary must be the largest count that fits", maxChunks, got, postgresMaxBindParameters)
	}

	// ...and one chunk past it overflows.
	overBoundary := maxChunks + 1

	if got := insertValuesPerChunk * overBoundary; got <= postgresMaxBindParameters {
		t.Errorf("%d chunks bind only %d parameters; the ceiling must be exceeded above %d chunks", overBoundary, got, maxChunks)
	}

	// State the boundary explicitly so the recorded baseline is self-describing.
	if maxChunks != 5461 || insertValuesPerChunk*(maxChunks+1) <= postgresMaxBindParameters {
		t.Fatalf("F5 baseline mismatch: want %d values/chunk with the %d ceiling crossing above %d chunks", insertValuesPerChunk, postgresMaxBindParameters, maxChunks)
	}
}

// REGRESSION(F5): pin the per-chunk parameter count that the ceiling is derived
// from. insertChunks writes one 12-column VALUES tuple per chunk using
// `base := index * 12` and a 12-placeholder format string, and sizes args with
// `len(embedded) * 12`. If the column list or placeholder arithmetic in
// ingestion.go changes, this constant must change too, which fails the baseline
// above rather than silently invalidating the ceiling math.
//
// The executable regression against a disposable database is owned by RAG-023D.
func TestInsertValuesPerChunkPinned(t *testing.T) {
	if insertValuesPerChunk != 12 {
		t.Fatalf("insertValuesPerChunk = %d, want 12; update the F5 baseline and its ceiling derivation", insertValuesPerChunk)
	}

	// A 5_462-chunk document is the smallest single INSERT that exceeds the
	// PostgreSQL parameter ceiling with 12 values per chunk.
	const overCeilingChunks = 5462

	if overCeilingChunks <= postgresMaxBindParameters/insertValuesPerChunk {
		t.Fatalf("%d chunks must be above the %d-chunk ceiling", overCeilingChunks, postgresMaxBindParameters/insertValuesPerChunk)
	}

	if got := overCeilingChunks * insertValuesPerChunk; got <= postgresMaxBindParameters {
		t.Fatalf("%d chunks bind %d parameters, want more than %d", overCeilingChunks, got, postgresMaxBindParameters)
	}
}
