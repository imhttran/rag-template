# RAG-025E — Recovery evidence (cross-platform readiness)

**Type:** Recovery/acceptance evidence. The original SOP outcome is preserved untouched; this
document records the missing deliverable being produced directly, with independent verification.
**No SOP runtime state was modified. RAG-025E was not rerun.** Baseline: `main` @ `79ed678`.

## Original SOP outcome (preserved)

- Task **RAG-025E** — SOP status **`LOCAL_DONE`**, gate **`PASS`**.
- Recorded outcome: **`PARTIAL`** — SOP flagged
  **`deliverable-1 not met: "docs/operations/platform-support.md" not present`**, plus
  `criterion-1…3`/`objective-acceptance` "unverifiable: no deterministic check supplied".
- **Missing deliverable:** `docs/operations/platform-support.md` (the run made no repository
  mutation; the docs-only gate is therefore trivially green).

## Recovery actions

1. Re-read RAG-025E's acceptance criteria (macOS/Linux/Windows classification with repo evidence;
   documentation-only; `git diff --check`).
2. Inspected the actual requirements from the repository (not assumptions): `go.mod` (`go 1.26.0`,
   no cgo), `docker-compose.yml` (`pgvector/pgvector:pg17`, port `5434`), `Makefile` targets,
   `scripts/*.sh` (`#!/bin/sh`), `internal/config/config.go` defaults
   (`OLLAMA_URL=http://localhost:11434`, `OLLAMA_EMBED_MODEL=nomic-embed-text`,
   `DATABASE_URL=…@127.0.0.1:5434/rag`), `README.md` requirements, and the RAG-025B integration
   test's `RAG_TEST_DATABASE_URL`/`rag_db` guard.
3. **Created `docs/operations/platform-support.md`** classifying macOS / Linux (incl. Linux Mint)
   / Windows-native / WSL2, listing shared prerequisites (Go toolchain, PostgreSQL+pgvector,
   Docker, Ollama/provider, POSIX shell + Makefile), gaps, and platform-specific validation
   commands — distinguishing **Verified** (Linux, this environment) from **Expected (untested)**
   (macOS, WSL2) and **Gap** (Windows-native), and explicitly **not** claiming Windows-native
   compatibility.

## Acceptance criteria and verification

| Criterion | Verification | Result |
| --- | --- | --- |
| Each of macOS, Linux, Windows classified with repo evidence | `docs/operations/platform-support.md` — Linux **Verified**; macOS **Expected (untested)**; Windows-native **Gap**; WSL2 **Expected (untested)** | ✅ |
| Documentation-only; no code change beyond authorization | Only `docs/operations/platform-support.md` added | ✅ |
| Validation: `git diff --check` | exit 0 | ✅ |
| Commands verified against repo configuration | defaults (`OLLAMA_URL`, `DATABASE_URL` port `5434`, embed model), `docker-compose.yml` (`pgvector/pg17`), `Makefile`/`scripts` behavior, `RAG_TEST_DATABASE_URL` guard all matched | ✅ |
| Referenced paths exist | `scripts/{eval-model-sweep,sweep}.sh`, `Makefile`, `docker-compose.yml`, `internal/rag/e2e_db_test.go` present | ✅ |

## Attestation

RAG-025E remains **`LOCAL_DONE`** with no pending approvals; it was **not rerun** and no SOP state
was edited. This document records the recovery evidence only. `rag_db`, frozen datasets, archived
plans, and user-owned documents are untouched.
