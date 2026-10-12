# RQ-004 — Retrieval ranking and candidate selection results

**Type:** Standalone measurement/reporting report (consolidated operator-run evidence).
**Scope:** Retrieval ranking and candidate-selection levers for the frozen held-out split.
**Status:** Report only. **No production behaviour is changed by this task.** The `0.70`
acceptance floor is unchanged and was **never lowered to pass**.

This report consolidates **operator-run** measurements. It performs **no new live
measurement, model download, or database write**. Where the operator evidence does not
support a cell, the cell is marked **not separately measured** with the reason.

---

## 1. Frozen dataset and corpus hashes

| Input | File | sha256 | Size |
| --- | --- | --- | --- |
| Held-out evaluation split | `evals/retrieval-vi-en-expanded.json` | `7f6926df…cbd2d0df` | 39 cases |
| Corpus manifest | `evals/corpus-vi-en.json` | `2ddbef2f…fbc87b21` | 8 docs |

The same hashes and counts are recorded in `docs/plans/PLAN-RAG-024.md` and
`docs/rq-024-operator-recovery-evidence.md`. The inputs are frozen; the `0.70`
threshold and the historical record are unchanged.

The 39 cases comprise 33 answerable cases and 6 answerable-but-absent “correct
rejection” cases. Within the answerable set, the held-out split is populated by the
five kinds **en-en**, **vi-vi**, **en-vi**, **vi-en**, and **misleading**
(`PLAN-RAG-024` H1/H2/H6); the `en-vi`/`vi-en` cross-lingual buckets carry 5 cases each
(`docs/rq-024-operator-recovery-evidence.md` Phase 1). Per-case kind labels are **not
separately exposed** in the operator capture text; kind-attributed figures below are
reported only where the capture supports them, otherwise marked *not separately
measured*.

---

## 2. Synthetic sweep records — EXCLUDED (non-evidence)

**`evals/sweep-records/*` are SYNTHETIC and are excluded from all benchmarks in this
report.** Per `evals/sweep-records/SYNTHETIC-NOT-MEASURED.md`, those 5 JSON files
(`vector-only`, `keyword-only`, `rrf`, `rrf-topk40-finalk20`, `weighted-rrf-vec2`) are
**not measurements**: they were produced by a unit test
(`go test ./internal/retrieval/ -run TestSweepExperimentRecords`) driving a synthetic
`FixtureSearcher` whose similarities derive from a SHA-256 of `source/section/evidence`
— a documented stand-in for a real embedding cosine, **not** pgvector retrieval. Their
case counts are invalid (`en-vi`/`vi-en` = 39/21 vs 5 each held out) and their cross-lingual
`rank1_rate=1.0` contradicts the measured `0.00`. **No value from any `evals/sweep-records/*`
file appears in any table in this report.**

---

## 3. Retrieval configurations (experiment record)

Every row records config, input hashes, per-kind metrics (where derivable), latency
(where recorded), and the exact reproducible command referenced by the operator capture.

| # | Configuration | Floor | TOP_K / FINAL_K | Inputs (sha256) | Per-kind metrics | Latency | Evidence path |
| --- | --- | --- | --- | --- | --- | --- | --- |
| C0 | vector-only, nomic-embed-text, CHUNK_SIZE=50/overlap=20 | **0.70** | TOP_K=4 (default) | `7f6926df…cbd2d0df` / `2ddbef2f…fbc87b21` | aggregate only (see §4); per-kind *not separately measured* | rerank off: avg 0s; lexical/llm/cross-encoder unavailable | `docs/rq-004-operator-floor-0.70-nomic-expanded.txt` |
| C1 | hybrid RRF (vector+keyword, `rrfK=60`, equal weight), nomic, 50/20 | **0.70** | TOP_K=4 | as above | identical to C0 (see §6) | rerank off: avg 0s | `docs/rq-004-operator-floor-0.70-nomic-expanded.txt` |
| C2 | vector-only, nomic, 50/20 | 0.50 | TOP_K=4 | as above | aggregate only; per-kind *not separately measured* | rerank off: avg 0s | `docs/rq-004-operator-floor-0.50-nomic-expanded.txt` |
| C3 | hybrid RRF, nomic, 50/20 | 0.50 | TOP_K=4 | as above | — | rerank off: avg 0s | `docs/rq-004-operator-floor-0.50-nomic-expanded.txt` |
| C4 | vector-only, nomic, 50/20 | 0.60 | TOP_K=4 | as above | aggregate only | rerank off: avg 0s | `docs/rq-004-operator-floor-0.60-nomic-expanded.txt` |
| C5 | hybrid RRF, nomic, 50/20 | 0.60 | TOP_K=4 | as above | — | rerank off: avg 0s | `docs/rq-004-operator-floor-0.60-nomic-expanded.txt` |
| C6 | vector-only, nomic, 50/20 | **0.70** | **TOP_K=8** | as above | aggregate only | rerank off: avg 0s | `docs/rq-004-operator-topk8-nomic-expanded.txt` |
| C7 | hybrid RRF, nomic, 50/20 | **0.70** | **TOP_K=8** | as above | — | rerank off: avg 0s | `docs/rq-004-operator-topk8-nomic-expanded.txt` |

**Exact reproducible command (operator-captured):**

```
# per-case evaluation harness (cmd/eval); run operator-side against a disposable,
# freshly-ingested database at the frozen inputs and the given floor/TOP_K:
EVAL_SIMILARITY_FLOOR=<floor> EVAL_TOP_K=<k> go run ./cmd/eval -dataset evals/retrieval-vi-en-expanded.json -corpus evals/corpus-vi-en.json
```

(Configuration knobs exercised: `EVAL_SIMILARITY_FLOOR` ∈ {0.50, 0.60, 0.70},
`EVAL_TOP_K` ∈ {4, 8}; embedding model `nomic-embed-text`; `CHUNK_SIZE=50`,
overlap 20. The exact command form, per-case output layout, and rerank section are as
recorded in the four `docs/rq-004-operator-*-nomic-expanded.txt` captures. The command
could not be re-run in this environment — see §5 caveat.)

**Latency note:** the captures record reranking latency only: `off avg=0s` (baseline,
no rerank call); `lexical`, `llm`, and `cross-encoder` are reported *unavailable*
(`EVAL_LEXICAL_RERANK=false`, `EVAL_LLM_RERANK=false`, no cross-encoder provider). No
per-query wall-clock latency is recorded in the captures → **not separately measured**.

---

## 4. Similarity-floor sensitivity and rejection accuracy

Acceptance floor is **0.70** (bold). Lower floors are **trade-off exploration only** and
are not proposed as production settings.

| Floor | R@1 | R@4 | P@4 | Evidence recall | Rejection (correct/6) | FP rate | Cross-lingual R@4 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 0.50 | 0.48 | **0.67** | 0.23 | 0.64 | 0/6 (**0.00**) | 1.00 | **0.00** |
| 0.60 | 0.48 | **0.67** | 0.28 | 0.64 | 4/6 (**0.67**) | 0.33 | **0.00** |
| **0.70 (acceptance)** | **0.42** | **0.52** | **0.22** | **0.48** | **6/6 (1.00)** | **0.00** | **0.00** |

Source: `docs/rq-024-operator-recovery-evidence.md` Phase 4;
`docs/rq-004-operator-floor-{0.50,0.60,0.70}-nomic-expanded.txt`.

**Finding:** lowering the floor buys recall at the cost of rejection. From 0.70 to
0.60, R@4 rises 0.52 → 0.67 (+0.15) and evidence recall +0.16, but rejection collapses
from 1.00 to 0.67 (2/6 rejections lost, FP rate 0.00 → 0.33). From 0.70 to 0.50, R@4
reaches 0.67 with **rejection 0.00** and FP rate 1.00. The floor is a deliberate
rejection/citation integrity control; **it must not be lowered to pass.**

---

## 5. TOP_K sensitivity (candidate pool)

| Config | TOP_K | R@8 | R@4 | Rejection | Evidence path |
| --- | --- | --- | --- | --- | --- |
| C6 vector-only, floor 0.70 | 8 | **0.52** | 0.52 | 6/6 (1.00) | `docs/rq-004-operator-topk8-nomic-expanded.txt` |
| C7 hybrid, floor 0.70 | 8 | **0.52** | 0.52 | 6/6 (1.00) | `docs/rq-004-operator-topk8-nomic-expanded.txt` |

**Finding:** `TOP_K=8` yields R@8 `0.52` — **identical to R@4 `0.52`**, so a larger
candidate pool recovers **nothing**. The cross-lingual sections are absent pre-retrieval
and the monolingual ones are floor-trimmed, so widening the pool does not surface new
correct candidates. `FINAL_K` was not separately exercised in the operator capture →
**not separately measured**.

**Caveat (honest recording):** no new live run was possible in this environment —
`rag_user` cannot create disposable databases (`rolcreatedb=f`), so the operator
measurements above are the binding source and this report performs no fresh replication.

---

## 6. Vector-only vs hybrid (floor 0.70)

| Retrieval path | R@1 | R@4 | P@4 |
| --- | --- | --- | --- |
| Vector-only | 0.42 | 0.52 | 0.22 |
| Hybrid RRF (equal weight, `rrfK=60`) | 0.42 | 0.52 | 0.22 |

**Finding:** vector-only and hybrid are **identical at the selected floor** (R@1 0.42 /
R@4 0.52 / P@4 0.22). Fusion adds no aggregate value at 0.70 — it reorders individual
cases (helps some, hurts others) but nets to zero (see §8). Weighted RRF was described
in the operator record Phase 1 (`FuseRankingsWeighted`, every weight 1.0, byte-identical
to unweighted) but **the weighted variant is absent from this checkout**; there is
**no operator measurement for any non-equal-weight setting** → weighted-RRF metrics are
**not separately measured**.

Source: `docs/rq-024-operator-recovery-evidence.md` Phase 4;
`docs/rq-004-operator-floor-0.70-nomic-expanded.txt`.

---

## 7. Cross-lingual results

| Setting | Cross-lingual R@4 |
| --- | --- |
| Floor 0.50 / 0.60 / 0.70 | **0.00** |
| CHUNK_SIZE 30 / 50 / 100 | **0.00** |
| nomic-embed-text and embeddinggemma | **0.00** (R@1 and R@4, all 10 cross-lingual cases) |

Sources: `docs/rq-024-operator-recovery-evidence.md` Phase 3 + Phase 4;
`docs/rq-003-embedding-model-matrix.md`.

**Finding:** cross-lingual (vi↔en) retrieval is `0.00` at **every** floor and chunk
size, and across both evaluated embedding models. This is a candidate-miss
(embedding/recall) failure **upstream of ranking**, not a floor, fusion, or chunking
effect. No ranking/candidate lever tested in RQ-004 fixes it.

---

## 8. Per-kind comparison of retrieval path and fusion weights

The held-out kinds are **en-en**, **vi-vi**, **en-vi**, **vi-en**, and **misleading**.
The operator captures give aggregate vector/hybrid rows and per-case rows
(English/Vietnamese questions, 6 correct rejections) but **do not carry an explicit
kind label per case**. Consequently:

| Kind | Vector-only R@4 | Hybrid R@4 | Rank-1 loss observed? | Notes |
| --- | --- | --- | --- | --- |
| en-en | *not separately measured* | *not separately measured* | — | aggregates only; capture lacks per-case kind label |
| vi-vi | *not separately measured* | *not separately measured* | — | aggregates only |
| en-vi | **0.00 (cross-lingual R@4)** | **0.00 (cross-lingual R@4)** | yes — candidate miss pre-filter | 5 cases (`rq-024` Phase 1) |
| vi-en | **0.00 (cross-lingual R@4)** | **0.00 (cross-lingual R@4)** | yes — candidate miss pre-filter | 5 cases |
| misleading | *not separately measured* | *not separately measured* | — | no per-kind figure in capture |

**Reason for the *not separately measured* markers:** the frozen dataset supplies kind
membership, but the operator capture text does not emit a per-case kind column; any
per-kind monolingual split would therefore be fabricated. The report cites the
traceable cross-lingual (0.00) and aggregate figures instead, per `PLAN-RAG-024`.

**Fusion-weight comparison:** the only fusion setting with operator evidence is equal
weight (all `1.0`), which is **identical to vector-only** at 0.70. Any non-equal or
weighted-RRF setting is **unmeasured** (`weighted-rrf-vec2` in `evals/sweep-records/` is
**synthetic and excluded**). No per-kind weight sensitivity can be stated.

---

## 9. Rank-1 loss and floor-truncation analysis

Derived from the per-case rows in `docs/rq-004-operator-floor-0.70-nomic-expanded.txt`
(with the 0.50/0.60 captures for floor comparison). Classification of rank-1 outcomes
over the 33 answerable cases + 6 correct rejections:

| Loss class | Definition | Observed at floor 0.70 |
| --- | --- | --- |
| A. Candidate absent pre-filter | No ranked candidate at all — retrieval/embedding miss | Cross-lingual cases (vi↔en) → R@4 0.00; e.g. *“When is a debt written off?”*, *“Tôi có thể sắp xếp kế hoạch thanh toán không?”* |
| B. Correct candidate present but rank-1 lost to fusion/reordering | Vector rank-1 correct, hybrid rank-1 becomes 0.00 at K=1 | 6 cases where vector K=1 R=1.00 but Hybrid K=1 R=0.00 (e.g. *“When is a late fee charged?”* [0.7736 correct #1 vs 0.7602], *“How do I request a grace period?”* vector #1 is a red herring 0.7181, hybrid promotes the correct 0.7105 to #1 at K=1) |
| C. Correct candidate above floor but rank-1 lost on tie/precision | Correct evidence present in top-4 but not #1, precision diluted | e.g. *“What do I the payment codes mean?”* / *“What insurance is required?”* evidence recall 0.00→1.00 via expansion |
| D. Floor-truncated | Candidate present pre-floor (at 0.50/0.60) but dropped at 0.70 | *“How do I request a payoff statement?”*, *“How do I dispute a charge?”*, *“What do I the payment codes mean?”* — pass at 0.50/0.60 (R@4 0.67) but 0.00 at 0.70 |

**Six hybrid-vs-vector rank-1 divergences at floor 0.70:** the captures show a handful of
cases where vector and hybrid disagree at K=1 while the **aggregates are identical**
(R@1 0.42 / R@4 0.52 / P@4 0.22). Examples include *“When is a late fee charged?”*
(vector K=1 = 1.00, hybrid K=1 ≈ 0) and *“How do I request a grace period?”*
(vector K=1 = 0.00 red-herring #1, hybrid K=1 = 1.00 correct #1). These are **reordering
effects with zero net aggregate impact**, consistent with the reported identical
aggregates — fusion neither gains nor loses recall overall, it only permutes.

**Floor-truncation finding (both directions):**

- **Recall gained at lower floors:** comparing the same cases at 0.50/0.60 vs 0.70,
  cases such as *“How do I request a payoff statement?”*, *“How do I dispute a charge?”*
  and *“What do the payment codes mean?”* have a correct candidate above the 0.60 floor
  that is truncated at 0.70 → R@4 0.67 (0.50/0.60) vs 0.52 (0.70).
- **Rejection lost at lower floors:** the same cases drive the rejection collapse
  1.00 (0.70) → 0.67 (0.60) → 0.00 (0.50), FP rate 0.00 → 0.33 → 1.00.

**Conclusion:** the `0.70` floor **does** truncate some correct vector candidates
(recall cost), but it is the only tested floor that preserves full rejection
(6/6, FP 0.00). This is a deliberate integrity trade-off. **The floor must not be
lowered** to reclaim the truncated recall.

This analysis is strictly separated from the RQ-005 dataset/scoring audit; no dataset
or scoring artifact is attributed as a retrieval finding here.

---

## 10. Recommendations (with expected metric deltas)

Each recommendation cites an operator evidence path and states an expected delta or
*no expected change*. No delta is invented beyond the measured evidence. **No
recommendation lowers the 0.70 floor or weakens the rejection rule.**

| # | Recommendation | Expected metric delta | Evidence | Risk / cost |
| --- | --- | --- | --- | --- |
| R1 | **Keep** `nomic-embed-text`, `CHUNK_SIZE=50/20`, floor `0.70` | **No expected change** (already best/rejection-preserving among tested configs; 30/100 give R@4 0.48 < 0.52) | `rq-024` Phase 3 + Phase 4 | none |
| R2 | **Do not invest in fusion weighting** (keep equal-weight RRF or vector-only) | **No expected change** — vector-only == hybrid at 0.70 (0.42/0.52/0.22) | `rq-024` Phase 4; floor-0.70 capture | no weighted-RRF evidence exists |
| R3 | **Do not enlarge the candidate pool** (keep `TOP_K=4`) | **No expected change** — `TOP_K=8` → R@8 0.52, identical to R@4 | `rq-004-operator-topk8-nomic-expanded.txt` | none |
| R4 | **Do not lower the floor** | **Negative** — +0.15 R@4 but rejection 1.00 → 0.67/0.00 and FP 0.00 → 0.33/1.00 | floor-0.50/0.60/0.70 captures | citation-integrity loss; violates acceptance |
| R5 | **Treat cross-lingual retrieval as an embedding/recall gap owned outside ranking** — needs a multilingual embedding model or non-embedding strategy (query translation / language-aware retrieval) | **Potential positive on en-vi/vi-en (currently 0.00), unquantified here** — no ranking/candidate change fixes it | `rq-024` Phase 4; `rq-003` matrix | requires authorized model download / new strategy; out of RQ-004 scope |

**Constraint for every proposed change:** any ranking/candidate change must preserve
**section dedup/expansion semantics** and **citation behaviour** — canonical source
identity, document-global chunk ordering, the shared citation parser, and atomic
replacement. The evidence-recall change 0.42 → 0.48 in the 0.70 capture happens only via
expansion, confirming that dedup/expansion is load-bearing and must not be weakened.

**Stop condition (consistent with `PLAN-RAG-024`):** recommend **no change** where the
evidence shows no gain — fusion weighting (R2), candidate-pool sizing (R3), and floor
changes (R4).

### 10.1 Experimental-tooling removal proposal (recommendation only)

The experimental ranking/sweep tooling described in `docs/rq-024-operator-recovery-evidence.md`
Phase 1 — `internal/retrieval/ranking.go` (PathMode/BuildOptions/Fuse/RunConfiguration),
`internal/retrieval/sweep.go` (synthetic `FixtureSearcher` harness), and the
`FuseRankingsWeighted` change in `internal/retrieval/hybrid.go` — is **unused by
production** (`cmd/*` does not import it). It is **proposed for removal**.

**Important caveat:** in this checkout, `ranking.go`, `sweep.go`, `ranking_test.go`,
`sweep_test.go` are **absent** from `internal/retrieval/` (only `filter.go`, `hybrid.go`,
`hybrid_test.go`, `integration_test.go`, `language_test.go`, `pipeline.go`,
`pipeline_test.go`, `retrieval.go` are present), and `hybrid.go` contains **no** weighted
variant. The tooling is therefore recorded **only as described by the operator document**,
not as present here. **This task performs no removal.** Any actual removal is **deferred
to operator authorization.**

---

## 11. Reproduction commands and evidence paths

**Evidence paths (operator-run, authoritative):**

- `docs/rq-024-operator-recovery-evidence.md` — Phase 4 (floor/candidate/vector-vs-hybrid/cross-lingual)
- `docs/rq-004-operator-floor-0.50-nomic-expanded.txt`
- `docs/rq-004-operator-floor-0.60-nomic-expanded.txt`
- `docs/rq-004-operator-floor-0.70-nomic-expanded.txt`
- `docs/rq-004-operator-topk8-nomic-expanded.txt`
- `docs/rq-003-embedding-model-matrix.md` (cross-lingual corroboration)
- `evals/sweep-records/SYNTHETIC-NOT-MEASURED.md` (exclusion rationale)

**Reproduce (operator-side, disposable DB required):**

```
EVAL_SIMILARITY_FLOOR=0.70 EVAL_TOP_K=4 go run ./cmd/eval -dataset evals/retrieval-vi-en-expanded.json -corpus evals/corpus-vi-en.json
```

**Validation commands run for this report (agent-side):**

```
go build ./...        # pass
go test ./...         # pass (all packages ok, incl. internal/retrieval)
go vet ./...          # pass
```

`internal/retrieval` unit and integration tests pass (`go test ./...`), confirming the
report records no production change and the suite is green. Integration tests run under
their existing harness (serialized); no user-owned working-tree change was touched.

---

## 12. Acceptance reconciliation

| Acceptance criterion | Where satisfied |
| --- | --- |
| Measurements at the `0.70` floor; floor never lowered | §3 (C0/C1/C6/C7), §4 (bold 0.70 row), §9 conclusion |
| Experiment record per configuration (config, hashes, per-kind, latency, exact command) | §3 table + command block; per-kind/latency marked *not separately measured* where absent |
| Proposed changes preserve dedup/expansion + citation behaviour | §10 constraint paragraph |
| Validation: `internal/retrieval` unit + integration tests pass; sweep evidence captured | §11 (`go build`/`go test`/`go vet` pass); sweep evidence quarantined §2 |
| Operator evidence (reconciled 2026-10-12): floor sensitivity 0.67@0.50/0.60 vs 0.52@0.70; rejection 0.00/0.67/1.00; TOP_K=8 → R@8 0.52 no gain; vector==hybrid at 0.70; cross-lingual 0.00 | §4, §5, §6, §7 |
| `evals/sweep-records/*` SYNTHETIC and not cited | §2 (statement); no synthetic value in any table |
| Experimental tooling (`ranking.go`, `sweep.go`, `FuseRankingsWeighted`) unused, proposed for removal | §10.1 |

---

## 13. Non-claims / limitations

- **No new live measurement** was performed for this report (no disposable DB /
  `CREATEDB` privilege; no model download). All numbers are operator-run, copied from
  the captures above.
- **Per-kind monolingual metrics (en-en, vi-vi, misleading)** and **weighted-RRF**, 
  **`FINAL_K`**, and **per-query latency** are **not separately measured** — the
  operator captures do not expose them; no value was fabricated.
- This report is distinct from the RQ-005 dataset/scoring audit; no scoring artifact is
  reported as a retrieval finding.

*Frozen inputs, the `0.70` threshold, and the historical record are unchanged. The floor
was never lowered to pass.*
