# RQ-007 — Deferred production bilingual retrieval (backlog record)

**Type:** Operator-produced decision/backlog record (RQ-007 deliverable). Documentation only.
**No production retrieval change** — `internal/retrieval`, `cmd/rag`, `cmd/eval` scoring, and
all production defaults are **frozen** and byte-identical. This record references raw captures
by path; it does not embed their contents.

## Scope statement

This document records a **deferred** decision and preserves the RQ-006 findings as future
backlog. It makes **no** production code change and intends none: the retrieval pipeline, the
`cmd/rag` and `cmd/eval` scoring paths, and every production default remain unchanged. Any
future production integration of a bilingual-retrieval strategy requires separate
authorization and is tracked here only as backlog.

## Decision — DEFERRED

Production bilingual retrieval is **DEFERRED**. D2 is a real but **small** answerability
improvement (1/10 cross-lingual; rejection preserved; no monolingual regression; reproducible).
It is **not worth a production bilingual-retrieval path as-is**. No production change is made
or recommended at this time; the findings are preserved as future backlog.

## D2 — measured result

Deterministic, identical across 3 repeats (pinned decoding + query cache):

| metric | value |
| --- | --- |
| rejection | **6/6** |
| cross-lingual R@4 | **0.00 → 0.10** |
| en-vi R@4 | 0.00 → 0.20 |
| vi-en R@4 | 0.00 |
| vi-vi R@1 | 0.80 (unchanged) |
| en-en R@1 | 0.50 (unchanged) |
| variance across 3 repeats | zero |

- **Rejection preserved: `6/6`** — every unanswerable case is guard-rejected. D2 keeps the
  original-query rejection guard.
- **Cross-lingual R@4 `0.00 → 0.10`** (`en-vi 0.00 → 0.20`) — one cross-lingual case recovered.
- **Single recovered case: `eval-en-vi-02`** — recovered at Recall@4 (`r4=1.00`); its
  original-query guard **accepted** it (`guardOK=true`, 4 candidates), so the correct candidate
  is available to the answer generator.
- **No monolingual R@1 regression** — `vi-vi R@1` unchanged `0.80`, `en-en R@1` unchanged
  `0.50`.
- **Zero variance across 3 repeats** (pinned decoding + query cache).

D2 qualifies on all five Phase-4 criteria; its gain is candidate availability (`1/10`
cross-lingual), not a synthetic metric.

## Unresolved — `vi-en` direction

The `vi-en` direction remains **`R@4 0.00` in every arm** (embedding miss, upstream of ranking).
All five `vi-en` cases are guard-accepted but the correct English section is never retrieved;
the `en-vi` direction improves, the `vi-en` direction does not. This is an embedding-recall gap
that the ranking-side guard cannot fix.

## Future backlog

1. **A rejection-preserving cross-lingual strategy** — keep the original-query rejection guard
   and use translated/expanded candidates only where they cannot lower rejection (the D2 shape:
   pin the original query's top-1 at rank 1, fill ranks 2–4 from the fused translated ranking).
2. **An authorized multilingual embedding model** — for the unresolved `vi-en` direction; the
   `vi-en` miss is upstream of ranking, so only a stronger embedding model can address it.

## Traceability

This backlog is traceable to the verified RQ-006 captures:

- `docs/rq-006-decision-record.md` — decision (defer production integration; preserve findings
  as future backlog: a rejection-preserving cross-lingual strategy and an authorized
  multilingual embedding model for the unresolved `vi-en` direction); D2 row (overall R@1 0.42,
  R@4 0.55, rejection 6/6, cross-ling R@4 0.10, en-vi R@4 0.20, vi-en R@4 0.00, vi-vi R@1 0.80,
  en-en R@1 0.50); recovered case `eval-en-vi-02`; unresolved `vi-en` embed miss.
- `docs/rq-006-deterministic-armD-validation.md` — Phase 4 acceptance table (D2 qualifies on
  all five criteria: rejection 6/6; cross-lingual R@4 0.00 → 0.10; monolingual R@1 no regression
  vi-vi 0.80 / en-en 0.50; zero variance across 3 reps; ~+17 s / 39-case pass); "`vi-en` remains
  `0.00` in every arm".

Every number recorded above is found in one of those two captures (or the RQ-006 captures they
cite: `docs/rq-006-rejection-preserving-results.md`, `docs/rq-006-deterministic-matrix.txt`,
`docs/rq-006-diag-armD2.txt`); no new or contradictory number is introduced. The design
context for the arms is `docs/rq-006-bilingual-retrieval-experiment-design.md`.
