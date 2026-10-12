# PLAN — RAG-025 closure: final acceptance note

**Type:** Final acceptance / closure record for the RAG-025 architecture-assessment and
hardening effort (RAG-025A–F).
**Status:** Closure record — candidate for `/sop-historicalize` (a separate, explicit,
operator-initiated step; **not performed here**).
**Baseline:** `main` @ `79ed678`.
**Scope of this note:** records the RAG-025A–E outcomes, the P0/P1 resolution status, the native
Go gate results, the preservation invariants, the `rag_db` safety guard, and the operator-waiver
items. This note **makes no production change**.

## 1. RAG-025A–E outcome summary

| Task | Outcome | Evidence (repository paths / symbols) |
| --- | --- | --- |
| **RAG-025A** — Provider integration | Done | `cmd/rag/main.go`, `cmd/eval/main.go`, `cmd/ingest/main.go` resolve the embedder/generator through `cfg.Embedder()` / `cfg.Generator()`; `internal/config/config.go` `Load`/`validateEmbedProvider`/`validateGenProvider`; `internal/provider/stub_test.go`, `internal/provider/registry_seam_test.go`; `docs/rag-025a-recovery-evidence.md`. |
| **RAG-025B** — End-to-end reliability | Done | `internal/rag/e2e_test.go`, `internal/rag/e2e_db_test.go` (`TestE2EIngestRetrieveGenerateDBIntegration`, `TestE2EIngestReplacementIsAtomicDBIntegration`), `internal/rag/reliability_test.go`, `docs/guides/testing.md`, `docs/rag-025b-e2e-evidence.md`. |
| **RAG-025C** — Configuration hardening | Done (confirmed in §4) | `internal/config/config_test.go` — `RAGTestDBMarker`, `requireDisposableDB`, `databaseNameFromURL`, `TestRequireDisposableDBRefusesRagDB` (+ marker/opt-in/blank/unset subtests), `TestDatabaseNameFromURL`. |
| **RAG-025D** — Architecture and documentation | Done (confirmed in §4) | `README.md`, `docs/README.md`, `docs/architecture/OVERVIEW.md`, `docs/reference/capabilities.md`, `docs/reference/configuration.md`. |
| **RAG-025E** — Cross-platform readiness | Done | `docs/operations/platform-support.md`, `docs/rag-025e-recovery-evidence.md`. |

## 2. P0/P1 resolution status

| Priority | Item | Status | Evidence |
| --- | --- | --- | --- |
| **P0** | Provider registry not wired into commands (`EMBED_PROVIDER`/`GEN_PROVIDER` ignored) | **Fixed** | `cmd/rag`, `cmd/eval`, `cmd/ingest` call `cfg.Embedder()` / `cfg.Generator()`; `internal/config/config.go` `Embedder`/`Generator` delegate to `provider.Embedder`/`provider.Generator`. Default (unset/`ollama`) delegates to the same vendor client, URL, model, and timeout as before — structural equivalence per `docs/rag-025a-recovery-evidence.md` (not a live byte-identical diff). |
| **P1** | No fail-fast provider validation at `config.Load` | **Fixed** | `validateEmbedProvider`/`validateGenProvider` in `internal/config/config.go`; error names variable and value (`EMBED_PROVIDER=%q is not a registered provider`). Covered by `TestLoad` subtests `unknown embed provider fails fast` / `unknown gen provider fails fast`. |
| **P1** | Docs overstating provider wiring / Go version / cross-platform notes; `cmd/expbilingual` undocumented | **Corrected** | `docs/reference/configuration.md`, `docs/architecture/OVERVIEW.md`, `docs/reference/capabilities.md`, `README.md`, `docs/README.md`, `docs/operations/platform-support.md`. `cmd/expbilingual` present (see §4). |
| **P2** | Docs index for RAG-024/RAG-025; README architecture accuracy | **Addressed** | `docs/README.md` links: `operations/platform-support.md`, `rag-025a-recovery-evidence.md`, `rag-025b-e2e-evidence.md`, `rag-025e-recovery-evidence.md` (all resolve, see §4). |

## 3. Native Go gate results (final tree)

Toolchain: Go **1.27.2** (module `go directive` is `go 1.26.0`; the 1.27.2 toolchain is required
for a clean `govulncheck`). Commands run from the repository root.

| Command | Exit | Result |
| --- | --- | --- |
| `gofmt -l .` | 0 | no files listed |
| `go build ./...` | 0 | PASS |
| `go vet ./...` | 0 | PASS |
| `go test ./... -count=1` | 0 | PASS (all packages ok; DB-backed tests skip without a disposable database) |
| `go test -race ./...` | 0 | PASS — full module (all packages), not only `internal/provider`/`internal/config` |
| `govulncheck ./...` | — | **See §6/§7 (operator waiver)** — must be run under the Go 1.27.2 toolchain with `govulncheck` installed; if either is unavailable in the environment it is an explicit unmet-acceptance item, never reported as passing. |
| `git diff --check` | 0 | clean |

Raw output for the runs executed here (build/vet/test/test -race/gofmt/diff-check): all exit 0;
`go test ./... -count=1` reports `ok` for every package with tests and `[no test files]` for
`cmd/expbilingual`, `internal/embedding`, `internal/generation`, `internal/ollama`;
`go test -race ./...` reports the same set with race instrumentation enabled.

**DB-backed integration note.** With `RAG_INTEGRATION` unset or no disposable PostgreSQL on
`127.0.0.1:5434`, the DB-backed tests `t.Skip` (never fail), so `go test ./...` stays green.
This note distinguishes **gates pass with DB tests skipping** from **integration tests
exercised**. Integration execution requires `make db-up` and:

```sh
RAG_INTEGRATION=1 \
RAG_TEST_DATABASE_URL=postgres://rag:rag@127.0.0.1:5434/rag_test?sslmode=disable \
  go test ./internal/ingestion ./internal/retrieval -run Integration -v -p 1

RAG_INTEGRATION=1 \
RAG_TEST_DATABASE_URL=postgres://rag:rag@127.0.0.1:5434/rag_test?sslmode=disable \
  go test ./internal/rag -run Integration -v
```

**Real-Ollama smoke testing is optional and non-gating** and is reported separately
(`docs/rag-025b-e2e-evidence.md`, "Real-Ollama smoke testing" section). It is never part of
`go test ./...` and never gates a commit, push, or merge.

## 4. RAG-025C and RAG-025D confirmation

### RAG-025C — test-DB guard

- **Guard mechanism:** `requireDisposableDB(t *testing.T)` in `internal/config/config_test.go`.
  It calls `t.Skipf` when the configured database name is `rag_db` and the explicit test-DB marker
  `RAGTestDBMarker` (`RAG_TEST_DB`) is unset; a non-`rag_db` name proceeds. `DATABASE_URL` is read
  with the same semantics as `Load` (unset/blank → `DefaultDatabaseURL`), so the default production
  target is also refused.
- **Demonstrated by executed tests:** `TestRequireDisposableDBRefusesRagDB`,
  `TestRequireDisposableDBMarkerOptsIn`, `TestRequireDisposableDBAllowsNonRagDB`,
  `TestRequireDisposableDBUnsetFallsBackToDefault`, `TestRequireDisposableDBBlankMarkerCountsAsUnset`,
  `TestDatabaseNameFromURL` — all PASS in `go test ./internal/config`.
- **Documented defaults/precedence and provider configuration:** `docs/reference/configuration.md`.
  Fail-fast provider validation lives in RAG-025A (`internal/config/config.go`).
- **Scope:** test-only; no production code. **Confirmed.**

### RAG-025D — documentation alignment

- **No contradicting claims (code cross-check):** `docs/reference/configuration.md`,
  `docs/architecture/OVERVIEW.md`, `docs/reference/capabilities.md`, `README.md`, `docs/README.md`
  describe provider wiring via `cfg.Embedder()`/`cfg.Generator()`, the fail-fast validation, the
  command set including evaluation tooling (`cmd/eval`, `cmd/expbilingual`), the hybrid retrieval
  pipeline and provider seam, and the supported Go toolchain. These match the shipped `cmd/` and
  `internal/` behaviour inspected for this note.
- **`cmd/expbilingual`:** **present** (`cmd/expbilingual/`, the RAG-024 evaluation harness) and
  referenced by the documented command set / architecture notes.
- **Docs index links:** the added links in `docs/README.md`
  (`operations/platform-support.md`, `rag-025a-recovery-evidence.md`,
  `rag-025b-e2e-evidence.md`, `rag-025e-recovery-evidence.md`) resolve to existing files.
- **Residual D note:** this closure note is itself added to the index in the same change so the
  index stays complete. **Confirmed.**

## 5. Preservation record

Method: `git status`/`git diff` scope review plus inspection of defaults.

| Protected class | Verification | Status |
| --- | --- | --- |
| Production defaults (`OLLAMA_URL`=`http://localhost:11434`, `DATABASE_URL` port `5434`, embed model `nomic-embed-text`, chat model, `REQUEST_TIMEOUT`=5m, `EMBED_DIM`=768, `MIN_SIMILARITY`=0.6, others) | `internal/config/config.go` constants and `TestLoadDefaults` | **Unchanged** — no recorded default change |
| 0.70 threshold / frozen evaluation datasets (`docs/rq-*.txt`/`.json`, `docs/rq-00*-*.md`) | diff scope review; not modified or deleted | **Untouched** |
| RAG-023 / RAG-024 archives (`docs/plans/PLAN-RAG-023*.md`, `docs/plans/PLAN-RAG-024.md`, `docs/rq-008-final-acceptance.md`, `docs/rq-024-operator-recovery-evidence.md`) | diff scope review; not modified or deleted | **Untouched** |
| Deferred multilingual backlog (`docs/rq-007-deferred-bilingual-backlog.md`) | not reopened; not modified | **Untouched** |
| User-owned files (`docs/plans/PLAN-RAG-Phase-23.md`, `docs/plans/PLAN-RAG-Phase-23b.md`, `docs/sop-defect-historicalize-stale-approval.md`) | present; not modified/removed by RAG-025 | **Preserved** |
| `migrations/*.sql` | not modified | **Untouched** |
| Production database `rag_db` | never targeted by any test path (see §6 guard) | **Untouched** |

Diff scope note: the RAG-025 changes are source, test, and documentation files (modified:
`README.md`, `cmd/{eval,ingest,rag}/main.go`, `docs/README.md`, `docs/architecture/OVERVIEW.md`,
`docs/reference/{capabilities,configuration}.md`, `internal/config/{config.go,config_test.go}`,
`internal/provider/{provider.go,provider_test.go}`; added: `docs/guides/testing.md`,
`docs/operations/platform-support.md`, `docs/plans/PLAN-RAG-025.md`, the RAG-025 evidence records,
`internal/provider/{stub_test.go,registry_seam_test.go}`,
`internal/rag/{e2e_test.go,e2e_db_test.go,reliability_test.go}`).
Pre-existing user-owned documents in the working tree are preserved.

## 6. `rag_db` safety guard and disposable database

- **Test guard:** `requireIntegrationDB` in `internal/rag/e2e_db_test.go` parses the database name
  from `RAG_TEST_DATABASE_URL` (falling back to `DATABASE_URL` only when the former is unset) and
  calls `t.Fatalf` **before** connecting, migrating, or mutating anything when the name is
  `rag_db`, with the message:
  `refusing to run the destructive integration test against the production database "rag_db";
  point RAG_TEST_DATABASE_URL at a disposable database (for example rag_test)`.
- **No test path can reach `rag_db`:** the reliability and in-memory E2E tests touch no database;
  DB tests refuse `rag_db` and `t.Skip` when `RAG_INTEGRATION` is unset or the database is
  unreachable.
- **Disposable database identity:** `rag_test` on port `5434`
  (`postgres://rag:rag@127.0.0.1:5434/rag_test?sslmode=disable`).
- **Config-layer mirror:** `requireDisposableDB` in `internal/config/config_test.go` (see §4).

## 7. Residual gaps and candidate operator waivers

| Item | Reason | Owning layer | Disposition |
| --- | --- | --- | --- |
| `govulncheck ./...` under Go 1.27.2 | Requires the 1.27.2 toolchain and the `govulncheck` binary; if unavailable in the closure environment it cannot be executed. | Development environment / toolchain | **Executed — PASS** (`No vulnerabilities found.`, §9). The earlier “candidate operator waiver” applies **only** if the 1.27.2 toolchain / `govulncheck` binary are unavailable. |
| Real-Ollama smoke test | Requires a live Ollama server; explicitly optional and non-gating. | Manual/operator | **Waived** by plan design (`docs/rag-025b-e2e-evidence.md`). |
| DB-backed integration execution | Requires `make db-up` disposable PostgreSQL; DB tests skip without it. | Test harness / operator | Standard gates pass with DB tests skipping; operator decides whether to run integration. |
| Operator approval of the plan | The plan is **ACTIVE** (`plan-rag-025`; an earlier draft of this note said `PROPOSED`) and RAG-025F reached `LOCAL_DONE`; closure acceptance and historicalization still require operator approval. | Operator (human gate via SOP) | **Human gate** — see §8. |
| Historicalization | `/sop-historicalize` is a separate, explicit, operator-initiated step. | Operator (human gate via SOP) | **Not performed here.** |

## 8. Historicalization and operator gate

- `/sop-historicalize` is **not** performed by RAG-025F or any RAG-025 stage. It is a separate,
  explicit, operator-initiated step to be run only after approval. This note is the candidate
  input for it.
- **Lifecycle correction (an earlier draft of this note said `Status: PROPOSED`):** the plan is
  now **ACTIVE** (`plan-rag-025`) and RAG-025F reached `LOCAL_DONE` (gate PASS). RAG-025F's own
  criterion — *every acceptance criterion satisfied or explicitly waived by the operator* — is a
  human gate for closure and `/sop-historicalize`. Any criterion not backed by executed evidence
  is listed in §7; none is silently asserted as satisfied.

  ## 9. Executed validation matrix (supplemental, operator-run)

  **All six tasks (RAG-025A–F) reached `LOCAL_DONE`; SOP reports 6/6 complete with a final gate PASS.**
  These results supersede the §3/§7 “govulncheck candidate waiver” and the
  “DB tests skip” caveat: they were executed on the final tree.

  | Command | Result |
  | --- | --- |
  | `gofmt -l .` | clean |
  | `go build ./...` | PASS |
  | `go vet ./...` | PASS |
  | `go test ./... -count=1` | PASS (16/16 packages ok; no FAIL) |
  | `go test -race -count=1 ./...` | PASS (16/16) |
  | `GOTOOLCHAIN=go1.27.2 govulncheck ./...` | **PASS — “No vulnerabilities found.”** (executed; not a waiver) |
  | `git diff --check` | exit 0 |
  | DB integration: `RAG_INTEGRATION=1` on disposable `rag_021_test` | `internal/retrieval` Integration ok; `internal/ingestion` Integration ok; `internal/rag` DBIntegration ok |

  The disposable database used for this run was `rag_021_test` on `127.0.0.1:5432`
  (`RAG_TEST_DATABASE_URL`/`DATABASE_URL`); `rag_db` was never targeted.

  ### 9.1 RAG-025D documentation corrections (supplemental)

  After F, three RAG-025D documentation issues were corrected (D was not rerun):
  (1) `docs/operations/platform-support.md` is now linked from `README.md` and
  `docs/README.md`; (2) the garbled Go-version sentence in `README.md` now distinguishes
  the module’s minimum Go version (`go 1.26.0`) from the toolchain used for a clean
  `govulncheck` (`GOTOOLCHAIN=go1.27.2`); (3) the `docs/README.md` “Start here” list
  indentation was fixed. `git diff --check` remains clean.

  ## 10. Historical `PARTIAL` outcomes and recovery evidence

  Every RAG-025 task reached `LOCAL_DONE` with gate `PASS`, but each also carries an SOP
  outcome **`PARTIAL`** flag — a verification-**metadata** artifact (the plan’s acceptance
  criteria are prose, so SOP reports “unverifiable: no deterministic check supplied”), **not**
  an implementation failure. These are recorded, not concealed:

  | Task | SOP outcome | Recovery / evidence |
  | --- | --- | --- |
  | RAG-025A | PARTIAL | `docs/rag-025a-recovery-evidence.md` |
  | RAG-025B | PARTIAL | `docs/rag-025b-e2e-evidence.md` (+ disposable-DB E2E verified) |
  | RAG-025C | PARTIAL | test-DB guard + `docs/reference/configuration.md` (verified, §4) |
  | RAG-025D | PARTIAL | docs updated (§1/§4) + §9.1 corrections |
  | RAG-025E | PARTIAL | `docs/operations/platform-support.md` + `docs/rag-025e-recovery-evidence.md` |
  | RAG-025F | PARTIAL | this closure note + §9 |

  ## 11. Final architecture assessment

  - **What it is.** A deliberately small, framework-free RAG template: `cmd/` wires
    single-responsibility `internal/` packages; the retrieval pipeline is one concrete
    implementation shared by `cmd/rag`, `cmd/eval`, and the evaluation-only `cmd/expbilingual`.
  - **Verified seams.** Loader registry; provider registry (now **wired** into the commands,
    with fail-fast validation); ingestion with an atomic delete-then-insert replacement and a
    schema/dimension guard; hybrid retrieval (vector + FTS, RRF fusion, section dedup/expansion,
    `KeepSimilar` floor); citations/answerability; deterministic tests and a disposable-DB E2E.
  - **Boundaries.** Commands own no retrieval/storage logic; schema is owned by `migrations/`;
    the provider is a registry entry, not a pipeline edit.
  - **Not implemented:** OCR (RAG-008), genealogy extraction (RAG-016); deferred bilingual
    production retrieval (`docs/rq-007-deferred-bilingual-backlog.md`).

  ## 12. Recommended next backlog

  1. **Harden the acceptance criteria into machine-checkable commands** (the root cause of the
     recurring `PARTIAL` metadata outcome).
  2. **Rejection-preserving cross-lingual retrieval** (RQ-006 D2 shape) + an **authorized
     multilingual embedding model** (`docs/rq-007-deferred-bilingual-backlog.md`).
  3. **Windows-native support** — address the POSIX `sh`/`Makefile` gap
     (`docs/operations/platform-support.md`).
  4. **Phase-23b** (PDF/OCR/page provenance) — frozen; requires operator authorization.
  5. **`agentic-sop` verification hardening** — SOP repeatedly cannot transition a satisfied task
     without a mutation (RQ-006, RAG-025B); a defect report + regression tests would help.
