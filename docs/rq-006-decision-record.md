# RQ-006 — Decision record

**Type:** Operator-produced decision record (RQ-006 deliverable). References raw captures by
path; it does **not** embed their contents. No production retrieval change.

## Inputs and configuration
- Held-out split `evals/retrieval-vi-en-expanded.json` sha256 `7f6926df…cbd2d0df` (39 cases:
  33 answerable + 6 unanswerable); corpus `evals/corpus-vi-en.json` sha256 `2ddbef2f…fbc87b21`.
- Disposable DB `rag_021_test`; `nomic-embed-text` (dim 768); floor `MIN_SIMILARITY=0.70`;
  `CHUNK_SIZE=50/20`; query model `llama3.2:3b` (installed); pinned decoding
  (`temperature=0, seed=0, num_predict=64`).

## Baseline vs D1/D2 (deterministic; identical across 3 repeats)

| arm | overall R@1 | overall R@4 | evidence | rejection | cross-ling R@4 | en-vi R@4 | vi-en R@4 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| A baseline | 0.42 | 0.52 | 0.52 | 6/6 | 0.00 | 0.00 | 0.00 |
| D1 guard + full fusion | 0.42 | 0.64 | 0.61 | 6/6 | 0.30 | 0.60 | 0.00 |
| **D2 guard + baseline-first** | 0.42 | 0.55 | 0.52 | **6/6** | **0.10** | **0.20** | 0.00 |

Evidence uses the harness's top-`finalK` distinct sections; `cmd/eval`'s recorded baseline is
`0.48` (fuses `finalK` docs then dedups). Both reported.

## Recovered case
**`eval-en-vi-02`** is the single cross-lingual case recovered by D2 at Recall@4
(`r4=1.00`). Its **original-query guard accepted it** (`guardOK=true`, 4 candidates) — so the
correct candidate is **available to the answer generator**. Candidate recall (correct section
in top-4) is `1/10` cross-lingual; **guard-accepted cross-lingual recall is also `1/10`** (the
guard did not reject the one recovered case).

## Rejection accuracy
**`6/6 = 1.00`** — every unanswerable case is guard-rejected (`guardOK=false`, 0 candidates).
D2 preserves rejection exactly; arms B/C (expansion/translation alone) do **not** (0–1/6).

## Unresolved: Vietnamese → English retrieval
All five `vi-en` cases are guard-accepted but the correct English section is **never**
retrieved (embedding miss) → `vi-en R@4 = 0.00` in every arm. The `en-vi` direction improves;
the `vi-en` direction does not.

## Reproducibility and latency limitations
- With pinned decoding + the query cache (`docs/rq-006-query-cache.json`),
  **all arms show zero variance across 3 repeats**.
- Latency: A ≈ 2 s / 39-case pass; D1/D2 ≈ 19 s (one-off generation of 117 queries; cache-served
  afterward). The overhead is the added query generation.
- Limitation: results depend on one small local model (`llama3.2:3b`); a stronger or pinned
  model could change magnitudes (not tested; no downloads).

## Decision
**Defer production integration.** D2 is a real but **small** answerability improvement
(1/10 cross-lingual; rejection preserved; no monolingual regression; reproducible). It is not
worth a production bilingual-retrieval path as-is. The findings are preserved as future backlog
(a rejection-preserving cross-lingual strategy, and an authorized multilingual embedding model
for the unresolved `vi-en` direction). **No production change recommended at this time.**

## References
`docs/rq-006-rejection-preserving-results.md`, `docs/rq-006-deterministic-armD-validation.md`,
`docs/rq-006-deterministic-matrix.txt`, `docs/rq-006-diag-armD2.txt`, `docs/rq-006-query-cache.json`.

---

## 2026-10-12 — Verification note (verify-and-annotate; append-only)

*Appended by the RQ-006 consolidation pass. This note is additive. **Every line and number
above is preserved unchanged** (append-only; no historical number rewritten, no baseline
dropped). Captures are referenced by path only; no capture contents are embedded.*

### V1. RQ-005 audit gate — PASS

The RQ-005 evaluation audit is recorded as **PASS** before any comparison or candidate
selection:

- `docs/rq-005-dataset-scoring-audit.md` — read-only audit: all inspected dataset
expectations have stored headings and substring-matchable evidence; read-only attestation;
Section 7 separation statement states nothing here relaxes the `0.70` acceptance rule; no
dataset edit applied (proposals only, operator approval required).
- `docs/rq-005-dataset-scoring-audit-addendum.md` — closes the only flagged *unverified* gap
(`examples/commercial-servicing-manual.md` headings/evidence **14/14 present**); concludes
**no dataset/scoring/corpus artifact is present** and **no dataset edit is proposed**.

Gate status: **PASS**. No confirmed dataset/labeling/scoring artifact in the inspected window;
the comparison and candidate selection below therefore cannot be a dataset/scoring artifact
mistaken for a retrieval gain.

### V2. Frozen configuration verified against the captures

| item | value | source path |
| --- | --- | --- |
| Dataset hash | `evals/retrieval-vi-en-expanded.json` sha256 `7f6926df…cbd2d0df` (39 = 33 answerable + 6 unanswerable) | `docs/rq-003-embedding-model-matrix.md`, `docs/rq-004-ranking-candidate-results.md` §1 |
| Corpus hash | `evals/corpus-vi-en.json` sha256 `2ddbef2f…fbc87b21` (8 docs, 83 chunks @50/20) | `docs/rq-003-embedding-model-matrix.md`, `docs/rq-004-ranking-candidate-results.md` §1 |
| Floor | `MIN_SIMILARITY=0.70` | `docs/rq-004-ranking-candidate-results.md` §4 |
| Chunk size | `CHUNK_SIZE=50 / overlap=20` | `docs/rq-003-embedding-model-matrix.md` |
| Embedding model | `nomic-embed-text` (dim 768) | `docs/rq-003-embedding-model-matrix.md` |
| Chat/query model | `llama3.2:3b` (installed; no download) | `docs/rq-006-deterministic-armD-validation.md` |
| Pinned decoding | `temperature=0, seed=0, num_predict=64` | `docs/rq-006-deterministic-armD-validation.md` Phase 1 |
| Query cache | `docs/rq-006-query-cache.json`, keyed by chat model + case id (stores question + `exp`/`vi`/`en` variants) | `docs/rq-006-deterministic-armD-validation.md` Phase 1 |

The existing A/D1/D2 measurements, the frozen dataset/corpus hashes, the rejection `6/6`
result, the monolingual `vi-vi R@1 0.80` / `en-en R@1 0.50` results, and the query-cache
provenance above were **verified against the captures and matched** (no correction needed).

### V3. Consolidated comparison table — all candidate configurations × held-out metrics at floor `0.70`

One row per candidate configuration; every cell matches its captured evidence row (no number
rewritten or recomputed). Rows cite their capture by path. Two evidence semantics are recorded
separately and **not** collapsed: harness `0.52` vs `cmd/eval` `0.48`.

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

`n/m` = not separately measured in the row's capture (no value fabricated). Arm-level rows A/B/C/D/E
are the RQ-006 A–E matrix (`docs/rq-006-rejection-arm-matrix.txt`, A–E × 3 reps); A/D1/D2 rows are
the pinned-decoding matrix (`docs/rq-006-deterministic-matrix.txt`, A/D1/D2 × 3 reps). Pre-RQ-006
baselines (P1–P8) are retained from RQ-002/003/004 captures without edit. Evidence semantics:
the harness expands each arm's own top-`finalK` distinct sections (`0.52`); `cmd/eval` fuses
`finalK` docs then dedups (`0.48`) — both recorded, neither collapsed.

### V4. Measured improvement vs audit artifact

- **Measured improvement (a retrieval gain):** the rejection-preserving D2 guard raises
  cross-lingual `R@4` `0.00 → 0.10` (`en-vi 0.00 → 0.20`) while rejection stays `6/6` and
  monolingual `R@1` is unchanged — a candidate-availability gain, cited to
  `docs/rq-006-deterministic-matrix.txt` (D2 × 3) and `docs/rq-006-deterministic-armD-validation.md`
  Phase 4.
- **Audit artifact (dataset/scoring conformance):** the RQ-005 findings are dataset/scoring
  conformance statements only. No confirmed labeling artifact exists in the inspected window
  and the addendum closed the sole *unverified* entry with **14/14** evidence present and **no
  dataset edit proposed** (`docs/rq-005-dataset-scoring-audit.md` §7; addendum §5). The audit
  **never relaxes the `0.70` rule**. Per the RQ-005 separation statement, audit findings are kept
  distinct from any improvement measurement, so no candidate's recall gain is attributable to a
  dataset/labeling/scoring artifact.
- **Eligibility rule (verbatim, §7.1):** *a candidate is eligible only if rejection stays `6/6`
  AND cross-lingual R@4 rises above `0.00`.* Arms **B** and **C** lower rejection to `0–1/6` and are
  therefore **ineligible** (their `0.20–0.40` cross-lingual figure is candidate recall on sections
  that should never surface). D1 is `6/6`/`0.30` but regresses `en-en R@1 0.50 → 0.40`. **D2 is the
  single eligible candidate**: rejection `6/6`, cross-lingual `R@4 0.00 → 0.10`, `vi-en 0.00`, no
  monolingual regression, zero variance across 3 repeats.
- **Candidate recall vs actual answerability:** candidate recall (correct section in top-4) is
  `1/10` cross-lingual; **guard-accepted cross-lingual recall is also `1/10`** — the guard did not
  reject the one recovered case, `eval-en-vi-02` (`guardOK=true`, 4 candidates; `r4=1.00`), so the
  correct candidate is available to the answer generator. The gain is candidate availability, not
  a synthetic metric (`docs/rq-006-deterministic-armD-validation.md`; `docs/rq-006-diag-armD2.txt`).

### V5. Ranked recommendation with limitations

| rank | candidate | rejection | cross-ling R@4 | status | cost / risk |
| --- | --- | --- | --- | --- | --- |
| 1 | **D2** guard + baseline-first | 6/6 | 0.00 → 0.10 (en-vi 0.00 → 0.20) | **eligible — recommended** | small magnitude (`1/10` cross-lingual); ≈ `+17 s` / 39-case pass (one-off generation; cache-served after); no monolingual regression |
| 2 | E cross-language agreement | 6/6 | 0.20–0.40 | eligible, **not selected** | larger gain but `vi-vi R@1 0.80 → 0.67` (−0.13); more complex than the D2 guard |
| — | B bilingual expansion | **0–1/6** | 0.20–0.40 | **ineligible** | lowers rejection; gain is on sections that should not surface |
| — | C translation alone | **0–1/6** | 0.10–0.30 | **ineligible** | lowers rejection; gain is on sections that should not surface |
| — | D1 guard + full fusion | 6/6 | 0.30 | eligible, not selected | regresses `en-en R@1 0.50 → 0.40` (unacceptable monolingual regression) |
| alt | **no change** | 6/6 | 0.00 | documented alternative | if the small `1/10` gain is judged not worth the added complexity |

Limitations: the `vi-en` direction remains `0.00` in **every** arm (embedding miss, upstream of
ranking); results depend on one small local model (`llama3.2:3b`) with no download; the corpus is
8 documents (anecdotal cross-lingual power). The ranked recommendation is consistent with
`docs/rq-006-rejection-preserving-results.md` §6/§7.4 and
`docs/rq-006-deterministic-armD-validation.md` Phase 4. **No production retrieval change is
recommended or implied here**; production integration of any guard is deferred to **RQ-007**
(evaluation-only change first).

*Traceability:* every recommendation above is traceable to a captured evidence row —
`docs/rq-006-rejection-arm-matrix.txt` (A–E × 3), `docs/rq-006-deterministic-matrix.txt`
(A/D1/D2 × 3), `docs/rq-006-diag-armD2.txt`, `docs/rq-006-query-cache.json`, and
`docs/rq-006-deterministic-armD-validation.md`.
