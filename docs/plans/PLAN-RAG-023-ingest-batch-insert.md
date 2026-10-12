# PLAN — RAG-023: Ingest documents larger than PostgreSQL's bind-parameter limit

**Type:** Focused defect fix (ingestion only; no retrieval, schema, or default change).
**Status:** ACTIVE.
**Baseline:** `main` at `4f8f821`.

## Project
rag-template

## Summary
`Ingester.insertChunks` (`internal/ingestion/ingestion.go`) writes every chunk of a
document in one multi-row `INSERT` with 12 bind parameters per row. PostgreSQL's
extended protocol allows at most 65535 parameters, so any document that yields more
than 5461 chunks cannot be ingested: `cmd/ingest` embeds every chunk, then fails with
`insert N chunks: extended protocol limited to 65535 parameters` and stores nothing.
With the default chunker (50 words, 20 overlap) that is roughly a 165k-word document —
a large PDF or manual.

Reproduced on `4f8f821` with a 5600-section Markdown file and real `nomic-embed-text`
embeddings: exit 1 after 37 s of embedding, 0 rows stored.

## RAG-023 — Batch the chunk insert so large documents ingest atomically

Split the insert into multi-row statements whose bind-parameter count stays under
PostgreSQL's 65535 limit, executed inside the existing replacement transaction so a
document is still replaced all-or-nothing.

### Deliverables
- `internal/ingestion/ingestion.go` — `insertChunks` writes the chunks in batches whose
  bind-parameter count never exceeds 65535, all inside the caller's transaction `tx`.
- `internal/ingestion/integration_test.go` — an integration test (existing
  `RAG_INTEGRATION=1` harness and `fakeOllama` stub) that replaces a document with more
  than 5461 chunks and asserts every chunk is stored with its `chunk_index`.
- A unit test (no database) covering the batch boundaries: 0 chunks, exactly one full
  batch, and one chunk over a full batch.

### Acceptance Criteria
- Ingesting a document that yields more than 5461 chunks succeeds and stores exactly one
  row per chunk, with the same column values a single-statement insert would write.
- Replacement stays atomic: all batches run in the existing transaction; if any batch
  fails, the transaction rolls back and the previously stored document is unchanged.
- Documents of 5461 chunks or fewer produce identical stored rows to today.
- No change to the schema, migrations, chunking, embedding, retrieval, or any default.
- `go build ./...`, `go vet ./...`, and `go test ./...` pass, and
  `RAG_INTEGRATION=1 go test ./internal/ingestion/ -run Integration` passes against a
  migrated database.

### Dependencies
- none
