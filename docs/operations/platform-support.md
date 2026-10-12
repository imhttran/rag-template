# Platform support

This page classifies the platforms the RAG template runs on, its prerequisites, and its gaps.
Support is stated explicitly as one of:

- **Verified** — exercised on this platform (commands run, results observed).
- **Expected (untested)** — no platform-specific blockers are evident from the repository, but it
  has **not** been exercised here.
- **Gap** — a known limitation or missing prerequisite.

> **Scope of the evidence.** Only **Linux** was exercised while writing this page (the CI/dev
> environment). **macOS** and **Windows** are **Expected (untested)** unless a "Gap" is noted.
> **Windows-native compatibility is not claimed** — it is a documented gap.

## Summary

| Platform | Status | Notes |
| --- | --- | --- |
| **Linux** (incl. Linux Mint) | **Verified** | `go build`/`go test`, `docker compose`, POSIX `sh` scripts, `make`, `psql` |
| **macOS** | Expected (untested) | Same POSIX toolchain; Docker Desktop; not exercised here |
| **Windows (native)** | **Gap** | No native `make`/`sh`; see Windows section |
| **Windows via WSL2** | Expected (untested) | Recommended Windows path (Ubuntu + Docker Desktop + Ollama) |

The Go code is portable: the module has **no cgo** and no OS-specific syscalls, and its only
non-standard dependencies are `github.com/jackc/pgx/v5` (pure Go) and
`github.com/ledongthuc/pdf` (pure-Go PDF text layer). So there is no platform-specific C toolchain
to install.

## macOS

- **Status:** Expected (untested).
- **Prerequisites:** Go (**≥ 1.26**, see below); Docker Desktop (bundled PostgreSQL + pgvector)
  or a local PostgreSQL with the `vector` extension; Ollama for ingest/answer; `make`, a POSIX
  `sh`, `psql`, and `jq` (`fmt-json`).
- **Notes:** The toolchain (`sh`, `make`, `psql`, `docker compose`) is native on macOS. No
  repository-specific macOS step is known.

## Linux (including Linux Mint)

- **Status:** **Verified** (this environment).
- **Prerequisites:** Go (**≥ 1.26**); Docker + Docker Compose (or a local PostgreSQL with
  `vector`); Ollama for ingest/answer; `make`, POSIX `sh`, `psql`, `jq`.
- **Notes:** Linux Mint is Ubuntu/Debian-based; the Debian/Ubuntu packages (`golang-go`,
  `docker.io`/Docker CE, `postgresql-client` for `psql`, `jq`, `make`) satisfy the prerequisites.
  Verified here: `go build ./...`, `go test ./...`, `docker compose` config, the `sh` scripts, the
  `Makefile`, and `psql`.

## Windows (native)

- **Status:** **Gap — not supported as-is.**
- **Limitations (evidence-based):**
  - `scripts/eval-model-sweep.sh` and `scripts/sweep.sh` start with `#!/bin/sh` and are POSIX
    shell; `Makefile` targets (`make test`, `make db-up`, `make integration`, …) shell out to
    `sh`, `docker compose`, `git`, `jq`, and `psql` — none of which is native to `cmd.exe`/
    PowerShell.
  - `make db-schema` invokes `psql "$DATABASE_URL"` — the PostgreSQL client is required.
- **What does work natively:** `go build ./...` and `go test ./...` run with the Go toolchain on
  Windows; the Docker Desktop engine can run `pgvector/pgvector:pg17`.
- **Do not assume** the shell/Makefile workflow, the sweep scripts, or the disposable-database
  integration commands work on native Windows without WSL2 or Git-Bash.

## Windows via WSL2 (recommended for Windows)

- **Status:** Expected (untested).
- Run the POSIX toolchain inside a WSL2 Linux distro (e.g. Ubuntu) so `sh`, `make`, `psql`, `jq`,
  and `docker compose` behave as on Linux. Use **Docker Desktop with the WSL2 backend**; point
  Ollama either at an Ollama server installed in WSL2 or at a Windows-hosted Ollama via
  `OLLAMA_URL` (e.g. the host IP). Keep the repository on the Linux filesystem (not `/mnt/c`)
  for correct file modes and performance.

## Shared requirements

### Go toolchain
- `go.mod` declares **`go 1.26.0`**; use **Go ≥ 1.26**.
- The repo is built/tested with the Go toolchain auto-selection (`GOTOOLCHAIN`); a **newer patch
  toolchain** may be needed for a clean `govulncheck` (the RAG-024/RAG-023 validation used
  `GOTOOLCHAIN=go1.27.2`). Pin it explicitly for reproducible vulnerability scans.

### PostgreSQL + pgvector
- A PostgreSQL 16/17 server **with the `vector` (pgvector) extension** (0.6+).
- Schema is owned by `migrations/00{1..4}_*.sql` (idempotent). Apply with `make db-schema`
  (requires `psql`) or let the Docker entrypoint apply them.
- Set `DATABASE_URL` to the target database (default
  `postgres://rag:rag@127.0.0.1:5434/rag?sslmode=disable`). **Never point destructive tests at a
  shared/production database.**

### Docker (or equivalent)
- `docker-compose.yml` runs `pgvector/pgvector:pg17`, publishes **5434→5432**, and mounts
  `./migrations` into `/docker-entrypoint-initdb.d`. `make db-up` runs
  `docker compose up -d --wait` then `make db-schema`.
- Equivalent setup: any reachable PostgreSQL+pgvector with `DATABASE_URL` set; the Docker path is
  a convenience, not a requirement.

### Ollama / provider
- A local Ollama at `OLLAMA_URL` (default `http://localhost:11434`) with an **embedding model**
  (default `nomic-embed-text`) and a **chat model** for answer/eval features.
- The provider registry (`EMBED_PROVIDER`/`GEN_PROVIDER`) selects the implementation; only
  `ollama` is registered today. An unknown provider fails fast at `config.Load`.
- `make test` (unit) needs **no** Ollama; ingest and `cmd/rag` do.

### POSIX shell, Makefile, and helper tools
- `scripts/*.sh` are `#!/bin/sh`; the `Makefile` uses POSIX `make` with `-include .env`.
- Additional tools used by targets: `docker` + `docker compose`, `psql` (db-schema),
  `jq` (`fmt-json`), `git`.

## Known unsupported workflows and gaps

- **Windows-native shell/Makefile/sweep workflow** — unsupported without WSL2 or Git-Bash+make.
- **OCR for scanned PDFs (RAG-008)** — not implemented (text-layer PDFs only).
- **Genealogy entity extraction (RAG-016)** — not implemented.
- **macOS / Windows validation** — not exercised in this environment; treat as untested.
- **Disposable-database integration tests** require a real PostgreSQL+pgvector; there is no mock
  database, and the RAG-025B integration test **refuses** the `rag_db` name and reads
  `RAG_TEST_DATABASE_URL` (falling back to `DATABASE_URL`).

## Platform-specific validation commands

Run from the repository root. These are the checks used to classify the platforms above.

```sh
# Go build and the standard (no-DB, no-network) suite — any platform with Go ≥ 1.26
go build ./...
go test ./...

# Same, pinned to the toolchain used for RAG-024/025 validation
GOTOOLCHAIN=go1.27.2 go build ./... && GOTOOLCHAIN=go1.27.2 go test ./...

# Database-backed integration (Linux/macOS/WSL2; needs Docker or a local PG+pgvector)
make db-up            # docker compose up -d --wait && make db-schema
RAG_INTEGRATION=1 go test ./internal/retrieval/ ./internal/ingestion/ -run Integration -p 1 -v
RAG_TEST_DATABASE_URL='postgres://rag:rag@127.0.0.1:5434/<disposable>?sslmode=disable' \
  RAG_INTEGRATION=1 go test ./internal/rag/ -run DBIntegration -count=1 -v

# Formatting / hygiene
gofmt -l .
git diff --check
```
