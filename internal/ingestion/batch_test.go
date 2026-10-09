package ingestion

import (
	"strings"
	"testing"

	"rag-template/internal/chunking"
)

// makeEmbedded builds n embedded chunks with deterministic values so batch
// boundary assertions can name exact rows.
func makeEmbedded(n int) []embeddedChunk {
	embedded := make([]embeddedChunk, 0, n)

	for index := 0; index < n; index++ {
		embedded = append(embedded, embeddedChunk{
			chunk: chunking.Chunk{
				Section: "Fees",
				Index:   index,
				Content: "chunk content",
			},
			vector: []float64{1, 2, 3},
		})
	}

	return embedded
}

// TestChunkInsertBatchesEmpty asserts zero chunks produce no statement at all,
// matching the previous no-op insert.
func TestChunkInsertBatchesEmpty(t *testing.T) {
	batches := chunkInsertBatches("source.md", Provenance{}, nil)

	if len(batches) != 0 {
		t.Fatalf("batches for 0 chunks = %d, want 0", len(batches))
	}
}

// TestChunkInsertBatchesExactlyOneFullBatch asserts exactly chunksPerBatch rows
// fit in a single statement whose parameter count stays under the limit.
func TestChunkInsertBatchesExactlyOneFullBatch(t *testing.T) {
	embedded := makeEmbedded(chunksPerBatch)

	batches := chunkInsertBatches("source.md", Provenance{}, embedded)

	if len(batches) != 1 {
		t.Fatalf("batches = %d, want 1", len(batches))
	}

	if got := batches[0].rows; got != chunksPerBatch {
		t.Fatalf("rows = %d, want %d", got, chunksPerBatch)
	}

	if got, want := len(batches[0].args), chunksPerBatch*paramsPerChunk; got != want {
		t.Fatalf("args = %d, want %d", got, want)
	}

	if params := len(batches[0].args); params >= maxBindParams {
		t.Fatalf(
			"bind parameters = %d, want strictly less than %d",
			params,
			maxBindParams,
		)
	}

	statement := batches[0].sql

	if !strings.HasPrefix(statement, "INSERT INTO documents ") {
		t.Fatalf("statement = %q, want it to start with the insert", statement)
	}

	if !strings.Contains(statement, "$1, $2, $3, $4, $5::vector, $6, $7, $8, $9, $10, $11, $12") {
		t.Fatalf("statement missing the first row placeholders: %q", statement)
	}

	last := chunksPerBatch * paramsPerChunk
	if !strings.Contains(statement, "($"+itoa(last-11)) {
		t.Fatalf("statement missing the final row starting at $%d", last-11)
	}

	if strings.Contains(statement, "$0") {
		t.Fatalf("statement contains $0: %q", statement)
	}
}

// TestChunkInsertBatchesOneOverFullBatch asserts one chunk over a full batch
// yields two batches whose rows sum to the input length and whose parameters
// both stay under the limit.
func TestChunkInsertBatchesOneOverFullBatch(t *testing.T) {
	total := chunksPerBatch + 1
	embedded := makeEmbedded(total)

	batches := chunkInsertBatches("source.md", Provenance{}, embedded)

	if len(batches) != 2 {
		t.Fatalf("batches = %d, want 2", len(batches))
	}

	if got := batches[0].rows; got != chunksPerBatch {
		t.Fatalf("first batch rows = %d, want %d", got, chunksPerBatch)
	}

	if got := batches[1].rows; got != 1 {
		t.Fatalf("second batch rows = %d, want 1", got)
	}

	if got := batches[0].rows + batches[1].rows; got != total {
		t.Fatalf("total rows = %d, want %d", got, total)
	}

	for index, batch := range batches {
		if params := len(batch.args); params >= maxBindParams {
			t.Fatalf(
				"batch %d bind parameters = %d, want strictly less than %d",
				index,
				params,
				maxBindParams,
			)
		}

		if got, want := len(batch.args), batch.rows*paramsPerChunk; got != want {
			t.Fatalf("batch %d args = %d, want %d", index, got, want)
		}

		if !strings.Contains(batch.sql, "$1, $2, $3, $4, $5::vector, $6, $7, $8, $9, $10, $11, $12") {
			t.Fatalf("batch %d does not start at $1: %q", index, batch.sql)
		}
	}
}

// TestChunkInsertBatchesRowsMatchSingleStatement asserts the row values written
// by the batched path equal those the previous single-statement insert wrote,
// so documents at or below the limit are byte-identical.
func TestChunkInsertBatchesRowsMatchSingleStatement(t *testing.T) {
	provenance := Provenance{
		ContentHash:   "hash",
		EmbedModel:    "fake",
		Dimension:     768,
		ChunkerConfig: "size=50",
		Language:      "en",
	}

	embedded := makeEmbedded(3)
	batches := chunkInsertBatches("source.md", provenance, embedded)

	if len(batches) != 1 {
		t.Fatalf("batches = %d, want 1", len(batches))
	}

	args := batches[0].args

	if args[0] != "source.md" {
		t.Fatalf("row 0 source = %v, want source.md", args[0])
	}

	if args[1] != "Fees" || args[3] != "chunk content" {
		t.Fatalf("row 0 section/content = %v/%v", args[1], args[3])
	}

	if args[2] != 0 {
		t.Fatalf("row 0 chunk_index = %v, want 0", args[2])
	}

	if args[10] != "en" || args[11] != nil {
		t.Fatalf("row 0 language/page = %v/%v, want en/nil", args[10], args[11])
	}
}

// itoa is a tiny local integer formatter so the test needs no extra imports.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}

	var digits []byte

	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}

	return string(digits)
}
