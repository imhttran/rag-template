# RQ-006 — Deterministic Arm D validation (results)

**Type:** Operator-run experiment; evaluation-only harness `cmd/expbilingual`; **no production
retrieval change.** Frozen 39-case split, disposable `rag_021_test`, `nomic-embed-text`, floor
`0.70`, query model `llama3.2:3b` (installed; no download).

## Phase 1 — Determinism
- **Pinned decoding** in the harness (the production `generation` package sends no options, so
  the harness calls the Ollama client directly): `temperature=0, seed=0, num_predict=64`.
  Recorded in the run header.
- **Per-request timeout** `GEN_TIMEOUT=60s`; **progress logging** (`GEN id=… variant=… source=cache|model`).
- **Query cache** `docs/rq-006-query-cache.json`, keyed by chat model + case id, storing the
  **source question** and each variant (`exp`, `vi`, `en`); a cache entry is used only if its
  stored question matches the dataset question (guards stale mappings).
- **Result:** all three arms produced **zero variance across 3 repeats** — generation was
  produced once (117 queries) and reused, so the runs isolate retrieval determinism.

## Phase 2/3 — Matrix (deterministic; identical across all 3 repeats)

| arm | overall R@1 | overall R@4 | evidence (harness) | **rejection** | cross-ling R@1 | cross-ling R@4 | en-vi R@4 | vi-en R@4 | vi-vi R@1 | en-en R@1 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **A** baseline | 0.42 | 0.52 | 0.52 | **6/6** | 0.00 | 0.00 | 0.00 | 0.00 | 0.80 | 0.50 |
| **D1** guard + full fusion | 0.42 | 0.64 | 0.61 | **6/6** | 0.10 | 0.30 | 0.60 | 0.00 | 0.80 | 0.40 |
| **D2** guard + baseline-first | 0.42 | 0.55 | 0.52 | **6/6** | 0.00 | 0.10 | 0.20 | 0.00 | 0.80 | 0.50 |

- **D2** pins the original query's top-1 at rank 1 and fills ranks 2–4 from the fused
  translated ranking; it keeps the original-query rejection guard.
- Latency (39-case pass): A ≈ 2 s; D1/D2 ≈ 19 s (first pass generates 117 queries, ~58 s total
  for the whole 3×3 matrix; subsequent runs are cache-served).
- **Evidence semantics:** the harness expands the arm's own top-`finalK` distinct sections, so
  its baseline is `0.52` vs `cmd/eval`'s recorded `0.48` (which fuses `finalK` docs *then*
  dedups). Same method across arms; R@1/R@4 match the baseline exactly. Both are reported.

## Phase 4 — Acceptance (D2)
| criterion | D2 | result |
| --- | --- | --- |
| 1. rejection 6/6 | 6/6 | ✅ |
| 2. cross-lingual R@4 > baseline | 0.00 → 0.10 | ✅ |
| 3. monolingual R@1 no regression | vi-vi 0.80, en-en 0.50 (unchanged) | ✅ |
| 4. repeatable (pinned + cache) | zero variance across 3 reps | ✅ |
| 5. runtime overhead acceptable | ~+17 s / 39-case pass (one-off generation) | ✅ |

**D2 qualifies on all five criteria.** D1 shows a larger cross-lingual gain (R@4 `0.30`) but
regresses `en-en` R@1 (`0.50 → 0.40`); D2 trades gain for no monolingual regression.
`vi-en` remains `0.00` in every arm.

## Recommendation
**Recommend D2 for production consideration** (RQ-007), as an evaluation-only change first:
preserve the original-query rejection guard, pin the original query's top-1 at rank 1, and use
translated/expanded candidates only to fill ranks 2–4. Expected: rejection `6/6`, cross-lingual
R@4 `0.00 → 0.10` (`en-vi 0.00 → 0.20`), no monolingual regression. If the (small) gain is
deemed not worth the complexity, **no production change** is the alternative.

*Captures:* `docs/rq-006-deterministic-matrix.txt` (A/D1/D2 × 3), `docs/rq-006-query-cache.json`,
`docs/rq-006-diag-arm{A,C}.txt`. Frozen inputs, the `0.70` threshold, and RAG-023 records unchanged.
