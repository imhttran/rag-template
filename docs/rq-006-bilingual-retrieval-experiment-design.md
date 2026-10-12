# RQ-006 — Proposed bilingual retrieval experiment (design only)

**Type:** Proposed experiment design. **Not implemented.** Requires separate authorization.
**Goal:** move the 10 cross-lingual held-out cases (`en-vi`, `vi-en`) whose R@1/R@4 is `0.00`
for every installed embedding model, chunk size, and floor — i.e. an **embedding-recall** gap
(the correct section is absent from the candidate set pre-filter).

## Fixed inputs (unchanged)
- Held-out: `evals/retrieval-vi-en-expanded.json` sha256 `7f6926df…cbd2d0df` (39 cases);
  calibration: `evals/retrieval-vi-en-calibration.json` (21 cases).
- Corpus manifest: `evals/corpus-vi-en.json` sha256 `2ddbef2f…fbc87b21` (8 docs).
- Scoring and acceptance unchanged (`MIN_SIMILARITY=0.70`, rejection-first rule). The threshold
  is not tuned to pass.
- Installed models only (**no downloads**): embeddings `nomic-embed-text`, `embeddinggemma`;
  local chat/translation candidates already present (`qwen3:8b`, `qwen3:4b`, `llama3.2:3b`).
- Disposable databases only (`rag_db` never touched); one DB per arm; fresh ingest per arm.

## Arms

Each arm is measured with the **same** frozen split and scoring, capturing per-case
`source/section` rank, hit/miss, evidence, and latency; ≥3 repeats; deterministic.

### Arm A — Baseline
Production pipeline as-is: single query, `CHUNK_SIZE=50/20`, `QUERY_REWRITE=false`, floor `0.70`.
This is the control (cross-lingual R@1/R@4 = `0.00`).

### Arm B — Bilingual query expansion (existing models/tooling)
Use an **installed** local model to expand the query with translated/paraphrased terms in the
document language, then retrieve with the multi-query path already present in `cmd/eval`
(original + expanded query fused via `MultiQueryRetrieve`). No new production code: drive it
through the existing `QUERY_REWRITE`/rewrite prompt surface (or an eval-only prompt) using a
local model. Measure whether the expanded query surfaces the cross-lingual section.

Open question to resolve before implementation: the exact expansion prompt and whether the
existing `QUERY_REWRITE` path accepts a bilingual instruction without code changes; if it does
not, a **small eval-only** extension is required and must be a separately approved task.

### Arm C — Query translation, then retrieval
Translate the query into the **document language** with an installed local model, embed the
translation with `nomic-embed-text`, retrieve, and fuse with the original-language query via
the same multi-query path. Compare against Arm B to separate "translation" from "expansion".

## Metrics (per case and aggregate; operator- vs SOP-run clearly labelled)
- Overall and **per-direction** hybrid R@1/R@4/P@4, evidence (before→after), rejection.
- **Cross-lingual (`en-vi`,`vi-en`) R@1/R@4** — the primary endpoint.
- Latency (embed + model calls + run wall); model identity/version; corpus + dataset hashes;
  the exact reproducible command per arm.

## Success / stop criteria
- Advance to RQ-007 only if an arm raises **cross-lingual R@4** above `0.00` **without**
  lowering rejection below `1.00` on the calibration split and without changing the threshold.
- If no arm moves cross-lingual R@4, recommend **no retrieval change** and record that the gap
  needs an authorized multilingual embedding model.
- Stop for review on any required production change, any download, or any scoring change.

## Risks / dependencies
- Depends on RQ-005 (audit) which is complete; feeds RQ-007.
- Determinism of local generation (temperature/seed) must be pinned; if not reproducible,
  record variance.
- Do **not** promote any experimental code into production; any code path must be separately
  approved and regression-tested.
