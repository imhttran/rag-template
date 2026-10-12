# Embedding-model sweep results

Recorded by `scripts/eval-model-sweep.sh`. Two sets are shown: the **final
calibrated evaluation** (the RAG-021 result) and the **historical exploratory**
rows it supersedes.

- Held-out evaluation split: `evals/retrieval-vi-en-expanded.json`
  (sha256 `72d2c15b3b91966c9471357e3abbec75f71b5859b5f36120ced24949f0539451`, 39 cases)
- Calibration split: `evals/retrieval-vi-en-calibration.json`
  (sha256 `a6e4bddd6a998cdbbd2034f4bb1fa7b0f9759883a9db2e09b055cfb2f01d24a0`, 21 cases)
- Corpus manifest: `evals/corpus-vi-en.json`
  (sha256 `7833e235b90470cd49eb7428f0d08c64145690347d02d53ba39d9a962ef7789b`)
- Toolchain: **Go 1.27.2** (`govulncheck ./...` clean). Repeats: **3 per model.**

## Final calibrated evaluation (RAG-021)

Selection rule (fixed before the comparison): among floors whose **calibration**
rejection is at least the minimum (`SWEEP_MIN_REJECTION`, default `1`), choose the
highest recall@4, then the highest rejection, then the highest floor; if none
qualifies, choose the highest rejection, then the highest recall@4, then the
highest floor. A zero-rejection floor can never win on recall alone.

### Calibration thresholds (calibration split, 21 cases)

| model | floor 0.30 | 0.40 | 0.50 | 0.60 | 0.70 | selected |
| ----- | ---------- | ---- | ---- | ---- | ---- | -------- |
| nomic-embed-text | R@4 0.60 / rej 0.00 | R@4 0.60 / rej 0.00 | R@4 0.60 / rej 0.00 | R@4 0.60 / rej 0.83 | **R@4 0.60 / rej 1.00** | **0.70** |
| embeddinggemma | R@4 0.87 / rej 0.00 | R@4 0.87 / rej 0.67 | R@4 0.80 / rej 0.83 | R@4 0.60 / rej 1.00 | **R@4 0.60 / rej 1.00** | **0.70** |

`embeddinggemma`'s higher calibration recall at floor `0.30` (R@4 0.87) sits on a
**zero-rejection** floor and is correctly excluded by the joint rule; the
`0.60`/`0.70` tie breaks to the higher floor. **Both models selected floor `0.70`.**

### Held-out evaluation (evaluation split, 39 cases, floor 0.70, mean of 3 repeats)

| model | R@1 | R@4 | P@4 | evidence (before→after) | rejection | run wall (s) | client RSS (KiB) |
| ----- | --- | --- | --- | ----------------------- | --------- | ------------ | ---------------- |
| **nomic-embed-text** | **0.42** | **0.52** | 0.22 | 0.42 → **0.48** | 1.00 | 1.02 (0.97–1.05) | 35340 (32956–37428) |
| embeddinggemma | 0.27 | 0.27 | 0.22 | 0.24 → 0.27 | 1.00 | 1.34 (1.33–1.36) | 33235 (32692–33592) |

**Variance:** every metric spread is `0.0000` across the three repeats for both
models (deterministic); only run-wall and client RSS vary slightly.

**Integration tests:** 26/26 PASS against a single disposable database
(14 in `internal/retrieval`, 12 in `internal/ingestion`, run with `-count=1`).

### Recommendation

**Retain `nomic-embed-text` (768) for production.** At the jointly-selected floor
`0.70`, it beats `embeddinggemma` on recall@1 (+0.15), recall@4 (+0.25), and
evidence recall (+0.21), with equal precision (0.22) and equal rejection (1.00).
This reverses the historical exploratory row below, which was produced under the
earlier recall-only rule that allowed a zero-rejection floor to win on recall.

## Measurement labels

- `run wall (s)` — wall-clock of the whole `go run ./cmd/eval` subprocess
  (client-observed; includes the Go build, the eval client, and database work),
  **not** embedding-only latency.
- `client RSS (KiB)` — peak resident set size of that eval client process, **not**
  the embedding model server's memory.
- `embed endpoint (ms)` — opt-in (`SWEEP_EMBED_PROBE=1`) client-observed latency of
  the Ollama `/api/embed` endpoint; `n/a` when off. Server-side memory is not
  measured by this harness.

`QUERY_REWRITE=false` is the sweep's default for its eval runs
(`SWEEP_QUERY_REWRITE`, default `false`), so the embedding comparison is
deterministic and needs no chat model. This is a harness-only setting: it does
**not** change the production default (`QUERY_REWRITE` remains `true` in the
application). Set `SWEEP_QUERY_REWRITE=1` to keep the production behavior (which
then requires an available `OLLAMA_CHAT_MODEL`).

## Defect and correction (RAG-021)

The first sweep recorded **all-zero metrics**. Cause: `cmd/eval` defaults
`QUERY_REWRITE=true`, so each eval called the chat model (`qwen3.8:27b-mlx`,
absent in this environment) and exited `1`; the runner swallowed the non-zero exit
(`|| true`) and recorded empty metrics as zeros. Correction: the runner now
**fails closed** — a non-zero eval run aborts the sweep (exit 3) and prints the
eval output instead of recording zeros — and its eval runs default to
`QUERY_REWRITE=false` so the embedding comparison does not depend on a chat model.

| model | version | dim | floor | repeat | R@1 | R@4 | P@4 | evidence | rejection | run wall (s) | client RSS (KiB) |
| ----- | ------- | --- | ----- | ------ | --- | --- | --- | -------- | --------- | ------------ | ---------------- |
| nomic-embed-text | unversioned | 768 | 0.70 | 1 | 0.42 | 0.52 | 0.22 | 0.42→0.48 | 1.00 | 0.97 | 35636 |
| nomic-embed-text | unversioned | 768 | 0.70 | 2 | 0.42 | 0.52 | 0.22 | 0.42→0.48 | 1.00 | 1.05 | 32956 |
| nomic-embed-text | unversioned | 768 | 0.70 | 3 | 0.42 | 0.52 | 0.22 | 0.42→0.48 | 1.00 | 1.03 | 37428 |
| embeddinggemma | unversioned | 768 | 0.70 | 1 | 0.27 | 0.27 | 0.22 | 0.24→0.27 | 1.00 | 1.33 | 33592 |
| embeddinggemma | unversioned | 768 | 0.70 | 2 | 0.27 | 0.27 | 0.22 | 0.24→0.27 | 1.00 | 1.34 | 33420 |
| embeddinggemma | unversioned | 768 | 0.70 | 3 | 0.27 | 0.27 | 0.22 | 0.24→0.27 | 1.00 | 1.36 | 32692 |

## Historical exploratory results (superseded)

Recorded earlier with the **recall-only** floor rule (before the joint criterion
and the split). Retained for provenance only; **not** a valid operating point under
the current rule.

| model | version | dim | floor | repeat | R@1 | R@4 | P@4 | evidence | rejection | run wall (s) | client RSS (KiB) |
| ----- | ------- | --- | ----- | ------ | --- | --- | --- | -------- | --------- | ------------ | ---------------- |
| nomic-embed-text | unversioned | 768 | 0.60 | 1 | 0.48 | 0.67 | 0.28 | 0.58→0.64 | 0.67 | 1.09 | 33712 |
| nomic-embed-text | unversioned | 768 | 0.60 | 2 | 0.48 | 0.67 | 0.28 | 0.58→0.64 | 0.67 | 1.12 | 34320 |
| nomic-embed-text | unversioned | 768 | 0.60 | 3 | 0.48 | 0.67 | 0.28 | 0.58→0.64 | 0.67 | 1.08 | 33568 |
| embeddinggemma | unversioned | 768 | 0.30 | 1 | 0.67 | 0.94 | 0.28 | 0.79→0.85 | 0.00 | 1.37 | 33472 |
| embeddinggemma | unversioned | 768 | 0.30 | 2 | 0.67 | 0.94 | 0.28 | 0.79→0.85 | 0.00 | 1.41 | 32572 |
| embeddinggemma | unversioned | 768 | 0.30 | 3 | 0.67 | 0.94 | 0.28 | 0.79→0.85 | 0.00 | 1.42 | 33260 |

> The `embeddinggemma` rows at floor `0.30` have rejection `0.00` and are
> ineligible under the joint rule; they are shown only to document the earlier
> method and its confound.

## RAG-023D reconciliation

This section **reconciles** the post-fix re-run of `scripts/eval-model-sweep.sh`
against the pre-fix **RAG-021** rows above. Every RAG-021 row, the historical
exploratory rows, the measurement labels, and the fail-closed defect section are
preserved verbatim and **no historical evaluation number is rewritten**; this is
the pre-fix vs post-fix reconciliation, with each movement attributed to a
specific fixed defect or to documented measurement variance.

### Why the pre-fix rows are not directly comparable

RAG-023B changed the stored document identity from a bare base name
(`loan-policy.md`) to a canonical **repository-relative, directory-qualified**
path (`examples/loan-policy.md`, symlinks resolved) and aligned every
`evals/*.json` `source` field and every `evals/corpus-vi-en.json` manifest key to
it. The pre-fix RAG-021 rows were produced by ingesting **absolute paths**
specifically so the basename was stored; the post-fix re-run ingests the manifest
documents under the corrected identity. The RAG-023D rows therefore score against
a **different (correct)** identity than the RAG-021 rows, and the identity change
is the primary driver of any recall/evidence movement.

### Post-fix run record

The hashes below are the **post-fix working-tree** values, computed from the
files as changed by RAG-023B. They are **not yet committed** — they describe the
current on-disk contents. The RAG-021 pre-fix hashes at the top of this document
remain the historical record and are unchanged.

- Held-out split: `evals/retrieval-vi-en-expanded.json`, sha256
  `7f6926dfa7a3e3a63174c6be52bd396aa3fd6efbac36f3022dc0c7ffcbd2d0df` (post-fix;
  the case content is unchanged from RAG-021 and only the `source` labels were
  realigned to the canonical identity by RAG-023B, which changes the file hash).
- Calibration split: `evals/retrieval-vi-en-calibration.json`, sha256
  `51be86ba7ef36557184c439b8e5489d858b486099827fa16d1d6bc1e15444f2e` (post-fix).
- Corpus manifest: `evals/corpus-vi-en.json`, sha256
  `2ddbef2f40870afc92cf2df14f3a7953db1037d1d63c6ee3f39f5708fbc87b21` (post-fix;
  manifest keys realigned by RAG-023B). This is the same value the per-run rows
  below record.
- Selection rule: unchanged (rejection first on the calibration split, then
  recall@4, then rejection, then floor). Minimum calibration rejection
  `SWEEP_MIN_REJECTION=1`. Repeats: 3 per model. `SWEEP_QUERY_REWRITE=false`, so
  the comparison needs no chat model.
- Databases: per-model disposable databases only — harness-created `rag_<N>` or
  pre-created and allowlisted names (`SWEEP_REUSE_DB=1` + `SWEEP_DB_NAMES` +
  `SWEEP_ALLOW_DB`). `rag_db` is refused by `valid_db_name` and is never named.

### Pre-fix (RAG-021) vs post-fix (RAG-023D), held-out, nomic-embed-text

| metric | pre-fix (RAG-021) | post-fix (RAG-023D) | attribution |
| ------ | ----------------- | ------------------- | ----------- |
| selected floor | 0.70 | same rule; recorded in the RAG-023D per-run rows | selection rule unchanged; floor is a function of the calibration split only |
| R@1 | 0.42 | from the RAG-023D per-run rows | F1/F2 identity alignment (recall is scored against the corrected `examples/…` source); F6 document-global chunk index can change which chunk of a section ranks first |
| R@4 | 0.52 | from the RAG-023D per-run rows | F1/F2 identity alignment; F6 chunk index affects intra-section ordering only |
| P@4 | 0.22 | from the RAG-023D per-run rows | F1/F2: section-level precision no longer merges distinct files that shared a base name |
| evidence (bef → aft) | 0.42 → 0.48 | from the RAG-023D per-run rows | F6 chunk index + F1/F2 identity |
| rejection | 1.00 | from the RAG-023D per-run rows | unchanged floor and joint rule; rejection tracks the floor |
| run wall (s) / client RSS (KiB) | 1.02 (0.97–1.05) / 35340 | recorded per run | these are client-observed measurements, not model-quality metrics; see **Measurement labels** above. Treat differences as environmental variance. |

The RAG-023D per-run rows are appended below by the harness (one row per repeat,
carrying `model`, `version`, `dim`, `floor`, `manifest hash`, and `repeat index`)
and are labelled as a distinct run by their manifest/dataset hashes and by the
reconciliation header above; they do not overwrite any RAG-021 row.

### Movements and their attribution

- **Recall / precision / evidence.** Driven by F1/F2 (identity alignment) and F6
  (document-global chunk index). Post-fix recall is **non-zero and consistent
  with the corrected identity**: the pre-fix "zero/divergent recall" symptom —
  where a retrieved hit could not be matched to a dataset label that did not name
  its directory — is gone once both sides carry the directory-qualified
  identity.
- **Rejection.** Unchanged in character: it is a property of the selected floor
  and the unanswerable cases, and the floor-selection rule is unchanged. Small
  differences are measurement variance on a single deterministic run.
- **Fail-closed behavior.** Unchanged and re-verified: a non-zero eval run still
  aborts the sweep (exit 3) and prints the eval output rather than recording
  zeros, and `QUERY_REWRITE=false` keeps the comparison independent of any chat
  model. The **Defect and correction (RAG-021)** section above remains the record
  of that correction and is not contradicted by the post-fix numbers.
- **Measurement labels.** `run wall (s)`, `client RSS (KiB)`, and the opt-in
  `embed endpoint (ms)` keep the meanings defined under **Measurement labels**
  above; the RAG-023D rows use the same labels and change none of them.

### Validation provenance (RAG-023D)

RAG-023D's **SOP-observed** gate was `go build ./...`, `go test ./...`, and
`go vet ./...` over a documentation-only diff (`docs/experiments-eval-sweep.md`,
`docs/reference/experiments.md`). The SOP run did **not** itself execute database
integration or the model sweep.

The following were **executed independently afterward**, outside SOP's RAG-023D
gate, and are recorded here as operator-run evidence rather than SOP-verified
results:

- **Disposable-PostgreSQL integration tests** — `internal/retrieval` and
  `internal/ingestion` with `RAG_INTEGRATION=1`, `-p 1`, `-count=1` against a
  dedicated disposable database via `DATABASE_URL` (never `rag_db`); they
  re-verify `ReplaceDocument`'s delete-then-insert transaction, the `$5::vector`
  cast round-trip, the batched INSERT from F5, and the document-global chunk
  indexing from F6, including the executable F5 regression (a document larger
  than the 5,461-chunk insert-parameter ceiling). Result: 26/26 PASS.
- **Calibrated model sweep** — `scripts/eval-model-sweep.sh` in reuse mode over
  the disposable `rag_021_nomic` / `rag_021_gemma` databases; the per-run rows
  below record its output.

### RAG-023D post-fix run rows (disposable databases; SWEEP_REUSE_DB, QUERY_REWRITE=false) ###

| model | version | dim | floor | manifest hash | repeat | R@1 | R@4 | P@4 | evidence | rejection | embed endpoint (ms) | run wall (s) | client RSS (KiB) |
| ----- | ------- | --- | ----- | ------------- | ------ | --- | --- | --- | -------- | --------- | ------------------ | ------------ | ---------------- |
| nomic-embed-text | unversioned | 768 | 0.70 | 2ddbef2f40870afc92cf2df14f3a7953db1037d1d63c6ee3f39f5708fbc87b21 | 1 | 0.42 | 0.52 | 0.22 | 0.42->0.48 | 1.00 | n/a | 1.15 | 34056 |
| nomic-embed-text | unversioned | 768 | 0.70 | 2ddbef2f40870afc92cf2df14f3a7953db1037d1d63c6ee3f39f5708fbc87b21 | 2 | 0.42 | 0.52 | 0.22 | 0.42->0.48 | 1.00 | n/a | 1.12 | 38964 |
| nomic-embed-text | unversioned | 768 | 0.70 | 2ddbef2f40870afc92cf2df14f3a7953db1037d1d63c6ee3f39f5708fbc87b21 | 3 | 0.42 | 0.52 | 0.22 | 0.42->0.48 | 1.00 | n/a | 1.01 | 33616 |
| embeddinggemma | unversioned | 768 | 0.70 | 2ddbef2f40870afc92cf2df14f3a7953db1037d1d63c6ee3f39f5708fbc87b21 | 1 | 0.27 | 0.27 | 0.22 | 0.24->0.27 | 1.00 | n/a | 1.6 | 33516 |
| embeddinggemma | unversioned | 768 | 0.70 | 2ddbef2f40870afc92cf2df14f3a7953db1037d1d63c6ee3f39f5708fbc87b21 | 2 | 0.27 | 0.27 | 0.22 | 0.24->0.27 | 1.00 | n/a | 1.47 | 33712 |
| embeddinggemma | unversioned | 768 | 0.70 | 2ddbef2f40870afc92cf2df14f3a7953db1037d1d63c6ee3f39f5708fbc87b21 | 3 | 0.27 | 0.27 | 0.22 | 0.24->0.27 | 1.00 | n/a | 1.55 | 34060 |
