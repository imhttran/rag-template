# Testing guides

This guide covers how to run the test suite, the disposable-PostgreSQL harness
used by the database integration tests, and the optional real-Ollama smoke test.

## The standard suite (no database, no network, no model)

```sh
make test        # or: go test ./...
```

`go test ./...` runs every non-optional test with no database and no model.
Database-backed tests **skip** (they do not fail) unless `RAG_INTEGRATION=1` is
set, so the standard suite stays fast and green on a machine with no PostgreSQL
and no Ollama running. Deterministic, network-free stub providers back the
in-process tests, so identical inputs always produce identical results.

## Disposable PostgreSQL (never `rag_db`)

The integration tests run against a **throwaway** database, never the development
database. The harness conventions are:

- **`RAG_TEST_DATABASE_URL`** points at the disposable database; it defaults to
  `postgres://rag:rag@127.0.0.1:5434/rag_test?sslmode=disable` (the
  `docker-compose` database exposed on port 5434).
- **Never `rag_db`.** The retrieval integration tests truncate the `documents`
  table, so they must never be pointed at `DATABASE_URL` (the dev database).
  The `.githooks/pre-push` hook probes `INTEGRATION_TEST_DB` (bound to
  `RAG_TEST_DATABASE_URL`) with `psql` first, and skips cleanly when the database
  is unreachable, so a stopped Docker daemon never blocks a push.
- **Throwaway name.** Use a clearly disposable name such as `rag_test`. Never
  create, migrate, or truncate a database named `rag_db`.
- **Refusal guard.** The RAG-025B harness in `internal/rag/e2e_db_test.go` parses
  the database name out of `RAG_TEST_DATABASE_URL` (falling back to `DATABASE_URL`
  only when the former is unset) and **refuses to run** — before any migration or
  mutation — when that name is `rag_db`. No integration path in the rag package
  can reach `rag_db`.

### Setup

```sh
make db-up            # start PostgreSQL 16 + pgvector (docker-compose)
```

### Running the integration tests

The ingestion/retrieval package integration tests:

```sh
RAG_INTEGRATION=1 \
RAG_TEST_DATABASE_URL=postgres://rag:rag@127.0.0.1:5434/rag_test?sslmode=disable \
  go test ./internal/ingestion ./internal/retrieval -run Integration -v -p 1
```

`-p 1` serializes the two packages: `internal/retrieval` truncates the whole
`documents` table, so it must not run in parallel with `internal/ingestion`.

The RAG-025B end-to-end + atomicity tests in `internal/rag` (ingest → pgvector
retrieve → generate, plus atomic-replacement rollback), against the same
disposable database:

```sh
RAG_INTEGRATION=1 \
RAG_TEST_DATABASE_URL=postgres://rag:rag@127.0.0.1:5434/rag_test?sslmode=disable \
  go test ./internal/rag -run Integration -v
```

The rag-package harness reads `RAG_TEST_DATABASE_URL`, applies every
`migrations/*.sql` idempotently, and **skips** (never fails) when
`RAG_INTEGRATION` is unset or the database is unreachable.

### Schema migration

The database tests connect to `RAG_TEST_DATABASE_URL` and apply every
`migrations/*.sql` file in name order (idempotent), so they work against a fresh
database and pick up migrations added after `001`.

### Teardown

```sh
make db-down          # stop and remove the disposable PostgreSQL container
```

## Atomic ingestion

Replacement ingest is atomic: `ReplaceDocument` embeds every chunk **before**
the transaction opens and runs all batched `INSERT`s on a single transaction.
If any batch fails, the transaction rolls back the whole replacement — including
the `DELETE` of the previous document — so a failing batch leaves the previously
stored document untouched. This contract is exercised by
`internal/ingestion`'s integration tests (`TestReplaceDocumentIntegration*`,
including `TestReplaceDocumentIntegrationLargeReplacementRollsBackOnLaterBatchFailure`)
and by `internal/rag`'s `TestE2EIngestReplacementIsAtomicDBIntegration`.

## Reliability paths (failure / cancellation / timeout)

The `internal/rag` reliability tests exercise the generation failure, request
cancellation, and request timeout paths. They run entirely in memory with the
deterministic stub generator and **do not touch any database** — so they can
never reach `rag_db`. See `internal/rag/reliability_test.go`.

## Optional: real-Ollama smoke testing (non-gating)

Real-Ollama smoke testing is **optional and reported separately**. It is **not**
required by the standard suite and **never** gates a commit, push, or merge.

When you want to exercise the real provider (a running Ollama server), run it by
hand with the network model available and record the result separately from the
suite's pass/fail:

```sh
# Example: point the commands at a running Ollama and exercise the path.
OLLAMA_URL=http://127.0.0.1:11434 go run ./cmd/rag
```

Because it needs a live model, it is excluded from `go test ./...`; the stub
provider/generator covers the deterministic, network-free path in the suite.

## Provider selection

The commands resolve the embedder and generator through the provider registry
(`EMBED_PROVIDER` / `GEN_PROVIDER`). An unregistered provider name fails fast at
`config.Load`, naming the variable and value; an unset value keeps the default
(`ollama`).
