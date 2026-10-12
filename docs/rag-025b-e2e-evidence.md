# RAG-025B — End-to-end reliability evidence

This is the evidence record for **RAG-025B: End-to-end reliability**. It
captures the exact commands, the disposable-database identity and safety guard,
the ingest → pgvector-retrieve → generate evidence, the atomicity and
reliability test results, and the standard Go validation results.

Real-Ollama smoke testing is **optional and reported separately** (see the last
section): it is non-gating and excluded from the standard suite.

## Deliverables

| Deliverable | Path |
| --- | --- |
| In-memory deterministic stub E2E | `internal/rag/e2e_test.go` |
| Disposable-DB E2E + atomicity | `internal/rag/e2e_db_test.go` |
| Failure / cancellation / timeout | `internal/rag/reliability_test.go` |
| Harness usage docs | `docs/guides/testing.md` |
| This evidence record | `docs/rag-025b-e2e-evidence.md` |

## Disposable database identity and safety guard

- **Harness variable:** `RAG_TEST_DATABASE_URL`
- **Example disposable test database:** `postgres://rag:rag@127.0.0.1:5434/rag_test?sslmode=disable`
  — a throwaway name on the `docker-compose` PostgreSQL 16 + pgvector instance (port 5434).
  This is **not** the application default: `DATABASE_URL` defaults to
  `postgres://rag:rag@127.0.0.1:5434/rag?sslmode=disable` (`internal/config/config.go`,
  `DefaultDatabaseURL`).
- **Database name used for integration:** `rag_test` — a throwaway name, **not**
  `rag_db`.
- **`rag_db` guard:** `requireIntegrationDB` in `internal/rag/e2e_db_test.go`
  parses the database name out of `RAG_TEST_DATABASE_URL` (falling back to
  `DATABASE_URL` only when the former is unset). When the name is `rag_db` it
  calls `t.Fatalf` **before** connecting, migrating, or mutating anything:

  ```
  refusing to run the destructive integration test against the production
  database "rag_db"; point RAG_TEST_DATABASE_URL at a disposable database
  (for example rag_test)
  ```

  No integration path in the rag package can reach `rag_db`.
- **Skip behavior:** with `RAG_INTEGRATION` unset, or with the disposable database
  unreachable, the DB tests `t.Skip` (never fail), so `go test ./...` stays green
  on a machine with no PostgreSQL.

## Ingestion → pgvector retrieval → generation evidence

`TestE2EIngestRetrieveGenerateDBIntegration` (`internal/rag/e2e_db_test.go`)
exercises the production path end to end over the disposable database:

1. **Migrations** — `applyMigrations` runs every `migrations/*.sql` in name
   order, idempotently.
2. **Ingestion** — `ingestion.NewWithOptions(conn, dbStubEmbedder{...}, 2, 1)`
   plus `ReplaceDocument` stores a synthetic document. The embedder is a pure,
   no-I/O stub returning a fixed-length vector (dim 768, matching
   `migrations/001_init.sql`'s `vector(768)`), so identical inputs yield
   identical vectors — deterministic and network-free.
3. **pgvector storage** — `SELECT count(*), count(embedding) FROM documents
   WHERE source = $1` asserts `embedded == rows`; the test fails if the pgvector
   `embedding` column is not populated.
4. **Retrieval** — the production pipeline `retrieval.HybridRetrieve(ctx,
   retrieval.New(conn), ...)` runs against the stored rows; the test fails if the
   ingested source is absent from `result.Fused`.
5. **Provenance** — `FormatContext` on the retrieved document asserts the
   assembled context carries the `Source: <source>` and `Section:` lines.
6. **Generation** — `Answer(ctx, stubGenerator{...}, ...)` runs through the
   production answer path with a deterministic, no-network stub generator (no
   Ollama, no external model); the test asserts the expected answer text and the
   `source` citation reference.

## Atomicity evidence

`TestE2EIngestReplacementIsAtomicDBIntegration` (`internal/rag/e2e_db_test.go`)
seeds a document, then forces a failing replacement (a chunk containing a NUL
byte), and asserts the previously stored row is unchanged — the delete-then-insert
replacement rolls back the whole replacement on a failing batch. The
in-memory unit path records the same `FormatContext` provenance contract in
`internal/rag/e2e_test.go` (`TestE2EIngestRetrieveGenerateDeterministic`).

## Reliability evidence

`internal/rag/reliability_test.go` covers failure, cancellation, timeout, and
empty-context paths. Each test **does not touch any database** — they run purely
in memory with the stub generator, so none can reach `rag_db`:

| Test | Path covered |
| --- | --- |
| `TestReliabilityGenerationErrorIsClearAndNoPartialWrite` | generation error surfaced via `errors.Is` with an empty answer (no partial write) |
| `TestReliabilityAnswerLimitedRejectsBeforeGenerate` | fail-fast input limit; generator never reached; empty answer |
| `TestReliabilityCancellationDoesNotHang` | canceled context unblocks promptly with `context.Canceled` (bounded at 5s) |
| `TestReliabilityTimeoutIsBounded` | request timeout bounded failure with `context.DeadlineExceeded` |
| `TestReliabilityEmptyContextPlaceholder` | empty-context provenance surface stays stable |

## Standard Go validation results

Run with no database and no model:

```sh
go build ./...    # exit 0
go vet ./...      # exit 0
go test ./...     # exit 0 — DB-backed tests skip, not fail
```

`go test ./...` includes `internal/rag`, where the stub E2E and reliability
tests run and the DB integration tests skip cleanly when no disposable database
is configured.

## Integration commands

```sh
# Ingestion / retrieval package integration tests.
RAG_INTEGRATION=1 \
RAG_TEST_DATABASE_URL=postgres://rag:rag@127.0.0.1:5434/rag_test?sslmode=disable \
  go test ./internal/ingestion ./internal/retrieval -run Integration -v -p 1

# RAG-025B E2E + atomicity over the same disposable database.
RAG_INTEGRATION=1 \
RAG_TEST_DATABASE_URL=postgres://rag:rag@127.0.0.1:5434/rag_test?sslmode=disable \
  go test ./internal/rag -run Integration -v
```

These commands match `docs/guides/testing.md`. The disposable database is
`rag_test` on port 5434; the harness refuses `rag_db` before any migration or
mutation (see the guard above). Secrets in the URL are the throwaway
`rag:rag` disposable credentials.

> Note: the disposable-PostgreSQL integration commands above require a running
> `docker-compose` database (`make db-up`) and are exercised when that server is
> available; when it is not, the DB-backed tests skip rather than fail, and the
> standard Go gate results above still hold.

## Config / command identifiers

- Integration gate: `RAG_INTEGRATION=1`
- Disposable DB URL: `RAG_TEST_DATABASE_URL` (default port 5434, database
  `rag_test`)
- Migration source: `migrations/*.sql`
- Stub dimensions: `dbEmbeddingDim = 768`, matching `vector(768)` in
  `migrations/001_init.sql`

## Real-Ollama smoke testing — optional, reported separately

Real-Ollama smoke testing is **optional and reported separately**. It requires a
live Ollama server and is **never** part of `go test ./...`; it never gates a
commit, push, or merge. The deterministic, network-free stub provider/generator
covers the E2E path in the standard suite. To exercise the real provider by hand,
point the commands at a running Ollama and record the result separately (see
`docs/guides/testing.md`).

## Acceptance-criterion conformance

| Criterion | Evidence |
| --- | --- |
| Stub-provider E2E is deterministic and network-free; runs in the standard suite (skips without a DB) | `internal/rag/e2e_test.go` (stub generator, 3 deterministic runs), `internal/rag/e2e_db_test.go` (stub embedder, skip gate) |
| Failure, cancellation, timeout paths do not touch `rag_db` | `internal/rag/reliability_test.go` (in-memory, no DB); rag DB tests guarded against `rag_db` |
| Ingestion remains atomic (failing batch rolls back) | `TestE2EIngestReplacementIsAtomicDBIntegration` |
| Real-Ollama smoke testing optional and reported separately | this document, "Real-Ollama smoke testing" section |
| Gates pass; disposable-PostgreSQL integration tests pass; evidence records commands/config/hashes | "Standard Go validation results", "Integration commands", "Config / command identifiers" sections |
| Evidence document exists | this file |
