# RAG-024 — Experiment evidence recovery (operator-run)

**Type:** Operator-run recovery evidence for RQ-002/RQ-003/RQ-004. Distinct from SOP-generated
records. **No commits, no pushes, no model downloads, no `rag_db` access.**

Held-out inputs (frozen): `evals/retrieval-vi-en-expanded.json` sha256 `7f6926df…cbd2d0df`
(39 cases); `evals/corpus-vi-en.json` sha256 `2ddbef2f…fbc87b21` (8 docs). Floor unchanged.

---

## Phase 1 — RQ-004 code review + quarantine

### Behaviour under default configuration
- `internal/retrieval/hybrid.go` (modified): adds `FuseRankingsWeighted`; `FuseRankings` now
  delegates to it with every weight `1.0`. The fused ordering and `FusionScore` values are
  **byte-identical** to before (unweighted RRF). **No default behaviour change**; `go test`
  passes.
- `internal/retrieval/ranking.go` (new, 171 lines): `PathMode` (vector-only, keyword-only,
  rrf, weighted-rrf), `RankingConfiguration`, `BuildOptions`, `Fuse`, `RunConfiguration`.
  **No production callers** (`cmd/*` does not import it).
- `internal/retrieval/sweep.go` (new, 501 lines): experiment harness whose searcher is a
  **synthetic `FixtureSearcher`** — similarities are derived from a SHA-256 of
  `source/section/evidence` (`contentScore`), i.e. a *documented stand-in for a real
  embedding cosine*, not real pgvector retrieval. No production callers.
- Tests: `ranking_test.go` (147), `sweep_test.go` (245).

### Quarantine inventory (NOT measurements)
`evals/sweep-records/*.json` — 5 files (`vector-only`, `keyword-only`, `rrf`,
`rrf-topk40-finalk20`, `weighted-rrf-vec2`). **Quarantined** because they are **synthetic**:
- produced by a **unit test** (`SWEEP_CONFIG=… go test ./internal/retrieval/ -run TestSweepExperimentRecords`),
  driven by `FixtureSearcher`, not a live database;
- **invalid case counts** (`en-vi num_cases=39`, `vi-en=21` — the held-out `en-vi`/`vi-en`
  buckets have 5 cases each, and the kind→file mapping is wrong);
- **implausible scores** (`rank1_rate=1.0` for every cross-lingual bucket), contradicting the
  measured `0.00`.

**Recommendation:** keep `hybrid.go` (behaviour-preserving) only if a follow-up uses weights —
otherwise revert; retain `ranking.go`/`sweep.go`+tests **only as explicitly-experimental,
non-production tooling** (or remove, as they are unused); **remove/quarantine the synthetic
`sweep-records`**. Never cite them as evidence.

---

## Phase 3 — RQ-002 chunking (operator-run, nomic, floor 0.70, held-out 39)

`scripts/chunk-sweep.sh` was audited: it **refuses `rag_db`** (name guard) and writes only to
`docs/` (never `evals/`); it is fail-closed. It **creates fresh databases**, which `rag_user`
cannot do (`rolcreatedb=f`) and it has no reuse/pre-created mode — so it cannot run here
as-is. The experiment was therefore run operator-side on the disposable `rag_021_test`
(fresh ingest per configuration).

| CHUNK_SIZE (overlap 20) | chunks | vector R@1/R@4 | hybrid R@1/R@4 | evidence (bef→aft) | **cross-lingual R@4** |
| --- | --- | --- | --- | --- | --- |
| 30 | 198 | 0.48 / 0.48 | 0.45 / 0.48 | 0.36 → 0.45 | **0.00** |
| **50 (default)** | 83 | 0.42 / **0.52** | 0.42 / **0.52** | 0.42 → **0.48** | **0.00** |
| 100 | 50 | 0.45 / 0.48 | 0.42 / 0.48 | 0.48 → 0.48 | **0.00** |

**Finding:** chunking does **not** move cross-lingual retrieval (0.00 at every size). The
default `50/20` is the best overall (R@4 `0.52`); 30 and 100 are worse (0.48). **No chunking
change recommended.** Captures: `docs/rq-002-operator-chunk-{30,100}-nomic-expanded.txt`.

---

## Phase 4 — RQ-004 ranking / candidate / floor (operator-run, nomic, held-out 39)

### Similarity-floor sensitivity (CHUNK_SIZE=50)

| floor | R@1 | R@4 | P@4 | evidence | rejection | cross-lingual R@4 |
| --- | --- | --- | --- | --- | --- | --- |
| 0.50 | 0.48 | 0.67 | 0.23 | 0.64 | 0/6 (0.00), FP 1.00 | 0.00 |
| 0.60 | 0.48 | 0.67 | 0.28 | 0.64 | 4/6 (0.67), FP 0.33 | 0.00 |
| **0.70 (acceptance)** | 0.42 | 0.52 | 0.22 | 0.48 | 6/6 (1.00), FP 0.00 | 0.00 |

The floor buys rejection at the cost of recall. Lowering it to `0.60` would add +0.15 R@4
and +0.16 evidence but lose 2/6 rejections — a trade-off to **measure, not change**.

### Candidate-pool size
`TOP_K=8` at floor `0.70`: R@8 `0.52` — identical to R@4, so a larger pool recovers nothing
(the cross-lingual sections are absent entirely; the monolingual ones are floor-trimmed).

### Vector-only vs hybrid (floor 0.70)
Identical overall (R@1 `0.42`, R@4 `0.52`, P@4 `0.22`). **Fusion adds no aggregate value at
the selected floor**; it reorders individual cases (helps some, hurts others) but nets to zero.

### Cross-lingual candidate recovery
At every floor (0.50/0.60/0.70) and every chunk size (30/50/100), cross-lingual R@4 is **0.00**.

---

## Recommended retrieval strategy

1. **Keep** `nomic-embed-text`, `CHUNK_SIZE=50/20`, floor `0.70` — each is already optimal or
   rejection-preserving among tested configs.
2. **Cross-lingual is the only large remaining gain.** It is an **embedding/recall** failure
   (candidate miss pre-filter), not floor, fusion, or chunking. Fix requires an authorized
   **multilingual embedding model** (download) or a **non-embedding** strategy (query
   translation / language-aware retrieval). No installable model improves it (Phase 2).
3. **Do not** invest in fusion weighting (vector==hybrid) or floor changes (rejection loss).

---

## Phase 5 — SOP reconciliation proposals (no state edits made)

| task | SOP state | actual deliverable | smallest supported reconciliation |
| --- | --- | --- | --- |
| RQ-002 | `LOCAL_DONE` (PASS) | only a harness script; **no results table**; script can't run under `rag_user` | amend RQ-002 to accept **operator-run** chunking results (this doc, Phase 3) and document the CREATE-DATABASE prerequisite; keep the script as tooling |
| RQ-003 | `LOCAL_DONE` (**ALREADY_SATISFIED**, 0 mutations) | **no matrix** | point RQ-003's deliverable at `docs/rq-003-embedding-model-matrix.md` (operator-run) |
| RQ-004 | **`CONTINUE`/INCOMPLETE** | production tooling + **synthetic** records | quarantine `evals/sweep-records/*`; decide retain/revert `ranking.go`/`sweep.go`/`hybrid.go`; accept the Phase-4 operator measurements as the deliverable |

Apply each via `sop reconcile --accept-changed <id>` (with operator approval) or `sop retry`
as appropriate — **not performed here**.

*All measurements above are operator-run against disposable databases; the frozen inputs, the
`0.70` threshold, and the historical record are unchanged.*
