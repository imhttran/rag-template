# RQ-008 — Final acceptance and historicalization candidate

**Type:** Operator/SOP-produced final acceptance note (RQ-008 deliverable). **Documentation only.**
**No implementation required.** This note records the outcome of the RQ-001–RQ-007 investigation
series, records the RQ-007 production decision as **DEFERRED** (backlog only), enumerates the
preserved baselines, and asserts that **no production default changed**. It marks the plan as a
candidate for `/sop-historicalize`, performed only as a separate, explicit, later step.

This note is **self-contained**: it consolidates the RQ-001–RQ-006 outcomes and the RQ-007
deferral without re-deriving them. Every figure below is quoted verbatim from a named capture
path; no number is fabricated and `n/m` cells remain `n/m`.

---

## 1. Series outcome summary (RQ-001–RQ-006)

| RQ | Deliverable | Outcome | Source capture(s) |
| --- | --- | --- | --- |
| RQ-001 | Evaluation baseline and failure classification | Baseline reproduced exactly at floor `0.70`; cross-lingual (`en-vi`/`vi-en`) is the dominant failure (candidate misses upstream of ranking); floor-trimming accounts for the 5 monolingual failures; 6/6 rejection intact | `docs/rq-001-baseline-failure-classification.md`, `docs/rq-001-per-case-eval-capture-nomic-070-expanded-20261012.txt`, `docs/rq-001-prefilter-dump-nomic-070-expanded-20261012.txt` |
| RQ-002 | Chunk-size operator captures | Both `chunk=100` and `chunk=30` give overall R@1 `0.48`, R@4 `0.48`, evidence `0.45`, rejection `6/6`; neither beats the frozen `50/20` at `0.70` (R@4 `0.52`) | `docs/rq-002-operator-chunk-100-nomic-expanded.txt`, `docs/rq-002-operator-chunk-30-nomic-expanded.txt` |
| RQ-003 | Embedding-model matrix | `nomic-embed-text`: R@1 `0.42`, R@4 `0.52`, evidence `0.48`, rejection `6/6`, `vi-vi R@1 0.80`, `en-en R@1 0.50`. `embeddinggemma`: R@1 `0.27`, R@4 `0.27`, evidence `0.27`, rejection `6/6`, `vi-vi R@1 0.60`, `en-en R@1 0.30`. Cross-lingual `0.00` for both | `docs/rq-003-embedding-model-matrix.md`, `docs/rq-003-operator-gemma-capture-expanded-20261012.txt` |
| RQ-004 | Ranking / floor candidate results | Floor is a deliberate rejection/citation-integrity control; lowering it buys recall at the cost of rejection (0.50 → R@4 `0.67`, rejection `0/6`; 0.60 → R@4 `0.67`, rejection `4/6`; 0.70 → R@4 `0.52`, rejection `6/6`). `TOP_K=8` → R@8 `0.52` (no gain); vector-only == hybrid at `0.70` (`0.42/0.52/0.22`); cross-lingual `0.00` everywhere. **Floor must not be lowered** | `docs/rq-004-ranking-candidate-results.md`, `docs/rq-004-operator-floor-0.50-nomic-expanded.txt`, `docs/rq-004-operator-floor-0.60-nomic-expanded.txt`, `docs/rq-004-operator-floor-0.70-nomic-expanded.txt`, `docs/rq-004-operator-topk8-nomic-expanded.txt` |
| RQ-005 | Dataset/scoring audit (+ addendum) | **PASS** — read-only audit; no confirmed dataset/labeling/scoring artifact in the inspected window; addendum closes the sole flagged *unverified* gap (`examples/commercial-servicing-manual.md` headings/evidence **14/14** present); **no dataset edit proposed**; §7 separation statement confirms nothing relaxes the `0.70` acceptance rule | `docs/rq-005-dataset-scoring-audit.md`, `docs/rq-005-dataset-scoring-audit-addendum.md` |
| RQ-006 | Bilingual-retrieval experiment + decision record | D2 (guard + baseline-first) is the single eligible candidate: rejection `6/6`, cross-lingual R@4 `0.00 → 0.10` (`en-vi 0.00 → 0.20`), `vi-en` `0.00`, no monolingual regression (`vi-vi R@1 0.80`, `en-en R@1 0.50`), zero variance across 3 repeats. Production integration **deferred** | `docs/rq-006-decision-record.md`, `docs/rq-006-deterministic-armD-validation.md`, `docs/rq-006-rejection-preserving-results.md`, `docs/rq-006-deterministic-matrix.txt`, `docs/rq-006-rejection-arm-matrix.txt`, `docs/rq-006-query-cache.json`, `docs/rq-006-bilingual-retrieval-experiment-design.md`, `docs/rq-006-diag-armA.txt`, `docs/rq-006-diag-armC.txt`, `docs/rq-006-diag-armD2.txt` |

### 1.1 RQ-006 consolidated comparison table (all candidate configurations × held-out metrics at floor `0.70`)

Reproduced verbatim from `docs/rq-006-decision-record.md` §V3; every cell matches its captured
evidence row. `n/m` = not separately measured in the row's capture (no value fabricated).

| # | configuration | floor | overall R@1 | overall R@4 | evidence | rejection | cross-ling R@4 | en-vi R@4 | vi-en R@4 | vi-vi R@1 | en-en R@1 | capture path |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| P1 | RQ-002 chunk=100, nomic | 0.70 | 0.48 | 0.48 | 0.45 | 6/6 | 0.00 | 0.00 | 0.00 | n/m | n/m | `docs/rq-002-operator-chunk-100-nomic-expanded.txt` |
| P2 | RQ-002 chunk=30, nomic | 0.70 | 0.48 | 0.48 | 0.45 | 6/6 | 0.00 | 0.00 | 0.00 | n/m | n/m | `docs/rq-002-operator-chunk-30-nomic-expanded.txt` |
| P3 | RQ-003 nomic | 0.70 | 0.42 | 0.52 | 0.48 | 6/6 | 0.00 | 0.00 | 0.00 | 0.80 | 0.50 | `docs/rq-003-embedding-model-matrix.md` |
| P4 | RQ-003 embeddinggemma | 0.70 | 0.27 | 0.27 | 0.27 | 6/6 | 0.00 | 0.00 | 0.00 | 0.60 | 0.30 | `docs/rq-003-embedding-model-matrix.md` |
| P5 | RQ-004 floor 0.50 (trade-off only) | 0.50 | 0.48 | 0.67 | 0.64 | 0/6 | 0.00 | n/m | n/m | n/m | n/m | `docs/rq-004-operator-floor-0.50-nomic-expanded.txt` |
| P6 | RQ-004 floor 0.60 (trade-off only) | 0.60 | 0.48 | 0.67 | 0.64 | 4/6 | 0.00 | n/m | n/m | n/m | n/m | `docs/rq-004-operator-floor-0.60-nomic-expanded.txt` |
| P7 | RQ-004 floor 0.70, TOP_K=4 (vector==hybrid) | 0.70 | 0.42 | 0.52 | 0.48 | 6/6 | 0.00 | 0.00 | 0.00 | n/m | n/m | `docs/rq-004-operator-floor-0.70-nomic-expanded.txt` |
| P8 | RQ-004 TOP_K=8 (pool widened; no gain) | 0.70 | n/m | 0.52 | n/m | 6/6 | 0.00 | n/m | n/m | n/m | n/m | `docs/rq-004-operator-topk8-nomic-expanded.txt` |
| A | RQ-006 baseline (Arm A) | 0.70 | 0.42 | 0.52 | 0.52 | 6/6 | 0.00 | 0.00 | 0.00 | 0.80 | 0.50 | `docs/rq-006-deterministic-matrix.txt` |
| B | bilingual expansion (fused) | 0.70 | 0.37 | 0.63 | 0.65 | **0–1/6** | 0.20–0.40 | n/m | n/m | n/m | n/m | `docs/rq-006-rejection-arm-matrix.txt` |
| C | translation (fused) | 0.70 | 0.39 | 0.63 | 0.64 | **0–1/6** | 0.10–0.30 | n/m | n/m | n/m | n/m | `docs/rq-006-rejection-arm-matrix.txt` |
| D1 | guard + full fusion | 0.70 | 0.42 | 0.64 | 0.61 | 6/6 | 0.30 | 0.60 | 0.00 | 0.80 | **0.40** | `docs/rq-006-deterministic-matrix.txt` |
| **D2** | **guard + baseline-first (eligible)** | 0.70 | 0.42 | 0.55 | 0.52 | **6/6** | **0.10** | **0.20** | 0.00 | 0.80 | 0.50 | `docs/rq-006-deterministic-matrix.txt` |
| E | cross-language agreement (≥2 paths) | 0.70 | 0.37 | 0.64 | 0.65 | 6/6 | 0.20–0.40 | n/m | n/m | **0.67** | n/m | `docs/rq-006-rejection-arm-matrix.txt` |

Two evidence semantics are recorded separately and **not collapsed**: harness `0.52` vs
`cmd/eval` `0.48`. The harness expands each arm's own top-`finalK` distinct sections (`0.52`);
`cmd/eval` fuses `finalK` docs then dedups (`0.48`). Both are reported; neither is collapsed.

---

## 2. RQ-007 — DEFERRED production bilingual-retrieval decision

Source: `docs/rq-007-deferred-bilingual-backlog.md` (traceable to `docs/rq-006-decision-record.md`
and `docs/rq-006-deterministic-armD-validation.md`).

- **Decision: DEFERRED.** Production bilingual retrieval is deferred. D2 is a real but **small**
  answerability improvement (`1/10` cross-lingual; rejection preserved; no monolingual
  regression; reproducible). It is **not worth a production bilingual-retrieval path as-is**.
- **No production code change.** `internal/retrieval`, `cmd/rag`, `cmd/eval` scoring, and all
  production defaults remain **frozen** and byte-identical. **No production bilingual-retrieval
  path was implemented by RQ-008 (or RQ-007).**
- **D2 measured result** (deterministic, identical across 3 repeats, pinned decoding + query
  cache): rejection **`6/6`**; cross-lingual R@4 **`0.00 → 0.10`** (`en-vi 0.00 → 0.20`);
  `vi-en` R@4 `0.00`; `vi-vi` R@1 `0.80` (unchanged); `en-en` R@1 `0.50` (unchanged);
  variance across 3 repeats **zero**.
- **Single recovered case:** `eval-en-vi-02` (recovered at Recall@4, `r4=1.00`; original-query
  guard accepted it, `guardOK=true`, 4 candidates).

### 2.1 Backlog (two unresolved items)

1. **A rejection-preserving cross-lingual strategy** — keep the original-query rejection guard
   and use translated/expanded candidates only where they cannot lower rejection (the D2 shape:
   pin the original query's top-1 at rank 1, fill ranks 2–4 from the fused translated ranking).
2. **An authorized multilingual embedding model** — for the unresolved `vi-en` direction; the
   `vi-en` miss is **upstream of ranking** (embedding miss), so only a stronger embedding model
   (or an authorized non-embedding strategy) can address it. `vi-en` remains `R@4 0.00` in
   **every** arm.

Any future production integration of either item requires **separate authorization** and is not
authorized or performed here.

---

## 3. Preserved baselines (unchanged)

None of the following was changed by RQ-001–RQ-008:

| Item | Preserved value | Source |
| --- | --- | --- |
| Frozen held-out dataset | `evals/retrieval-vi-en-expanded.json` sha256 `7f6926df…cbd2d0df` (39 cases = 33 answerable + 6 unanswerable) | `docs/rq-006-decision-record.md` §V2 |
| Frozen corpus manifest | `evals/corpus-vi-en.json` sha256 `2ddbef2f…fbc87b21` (8 docs, 83 chunks @50/20) | `docs/rq-006-decision-record.md` §V2 |
| Acceptance floor | `MIN_SIMILARITY=0.70` (unchanged; never lowered to pass) | `docs/rq-004-ranking-candidate-results.md` §4, §9 |
| Chunk size | `CHUNK_SIZE=50` / overlap `20` | `docs/rq-003-embedding-model-matrix.md` |
| Embedding model | `nomic-embed-text` (dim `768`) | `docs/rq-003-embedding-model-matrix.md` |
| Rejection accuracy | `6/6` (rate `1.00`, FP rate `0.00`) | `docs/rq-006-decision-record.md`, `docs/rq-004-ranking-candidate-results.md` §4 |
| Monolingual Vietnamese | `vi-vi R@1 0.80` | `docs/rq-006-decision-record.md` §V2 |
| Monolingual English | `en-en R@1 0.50` | `docs/rq-006-decision-record.md` §V2 |
| A/D1/D2 matrix and P1–P8 matrix | retained without edit (§1.1 above) | `docs/rq-006-deterministic-matrix.txt`, `docs/rq-006-rejection-arm-matrix.txt` |
| Two evidence semantics | harness `0.52` vs `cmd/eval` `0.48` (not collapsed) | `docs/rq-006-decision-record.md` §V3 |

**Explicit assertion.** **No production default changed.** The frozen datasets and the `0.70`
threshold are **unchanged**, and the historical **RAG-023** records (`docs/plans/PLAN-RAG-023.md`,
`PLAN-RAG-023-decisions.md`, `PLAN-RAG-023-ingest-batch-insert.md`, `PLAN-RAG-023E-decisions.md`)
are **untouched**. `PLAN-RAG-023-decisions.md` A5 records documentation-only decisions and
states "No code changed".

---

## 4. Historicalization is a separate explicit step

This plan (`RQ-008`) is a **candidate** for `/sop-historicalize` (workflow documented in
`docs/sop-defect-historicalize-stale-approval.md`). **Historicalization is a separate, explicit,
operator-invoked SOP step and is NOT performed by RQ-008.** No historicalization action is
included in any RQ-008 stage or performed by this note. No historical record is archived,
compacted, or moved here; the plan is only marked as a candidate.

---

## 5. No unapproved implementation required

RQ-007 is a **documentation deferral**, not an implementation requirement. No production
bilingual-retrieval path is built or required by RQ-008. No production code
(`internal/retrieval`, `cmd/rag`, `cmd/eval`), dataset, threshold, or RAG-023 document is
modified by this task. **No unapproved implementation is required or performed.**

---

## 6. RQ-008 acceptance-criteria checklist

| # | Acceptance criterion | Status | Evidence |
| --- | --- | --- | --- |
| 1 | Documentation file records RQ-001–RQ-006 outcomes with each figure traceable to a named capture path; no number fabricated; `n/m` cells remain `n/m` | **Satisfied** | §1, §1.1 (each row cites its capture path; `n/m` cells preserved) |
| 2 | Note records RQ-007 as DEFERRED with its backlog items and states no production bilingual-retrieval path was implemented | **Satisfied** | §2, §2.1 |
| 3 | Note enumerates preserved baselines (frozen dataset/corpus hashes, `MIN_SIMILARITY=0.70`, `CHUNK_SIZE=50/20`, `nomic-embed-text` dim 768, rejection `6/6`, `vi-vi R@1 0.80`, `en-en R@1 0.50`) and asserts no production default changed and RAG-023 records untouched | **Satisfied** | §3 |
| 4 | Note states the plan is a candidate for `/sop-historicalize` but historicalization is a separate explicit step, not performed by RQ-008 | **Satisfied** | §4 |
| 5 | Every RQ-008 acceptance criterion is marked satisfied or explicitly operator-waived; the note states no unapproved implementation is required | **Satisfied** | This checklist (§6); §5 |
| 6 | Only the new documentation file is created; no production code, dataset, threshold, or RAG-023 document modified | **Satisfied** | §3 assertion; RQ008-S2 repository inspection (below) |

No RQ-008 acceptance criterion is waived. All are satisfied without implementation.

---

## 7. Validation record (RQ008-S2)

`RQ-008` is a **documentation-only** change. The full SOP gate set is run as required by the
RQ-008 acceptance criteria, and a repository preservation check (`git status` / `git diff`)
confirms that no production default, frozen dataset, `0.70` threshold, or historical RAG-023
record was altered.

- **Full SOP gate set:** `go build ./...`, `go test ./...`, `go vet ./...` — run by SOP for
  this documentation-only change; outcome recorded by SOP on the RQ-008 gate.
- **Preservation check:** `git status` / `git diff` confirm the only RQ-008 addition is this
  documentation file; `internal/retrieval`, `cmd/rag`, `cmd/eval`, the frozen datasets, the
  `0.70` threshold, and the `docs/plans/PLAN-RAG-023*.md` records are unmodified.
- **Capture verification:** every capture path cited in §1–§3 exists in the working tree, and
  every quoted figure matches its source capture (including `n/m` cells). The dual evidence
  semantics (`0.52` harness vs `0.48` `cmd/eval`) are preserved and not collapsed (§1.1).
- **Waivers:** none required; no acceptance criterion is unsatisfiable, so no operator waiver is
  recorded.
- **Historicalization:** confirmed to be a separate explicit step; no historicalization action
  is performed (§4).

---

## 8. Non-claims / limitations

- This is a consolidation/acceptance note; it performs **no** new measurement, experiment,
  model download, or database write, and re-runs no RQ-001–RQ-006 evaluation.
- All figures are quoted verbatim from the captures named in §1–§3; any value absent from a
  capture is recorded as `n/m` (`not separately measured`) rather than inferred.
- The `vi-en` direction remains `R@4 0.00` in every arm; the small D2 gain (`1/10` cross-lingual)
  is candidate availability, not a synthetic metric. The decision to defer production
  integration stands.

*Frozen inputs, the `0.70` threshold, and the historical RAG-023 record are unchanged. No
production default changed. Historicalization is a separate explicit step.*
