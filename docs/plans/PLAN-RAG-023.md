# PLAN — RAG-023: Correctness hardening (12 verified defects)

**Type:** Focused correctness-hardening plan (RAG runtime + ingestion + evaluation + docs).
**Status:** Active governed plan (refactored: broad RAG-023A split into A1–A5).
**Derived from:** the 12 operator-reported rag-template defects (verified read-only).
**Baseline:** `main` at `4f8f821` (includes RAG-022).
**Supersedes:** none. **Does not** rewrite historical evaluation results and **does not** remove provider/filter capabilities.

## Project
rag-template

## Summary
Baseline and fix twelve verified correctness defects across source-name identity, ingestion
limits, chunk ordering, RAG output, language parity, and citation parsing; reconcile
affected evaluation scores without rewriting history; and document (not implement) the
provider/filter/observability/dead-surface cleanup. Baseline tasks (A1–A5) add tests or a
documentation record only — **no production implementation changes**. Production embedding
defaults are unchanged; destructive tests use disposable databases only, never `rag_db`.

## RAG-023A — F1/F2 source-identity regression baselines (A1)

Characterize the source-name identity defect without a database. Files: `cmd/ingest`.

### Deliverables
- `cmd/ingest/sourcename_repro_test.go` — table-driven tests over `sourceName`.
- `cmd/eval/dataset_identity_repro_test.go` — asserts the dataset/ingest identity mismatch.

### Acceptance Criteria
- `sourceName("examples/loan-policy.md")` returns `examples/loan-policy.md` (relative dir kept) — current behavior pinned.
- `sourceName("/a/policy.md")` and `sourceName("/b/policy.md")` both return `policy.md` (absolute collision) — pinned.
- A test asserts every `evals/*.json` `source` is a bare base name (e.g. `loan-policy.md`) while a relative ingest stores `examples/loan-policy.md`, i.e. the identities differ — pinned.
- Each case carries a `// REGRESSION(F1)` / `// REGRESSION(F2)` marker; tests pass now and are expected to fail once RAG-023B corrects identity.
- Validation: `go test ./cmd/ingest/... ./cmd/eval/... && go vet ./...`

### Dependencies
- none

## RAG-023-A2 — F5/F6 ingestion and chunking regression baselines (A2)

Characterize the insert-parameter ceiling and per-section chunk numbering. Files:
`internal/ingestion`, `internal/chunking`.

### Deliverables
- `internal/chunking/chunking_repro_test.go` — repeated-heading numbering baseline.
- `internal/ingestion/insert_repro_test.go` — insert-parameter ceiling baseline.

### Acceptance Criteria
- A test over `chunking.FromSections` with two sections sharing a title asserts `Index` restarts at 0 in each section (two `0`s, two `1`s) — current behavior pinned (`// REGRESSION(F6)`).
- F5 is recorded as a deterministic documented baseline: the single `INSERT` uses 12 values/chunk and PostgreSQL's 65,535-parameter ceiling is exceeded above 5,461 chunks; the executable regression runs in RAG-023D against a disposable database (`// REGRESSION(F5)`).
- Validation: `go test ./internal/chunking/... ./internal/ingestion/... && go vet ./...`

### Dependencies
- none

## RAG-023-A3 — F3/F4 JSON output and language-parity baselines (A3)

Characterize the discarded answer in JSON mode and the eval/rag language divergence. Files:
`internal/observability`, `cmd/eval`.

### Deliverables
- `internal/observability/json_answer_repro_test.go` — asserts the JSON record carries no answer field (`// REGRESSION(F3)`).
- `cmd/eval/language_parity_repro_test.go` — asserts `pipelineOptions()` leaves `Language` empty while `rag` sets `cfg.Language` (`// REGRESSION(F4)`).

### Acceptance Criteria
- The JSON record schema test asserts the emitted record has timing/count fields and **no** answer field — current behavior pinned.
- The language test asserts `evaluator.pipelineOptions().Language == ""` while `cmd/rag` passes `cfg.Language` — pinned.
- Validation: `go test ./internal/observability/... ./cmd/eval/... && go vet ./...`

### Dependencies
- none

## RAG-023-A4 — F7/F8 citation regression baselines (A4)

Characterize the citation parser's `" - "`/page handling and the eval duplicate parser.
Files: `internal/citations`, `cmd/eval`.

### Deliverables
- `internal/citations/citations_repro_test.go` — section-title-with-`" - "` baseline (`// REGRESSION(F7)`).
- `cmd/eval/extract_claims_repro_test.go` — page-citation skip baseline (`// REGRESSION(F8)`).

### Acceptance Criteria
- `parseCitation("Source - Late Payments - Grace Period")` returns not-ok (citation dropped) — current behavior pinned.
- `extractCitedClaims("[manual.pdf - Page 3 - p.3]")` yields a section of `Page 3 - p.3` (no page handling) — pinned.
- Validation: `go test ./internal/citations/... ./cmd/eval/... && go vet ./...`

### Dependencies
- none

## RAG-023-A5 — F9–F12 architecture inventory (documentation only) (A5)

Record a decision inventory for the dead/unreachable surface. **No code is changed.**

### Deliverables
- `docs/plans/PLAN-RAG-023-decisions.md` — one entry per F9–F12 with recommendation and roadmap rationale.

### Acceptance Criteria
- Each of F9 (provider registry), F10 (unused `Filter`), F11 (`HumanReporter`), F12 (dead wrappers; `parseEntailment` duplicate) has a recorded recommendation (*wire up* / *document only* / *remove*) citing the roadmap (RAG-004/005, RAG-009/011).
- Provider and metadata-filter capabilities are recommended for removal only if the roadmap supersedes them.
- Any removal is proposed as a separate task requiring operator authorization.
- Validation: documentation only (no code gate).

### Dependencies
- none

## RAG-023B — Document identity, ingestion, and chunk ordering

Fixes F1, F2, F5, F6. (Unchanged scope from the original brief; now depends on A1 and A2.)

### Deliverables
- `cmd/ingest/main.go` + `cmd/ingest/sourcename_repro_test.go` — canonical repository-relative identity (symlinks resolved, never a bare base name); the normalization test asserts the corrected behavior.
- `internal/ingestion/ingestion.go` — batched INSERT (≈1,000 rows/statement, atomic within the caller's transaction).
- `internal/chunking/chunking.go` — document-global chunk index; bump `chunkerConfigVersion`.
- **Every retrieval dataset and the corpus manifest** aligned to the canonical identity — explicitly `evals/retrieval.json`, `evals/retrieval-vi-en.json`, `evals/retrieval-vi-en-expanded.json`, `evals/retrieval-vi-en-calibration.json`, and the manifest `evals/corpus-vi-en.json` — covering **all** `source` fields and **all** manifest keys (none may remain a bare base name).
- `Makefile`, `scripts/sweep.sh`, `scripts/eval-model-sweep.sh` aligned to the canonical identity.
- Existing characterization tests (`cmd/eval/dataset_identity_repro_test.go`, `internal/chunking/chunking_repro_test.go`, `internal/ingestion/insert_repro_test.go`) updated to assert the corrected behavior and to pass.

### Acceptance Criteria
- Every `evals/*.json` retrieval dataset `source` field AND every `evals/corpus-vi-en.json` manifest key carry the canonical directory-qualified identity; `TestExpandedDatasetAgainstManifest` and `TestDatasetSourcesCarryDirectoryContext` pass for **all** datasets, including `evals/retrieval-vi-en-calibration.json`.
- Fresh ingest through any entry point yields the canonical identity; eval recall is non-zero.
- Symlinked input stores the resolved target identity; `/a/policy.md` and `/b/policy.md` stay distinct; repeated ingestion is idempotent.
- >5,461-chunk documents ingest; repeated headings expand per section in order.
- `go test ./...` is green.

### Dependencies
- RAG-023A
- RAG-023-A2

## RAG-023C — RAG output, language, and citation correctness

Fixes F3, F4, F7, F8. (Unchanged scope; now depends on A3 and A4.)

### Deliverables
- `cmd/rag/main.go` — JSON mode preserves the answer.
- `cmd/eval/main.go` — `pipelineOptions` sets `Language: cfg.Language`; `extractCitedClaims` reuses the shared parser.
- `internal/citations/citations.go` — exported parser; trailing segment is a page only when it matches `p.N`.

### Acceptance Criteria
- `OBSERVABILITY_FORMAT=json` shows the answer.
- Eval keyword search matches `rag` under `CORPUS_LANGUAGE=vi`.
- `"Source - Late Payments - Grace Period"` keeps the full section.
- `"[manual.pdf - Page 3 - p.3]"` yields page.
- One shared parser.
- A3/A4 baselines updated.

### Dependencies
- RAG-023-A3
- RAG-023-A4

## RAG-023D — Integrated validation and evaluation-score reconciliation

(Unchanged scope; depends on B and C.)

### Deliverables
- Reconciliation note in `docs/reference/experiments.md` and `docs/experiments-eval-sweep.md` (pre-fix vs post-fix; historical numbers preserved).
- Re-run integration tests and the calibrated sweep on disposable databases.

### Acceptance Criteria
- Integration tests run against a real disposable PostgreSQL (never `rag_db`) and pass.
- The calibrated sweep re-runs on disposable databases; post-fix numbers are consistent with the corrected identity.
- No historical evaluation number is rewritten; every change is reconciled with an explanation.

### Dependencies
- RAG-023B
- RAG-023C

## RAG-023E — Architecture assessment and decision record (no implementation)

F9–F12 assessment. (Now depends on A5; no code changes; implementation requires separate operator authorization.)

### Deliverables
- Consolidated decision record referencing `PLAN-RAG-023-decisions.md`.

### Acceptance Criteria
- F9–F12 each have a recorded disposition with roadmap rationale; no code modified; any removal is a separately authorized task.

### Dependencies
- RAG-023-A5
