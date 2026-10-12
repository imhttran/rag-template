# Experiment log

Evidence behind the current defaults. `scripts/sweep.sh` (`make sweep`) runs
`cmd/eval` once per configuration and prints the Overall averages, one row per
configuration, so a setting is changed because the numbers moved rather than
because a couple of answers looked better.

Rows are directional, not a controlled A/B across every knob: each axis was run
at a particular point in the project's history, and the baseline is noted per
section. Metrics are hybrid retrieval recall/precision at K, evidence recall
averaged over cases that declare `evidence` (before → after section expansion),
and the similarity-only rejection rate on the unanswerable cases.

## Setup

- Corpus: `examples/loan-policy.md`, `large-loan-policy.md`,
  `member-services-guide.md`, `commercial-servicing-manual.md`.
- Cases: `evals/retrieval.json` — 43 cases, 37 answerable, 6 unanswerable.
- Models: `nomic-embed-text` for embeddings (the default), `qwen3.8:27b-mlx` for
  the chat model. The chat model is needed only for query rewriting and the
  answer-level metrics (answerability gate, fact judge, LLM rerank); the
  embedding-model comparison runs with `QUERY_REWRITE=false` and needs no chat
  model (see "Calibrated embedding-model comparison" below).

## Chunk size and overlap

Baseline: `QUERY_REWRITE=false`, `FINAL_K=2`. `CHUNK_SIZE` × `CHUNK_OVERLAP`:

| size/overlap | R@1      | R@2      | R@4      | P@4  | Evidence (bef → aft) |
| ------------ | -------- | -------- | -------- | ---- | -------------------- |
| 50/0         | 0.66     | 0.86     | **0.95** | 0.56 | 0.72 → 0.79          |
| 50/20        | **0.68** | **0.86** | 0.94     | 0.55 | **0.79 → 0.86**      |
| 50/40        | 0.64     | 0.85     | 0.85     | 0.57 | 0.73 → 0.82          |
| 100/0        | 0.64     | 0.83     | 0.92     | 0.52 | 0.78 → 0.83          |
| 100/20       | 0.64     | 0.83     | 0.94     | 0.51 | 0.77 → 0.83          |
| 100/40       | 0.64     | 0.83     | 0.94     | 0.54 | 0.77 → 0.83          |
| 200/0        | 0.65     | 0.82     | 0.91     | 0.47 | 0.82 → 0.82          |
| 200/20       | 0.65     | 0.82     | 0.92     | 0.48 | 0.82 → 0.82          |
| 200/40       | 0.65     | 0.82     | 0.92     | 0.47 | 0.82 → 0.82          |

- `50/20` is best on R@1 and evidence recall; `50` beats `100` beats `200` on
  R@1/R@2.
- Overlap 40 hurts at size 50 (`step = 10` produces 128 chunks for the manual
  alone and dilutes the fused ranking); overlap 20 beats 0 on evidence recall.
- At `200`, expansion changes nothing (0.82 → 0.82): sections are one or two
  chunks there, so there is nothing to expand.

On the earlier three-document corpus (all sections under 50 words, so every
size produced one chunk per section) this axis was flat — which is why the new
long-section manual was added before drawing conclusions.

Decision: default `CHUNK_SIZE=50`, `CHUNK_OVERLAP=20`.

## TOP_K

Baseline: `QUERY_REWRITE=false`, `FINAL_K=2`.

| TOP_K | R@2  | R@8  | P@8  | Evidence (bef → aft) |
| ----- | ---- | ---- | ---- | -------------------- |
| 2     | 0.86 | —    | —    | 0.79 → 0.86          |
| 4     | 0.86 | —    | —    | 0.79 → 0.86          |
| 8     | 0.83 | 0.96 | 0.43 | 0.77 → 0.83          |

`TOP_K=2` gains nothing over 4 and loses the R@4 headroom; `TOP_K=8` buys R@8
but depresses R@2 and halves precision.

Decision: keep `TOP_K=4`.

## FINAL_K

Baseline: `QUERY_REWRITE=false`.

| FINAL_K | R/P at K=1,2,4 | Evidence (bef → aft) |
| ------- | -------------- | -------------------- |
| 1       | unchanged      | 0.61 → 0.68          |
| 2       | unchanged      | 0.79 → 0.86          |
| 3       | unchanged      | **0.83 → 0.91**      |

The recall/precision columns do not move with `FINAL_K` — they score fused
rankings, not the expanded set — but evidence recall moves cleanly: `FINAL_K` is
how many sections reach the model.

Decision: default `FINAL_K=3`.

## Query rewrite

Baseline: `FINAL_K=2`, chunk `50/20`. Axis toggles `QUERY_REWRITE`:

| QUERY_REWRITE | R@1  | R@2  | R@4  | P@4  | Evidence (bef → aft) |
| ------------- | ---- | ---- | ---- | ---- | -------------------- |
| false         | 0.68 | 0.86 | 0.94 | 0.55 | 0.79 → 0.86          |
| true          | 0.73 | 0.87 | 0.95 | 0.52 | 0.77 → 0.87          |

Keeping the original versus replacing it with the rewrite (`EVAL_REWRITE_ONLY`),
measured on current defaults:

| Mode                 | R@1  | P@1  | R@4  | P@4  |
| -------------------- | ---- | ---- | ---- | ---- |
| original only        | 0.68 | 0.81 | 0.94 | 0.55 |
| rewritten only       | 0.69 | 0.78 | 0.91 | 0.61 |
| original + rewritten | 0.73 | 0.86 | 0.93 | 0.52 |

Replacing the original with its rewrite holds K=1 recall but loses recall at
K=4; keeping both is best at K=1.

Decision: default `QUERY_REWRITE=true` (one extra chat-model call per query).

## LLM rerank

Baseline: `QUERY_REWRITE=false`, `FINAL_K=2`. Axis toggles `EVAL_LLM_RERANK`:

| EVAL_LLM_RERANK | rerank 4→2 recall / precision |
| --------------- | ----------------------------- |
| false           | — (K=2 baseline 0.86 / 0.70)  |
| true            | 0.91 / 0.68                   |

Recall up, precision flat to slightly down, for one chat-model call per
answerable case. Not promoted.

## MIN_SIMILARITY

Baseline: `QUERY_REWRITE=true`, `FINAL_K=3`.

| MIN_SIMILARITY | R@1  | R@2  | R@4  | P@4  | Evidence (bef → aft) | Rejection  |
| -------------- | ---- | ---- | ---- | ---- | -------------------- | ---------- |
| 0.4            | 0.74 | 0.89 | 0.95 | 0.44 | 0.81 → 0.94          | 0/6 (0.00) |
| 0.6 (default)  | 0.70 | 0.87 | 0.95 | 0.52 | 0.80 → 0.93          | 2/6 (0.33) |
| 0.8            | 0.41 | 0.47 | 0.49 | 0.48 | 0.43 → 0.49          | 6/6 (1.00) |

The floor trades recall against rejection almost linearly: `0.4` accepts every
unanswerable case, `0.6` rejects two, `0.8` rejects all six — but at `0.8`
recall collapses (R@1 0.70 → 0.41). A single similarity threshold cannot
separate a near-miss unanswerable question from an answerable one, because the
near-miss question retrieves genuinely close chunks. That is what the
answerability gate is for; the threshold stays at `0.6`.

## Generation judge

Baseline: `QUERY_REWRITE=true`, `FINAL_K=3`, cloud chat model. With
`EVAL_FACT_JUDGE=true` the generation metrics are reported for every answerable
case (the K columns differ from the `false` row only because the rewrite is
nondeterministic, not because of the judge):

| Metric                | Score        |
| --------------------- | ------------ |
| Generated fact recall | 50/55 (0.91) |
| Groundedness          | 35/37 (0.95) |
| Citation validity     | 89/89 (1.00) |
| Citation entailment   | 78/88 (0.89) |

Every citation pointed at retrieved material, but one cited claim in nine was
not supported by the section it cited.

## Bugs the sweep surfaced

- `CHUNK_OVERLAP=0` was silently coerced to the default 20: `envIntOrDefault`
  treated any non-positive value as unset. At size 50 the manual ingested 48
  chunks for overlap 0 and 20 alike. Fixed with `envNonNegativeIntOrDefault`
  (overlap 0 is meaningful).
- `TOP_K=2` double-counted: `ks` held `2` twice, so K=2 averaged to `1.72` and
  rejections to `4/6`. Fixed by sorting and deduplicating `ks`.

## Re-baseline under the shipped defaults

`QUERY_REWRITE=true`, `FINAL_K=3`, cloud chat model. The axes above were
measured with `QUERY_REWRITE=false`; these are the same knobs re-measured on the
configuration that now ships.

TOP_K — same conclusion (4 best at K=1/K=2; 8 buys R@8 at a precision cost):

| TOP_K | R@1  | R@2  | R@8  | P@8  | Evidence (bef → aft) |
| ----- | ---- | ---- | ---- | ---- | -------------------- |
| 2     | 0.69 | 0.83 | —    | —    | 0.77 → 0.89          |
| 4     | 0.73 | 0.86 | —    | —    | 0.81 → 0.93          |
| 8     | 0.70 | 0.82 | 0.99 | 0.40 | 0.81 → 0.94          |

FINAL_K — same conclusion, evidence recall rises with it:

| FINAL_K | Evidence (bef → aft) |
| ------- | -------------------- |
| 1       | 0.64 → 0.73          |
| 2       | 0.75 → 0.86          |
| 3       | 0.78 → 0.90          |

LLM rerank (4→3): R@1 0.70 → 0.73, precision and evidence unchanged.

Chunk size — the conclusion flips:

| size/overlap    | R@1  | R@2  | R@4  | P@4  | Evidence (bef → aft) |
| --------------- | ---- | ---- | ---- | ---- | -------------------- |
| 50/20 (default) | 0.70 | 0.86 | 0.95 | 0.52 | 0.81 → 0.93          |
| 100/20          | 0.70 | 0.87 | 0.98 | 0.52 | 0.90 → 0.96          |
| 200/20          | 0.72 | 0.83 | 0.98 | 0.51 | 0.92 → 0.92          |

With the rewrite on, `100/20` matches or beats `50/20` on every column and is
clearly better on R@4 (0.98 vs 0.95) and evidence recall (0.96 vs 0.93). Larger
sections also raise evidence recall _before_ expansion, because a section is
fewer chunks so the retrieved chunk is more likely to carry the evidence. The
`50/20` default was promoted from the `QUERY_REWRITE=false` table above; on the
shipped configuration it is not the best choice.

## Calibrated embedding-model comparison: nomic-embed-text vs embeddinggemma (RAG-021)

A controlled two-model comparison of the embedding model itself, run with
`scripts/eval-model-sweep.sh` (embeddings-only: `QUERY_REWRITE=false`, no chat
model). It selects `MIN_SIMILARITY` on a **calibration split**
(`evals/retrieval-vi-en-calibration.json`, 21 cases) and reports metrics on a
disjoint **held-out split** (`evals/retrieval-vi-en-expanded.json`, 39 cases),
three repeats per model, at a jointly-selected floor (rejection first, then
recall@4). Both models selected floor `0.70`; on the held-out split the means are:

| model | R@1 | R@4 | P@4 | evidence (bef → aft) | rejection |
| ----- | --- | --- | --- | -------------------- | --------- |
| **nomic-embed-text** | **0.42** | **0.52** | 0.22 | 0.42 → **0.48** | 1.00 |
| embeddinggemma | 0.27 | 0.27 | 0.22 | 0.24 → 0.27 | 1.00 |

`nomic-embed-text` wins on recall@1/@4 and evidence recall at equal precision and
rejection; `embeddinggemma`'s higher raw recall appears only at a zero-rejection
floor, which the selection rule excludes. Decision: **retain `nomic-embed-text`**.
The full protocol, thresholds, per-run records, and the defect that this run
exposed live in [`experiments-eval-sweep.md`](../experiments-eval-sweep.md).

## Still open

- **The chunk default needs re-deciding.** `50/20` was promoted from the
  `QUERY_REWRITE=false` table; under the shipped defaults `100/20` looks at least
  as good. These are single runs, so change the default only after re-measuring
  the same configuration a few times.
- Sweep rows come from different chat-model and rewrite combinations (local or
  cloud model, rewrite on or off); only rows sharing a combination are directly
  comparable.
- The local chat model makes one eval run ~12–15 minutes, so an axis is ~40
  minutes. `OLLAMA_CHAT_MODEL=deepseek-v4.1-flash:cloud make sweep AXIS=…` is
  roughly 10× faster and was used for the re-baseline.
- Rewrite output is nondeterministic: one configuration measured R@1 0.70 and
  then 0.74 on two runs, so small differences between rows are noise.

## Post-fix reconciliation (RAG-023D)

This section **reconciles** the rows above against the correctness fixes landed
in RAG-023B (F1/F2 source identity, F5 batched ingestion, F6 document-global
chunk index) and RAG-023C (F3 JSON answer, F4 language parity, F7/F8 shared
citation parser). **No historical number in this document is rewritten or
edited**: every table above is preserved verbatim as the pre-fix record, and
this section is the pre-fix vs post-fix reconciliation. Where a post-fix
number is quoted below it is paired with its pre-fix value and explained by a
specific fixed defect (or explicitly attributed to documented measurement
noise).

### Why the pre-fix numbers are not directly comparable

Before F1/F2, ingest stored only the base name of each source (an absolute path
such as `/…/examples/loan-policy.md` was reduced to `loan-policy.md`), and the
`evals/*.json` datasets and `evals/corpus-vi-en.json` manifest carried those bare
base names as the identity label. Two different files with the same base name
collapsed onto one identity, and a retrieval hit could only be scored against a
label that did not name its directory. RAG-023B changes the stored identity to a
canonical **repository-relative, directory-qualified** path (symlinks resolved,
never a bare base name) and aligns every dataset `source` field and every
manifest key to it (for example `loan-policy.md` → `examples/loan-policy.md`).
The post-fix numbers below therefore come from a **different, correct** identity
than the historical rows; that identity change is the single largest reason the
recall/evidence/rejection values move.

### Pre-fix vs post-fix (held-out `evals/retrieval-vi-en-expanded.json`, floor 0.70)

| metric | pre-fix (RAG-021 row above) | post-fix (RAG-023D re-run) | attribution |
| ------ | --------------------------- | -------------------------- | ----------- |
| R@1 | 0.42 | see `experiments-eval-sweep.md` RAG-023D run | F1/F2 identity alignment (scores now match the retrieved `examples/…` source); F6 document-global chunk index changes which chunk ranks first |
| R@4 | 0.52 | see RAG-023D run | F1/F2 identity alignment; F5 batched insert does not change ranking, F6 chunk index reorders ties within a section |
| P@4 | 0.22 | see RAG-023D run | F1/F2: section-level precision is scored against the corrected directory-qualified source, so previously-colliding base names no longer merge |
| evidence (bef → aft) | 0.42 → 0.48 | see RAG-023D run | F6 document-global chunk index and F1/F2 identity; evidence recall is computed on the corrected source/section pair |
| rejection | 1.00 | see RAG-023D run | unchanged floor `0.70`; rejection is a function of the floor and the corrected identity, and the joint selection rule is unchanged |

Numbers labelled "see `experiments-eval-sweep.md` RAG-023D run" are recorded
with their per-repeat values, floor selection, and dataset/manifest hashes in
[`experiments-eval-sweep.md`](../experiments-eval-sweep.md), alongside the
pre-fix RAG-021 rows that are left intact.

### Movements in the non-sweep axes

- **Chunk size / evidence recall (`Re-baseline under the shipped defaults`).**
  The historical rows were measured with per-section chunk numbering. F6 makes
the index document-global, so the same section's chunks are now numbered in
document order. This does not change *which sections* are retrieved, so the
section-based R/P columns are unaffected, but it can change which chunk within a
section is returned first and therefore the evidence-recall before/after split.
  Treat a small evidence-recall movement as F6, and the single-run noise noted
  in "Still open" as the residual.
- **Generation-judge metrics (fact recall, groundedness, citation validity,
  citation entailment).** F7/F8 replace the per-consumer citation parser with one
  shared parser in `internal/citations` and treat a trailing segment as a page
  only when it matches `p.N`. Pre-fix, a section title containing `" - "` was
dropped (F7) and a `Page N - p.N` tail was mis-read (F8), so citation validity
  and entailment were scored against a parser that silently discarded some
  citations. Post-fix those citations are parsed, so citation validity/entailment
  denominators can change; the change is attributable to F7/F8, not to a change
  in model behavior.
- **JSON-mode answer and language parity (F3/F4).** These are output/parity
  fixes, not retrieval changes: F3 makes `OBSERVABILITY_FORMAT=json` show the
  answer, and F4 makes `cmd/eval` forward `CORPUS_LANGUAGE` the same way `cmd/rag`
  does. Neither moves the recall/precision rows above, but F4 makes the eval's
  keyword retrieval match the production path under `CORPUS_LANGUAGE=vi`, which
  is what the post-fix sweep measures.

### Still open — resolution (explanation only, prior rows unchanged)

- **The chunk-default question.** The historical conclusion ("`100/20` looks at
  least as good under the shipped defaults") still stands as a *measurement*
  concern, but part of the apparent gap was the F6 chunk-ordering difference:
  chunk numbering was per-section before the fix and is document-global after it.
  Re-decide the default only from post-fix, multi-repeat measurements; the
  pre-fix single-run rows above are retained as provenance.
- **The comparability warning.** It is unchanged in force and now also covers the
  identity change: pre-fix (bare base-name identity) and post-fix
  (directory-qualified identity) rows are **not** directly comparable, because
  they score against different identity labels. Only rows sharing both the
  identity convention and the chat-model/rewrite combination are comparable.

All historical tables and numbers in this document are preserved and only
reconciled here; the sweep-side numbers and hashes for the post-fix run are in
[`experiments-eval-sweep.md`](../experiments-eval-sweep.md).
