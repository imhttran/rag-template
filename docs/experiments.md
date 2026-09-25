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
- Models: `nomic-embed-text` for embeddings, `qwen3.8:27b-mlx` for the chat
  model. Both local, so reruns are free but the chat model is slow.

## Chunk size and overlap

Baseline: `QUERY_REWRITE=false`, `FINAL_K=2`. `CHUNK_SIZE` × `CHUNK_OVERLAP`:

| size/overlap | R@1 | R@2 | R@4 | P@4 | Evidence (bef → aft) |
| ------------ | --- | --- | --- | --- | -------------------- |
| 50/0         | 0.66 | 0.86 | **0.95** | 0.56 | 0.72 → 0.79 |
| 50/20        | **0.68** | **0.86** | 0.94 | 0.55 | **0.79 → 0.86** |
| 50/40        | 0.64 | 0.85 | 0.85 | 0.57 | 0.73 → 0.82 |
| 100/0        | 0.64 | 0.83 | 0.92 | 0.52 | 0.78 → 0.83 |
| 100/20       | 0.64 | 0.83 | 0.94 | 0.51 | 0.77 → 0.83 |
| 100/40       | 0.64 | 0.83 | 0.94 | 0.54 | 0.77 → 0.83 |
| 200/0        | 0.65 | 0.82 | 0.91 | 0.47 | 0.82 → 0.82 |
| 200/20       | 0.65 | 0.82 | 0.92 | 0.48 | 0.82 → 0.82 |
| 200/40       | 0.65 | 0.82 | 0.92 | 0.47 | 0.82 → 0.82 |

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

| TOP_K | R@2 | R@8 | P@8 | Evidence (bef → aft) |
| ----- | --- | --- | --- | -------------------- |
| 2     | 0.86 | — | — | 0.79 → 0.86 |
| 4     | 0.86 | — | — | 0.79 → 0.86 |
| 8     | 0.83 | 0.96 | 0.43 | 0.77 → 0.83 |

`TOP_K=2` gains nothing over 4 and loses the R@4 headroom; `TOP_K=8` buys R@8
but depresses R@2 and halves precision.

Decision: keep `TOP_K=4`.

## FINAL_K

Baseline: `QUERY_REWRITE=false`.

| FINAL_K | R/P at K=1,2,4 | Evidence (bef → aft) |
| ------- | -------------- | -------------------- |
| 1       | unchanged | 0.61 → 0.68 |
| 2       | unchanged | 0.79 → 0.86 |
| 3       | unchanged | **0.83 → 0.91** |

The recall/precision columns do not move with `FINAL_K` — they score fused
rankings, not the expanded set — but evidence recall moves cleanly: `FINAL_K` is
how many sections reach the model.

Decision: default `FINAL_K=3`.

## Query rewrite

Baseline: `FINAL_K=2`, chunk `50/20`. Axis toggles `QUERY_REWRITE`:

| QUERY_REWRITE | R@1 | R@2 | R@4 | P@4 | Evidence (bef → aft) |
| ------------- | --- | --- | --- | --- | -------------------- |
| false         | 0.68 | 0.86 | 0.94 | 0.55 | 0.79 → 0.86 |
| true          | 0.73 | 0.87 | 0.95 | 0.52 | 0.77 → 0.87 |

Keeping the original versus replacing it with the rewrite (`EVAL_REWRITE_ONLY`),
measured on current defaults:

| Mode                 | R@1 | P@1 | R@4 | P@4 |
| -------------------- | --- | --- | --- | --- |
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

Run **incomplete** — only the first row completed before the sweep was stopped.

| MIN_SIMILARITY | R@1 | R@4 | P@4 | Evidence (bef → aft) | Rejection |
| -------------- | --- | --- | --- | -------------------- | --------- |
| 0.4            | 0.70 | 0.93 | 0.43 | 0.79 → 0.91 | 0/6 (0.00) |
| 0.6 (default)  | — | — | — | — | 2/6 (0.33) |
| 0.8            | — | — | — | — | — |

`0.4` accepts everything on the unanswerable cases (false-positive rate 1.00),
so it is clearly the wrong direction; the default stays `0.6` until the rest
runs.

## Generation judge

`EVAL_FACT_JUDGE` axis has not been run. The fact judge, groundedness, and
citation metrics are implemented but no numbers have been recorded yet.

## Bugs the sweep surfaced

- `CHUNK_OVERLAP=0` was silently coerced to the default 20: `envIntOrDefault`
  treated any non-positive value as unset. At size 50 the manual ingested 48
  chunks for overlap 0 and 20 alike. Fixed with `envNonNegativeIntOrDefault`
  (overlap 0 is meaningful).
- `TOP_K=2` double-counted: `ks` held `2` twice, so K=2 averaged to `1.72` and
  rejections to `4/6`. Fixed by sorting and deduplicating `ks`.

## Still open

- `Similarity-only rejection` is `2/6` under every axis run — chunk size,
  overlap, TopK, FinalK, rewrite, rerank, and (so far) MIN_SIMILARITY. The two
  near-miss unanswerable cases are not fixed by retrieval tuning; this points at
  the similarity threshold or the gate.
- The chunk/TOP_K/FINAL_K/rerank rows were measured with `QUERY_REWRITE=false`.
  Re-run them under the current default (`QUERY_REWRITE=true`) to compare on the
  shipped configuration.
- Rewrite output is nondeterministic, so averages with `QUERY_REWRITE=true` vary
  a little between runs.
