# RQ-006 — Rejection-preserving bilingual retrieval (results)

**Type:** Operator-run experiment. Evaluation-only harness `cmd/expbilingual`; **no production
retrieval change.** Frozen inputs, disposable DB `rag_021_test`, `nomic-embed-text`, floor `0.70`,
query generation via installed `llama3.2:3b` (no download). 3 repeats per arm.

## 1. Diagnosis — why translated/expanded queries false-positive

For the 6 frozen unanswerable cases, Arm A retrieves **0** candidates (`sim >= 0.70`), so it
rejects 6/6. Arm C (translation) retrieves **3–4** candidates per unanswerable case, all
`sim = 0.74–0.84`. The translations are **correct** (e.g. `"How do I reset my online banking
password?"`, `"What are the holiday customer service hours?"`), and they match **unrelated**
Vietnamese sections (`Thời hạn gia hạn`, `Phí trả chậm`, …) above the floor.

**Root cause:** the `0.70` floor is calibrated for **same-language** cosine similarity; the
embedding's **cross-lingual** similarities are systematically inflated, so unrelated
cross-language sections clear the floor. The failure is **neither translation drift nor fusion
logic** — it is that the rejection threshold does not transfer across languages.

**Audit — Arm A evidence `0.52` vs historical `0.48`:** this harness computes evidence recall
over **all** fused sections, whereas `cmd/eval` expands only the `FinalK=3` fused sections. Same
method across arms; R@1/R@4 match the baseline exactly.

## 2. Experiment matrix (mean over 3 repeats)

| arm | description | overall R@1 | overall R@4 | evidence | **rejection** | cross-ling R@1 | cross-ling R@4 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| A | baseline | 0.42 | 0.52 | 0.52 | **6/6** | 0.00 | 0.00 |
| B | bilingual expansion (fused) | 0.37 | 0.63 | 0.65 | **0–1/6** | 0.00–0.20 | 0.20–0.40 |
| C | translation (fused) | 0.39 | 0.63 | 0.64 | **0–1/6** | 0.00–0.10 | 0.10–0.30 |
| **D** | C + **original-query rejection guard** | 0.38 | **0.64** | 0.66 | **6/6, 6/6, 6/6** | 0.00 | 0.30 / 0.30 / 0.20 |
| **E** | C + **cross-language agreement (≥2 paths)** | 0.37 | **0.64** | 0.65 | **6/6, 6/6, 6/6** | 0.00 | 0.20 / 0.30 / 0.40 |

Per-direction (mean of 3): `en-vi` R@4 A `0.00` → D `0.47` → E `0.53`; `vi-en` → D/E `0.07`
(marginal); `vi-vi` R@4 `0.90 → 1.00`, R@1 `0.80 → 0.73/0.67`; `en-en` R@4 unchanged.

## 3. Failure cases (unanswerable, Arm C/D-gates)

| id | translated query (en) | Arm A | Arm C (no guard) |
| --- | --- | --- | --- |
| 01 | How is cryptocurrency used for payment | reject | 3 FP (0.83–0.84) |
| 02 | How do I reset my online banking password | reject | 3 FP (0.75) |
| 03 | What are the holiday customer service hours | reject | 4 FP (0.74–0.76) |
| 04 | Is there a mobile app for club members | reject | 4 FP (0.75–0.80) |
| 05 | The interest rate for auto loans | reject | 4 FP (0.76–0.81) |
| 06 | How do I close my account | reject | 4 FP (0.75–0.76) |

D's guard (reject unless the **original** query clears the floor) restores rejection 6/6; E's
guard (≥2 of 3 query vector paths clear) also restores 6/6.

## 4. Trade-offs

| option | cross-ling R@4 | rejection | monolingual | complexity |
| --- | --- | --- | --- | --- |
| B expansion | 0.20–0.40 | **fails (0–1/6)** | R@1 −0.05 | low |
| C translation | 0.10–0.30 | **fails (0–1/6)** | R@1 −0.03 | low |
| **D original-query guard** | **~0.27** | **6/6** | vi-vi R@1 0.80→0.73; R@4 ok | low |
| **E agreement ≥2 paths** | **~0.30** | **6/6** | vi-vi R@1 0.80→0.67; R@4 ok | low–med |
| C-design: translation-confidence gate | unmeasured | — | — | high |

## 5. Phase 4 acceptance

| criterion | D | E |
| --- | --- | --- |
| 1. rejection = 6/6 | ✅ | ✅ |
| 2. cross-lingual R@4 > 0 | ✅ (~0.27) | ✅ (~0.30) |
| 3. no unacceptable monolingual regression | ⚠️ vi-vi R@1 −0.07, R@4 +0.10 | ⚠️ vi-vi R@1 −0.13, R@4 +0.10 |
| 4. reproducible | ⚠️ cross-ling R@4 varies 0.20–0.30; rejection stable | ⚠️ varies 0.20–0.40; rejection stable |
| 5. no synthetic metrics | ✅ | ✅ |

**D and E meet criteria 1, 2, 5 and are borderline on 3 and 4** (recall gain at a small
monolingual R@1 cost; LLM decoding not pinned, so magnitude varies while rejection stays 6/6).

## 6. Recommended next action

- **Lead candidate for RQ-007: Arm D (original-query rejection guard)** — simplest, preserves
  rejection exactly (answerability from the original query, ranking from the fused set), and
  lifts cross-lingual R@4 to ~0.27 (en-vi ~0.47).
- **Before RQ-007 implementation:** (a) pin generation decoding (temperature/seed) and re-run
  ≥3 times to confirm reproducibility; (b) sign off the monolingual R@1 trade-off; (c) confirm
  the guard is expressed as an evaluation-only change first.
- If the operator rejects the monolingual R@1 cost or cannot pin determinism, **no production
  change** is the alternative — the cross-lingual gain is not free.

*Captures:* `docs/rq-006-rejection-arm-matrix.txt` (A–E × 3), `docs/rq-006-diag-armA.txt`,
`docs/rq-006-diag-armC.txt`, `docs/rq-006-operator-arm{A,B,C}-*.txt`.
*Operator-measured; frozen inputs, the `0.70` threshold, and RAG-023 records are unchanged.*

---

## 7. 2026-10-12 reconciliation — eligibility rule and the eligible candidate (D2)

*Added 2026-10-12. This section adds framing/eligibility text only. **Every previously
recorded number in §§1–6 above is unchanged;** the A–E matrix (§2), the failure-case table
(§3), the trade-off table (§4), the Phase-4 acceptance rows (§5), and the recommendation
(§6) are preserved verbatim. The raw per-case rows are `docs/rq-006-rejection-arm-matrix.txt`
(A–E × 3 repeats).*

### 7.1 Eligibility rule (verbatim)

> A candidate is **eligible only if rejection stays `6/6` AND cross-lingual R@4 rises above
> `0.00`**. Arms that lower rejection (**bilingual expansion / translation alone, B/C**) are
> **ineligible**.

### 7.2 Ineligibility of Arms B and C

- **Arm B (bilingual expansion) is INELIGIBLE** — it lowers rejection to `0–1/6` (§2, §4).
  In the raw matrix every `arm=B` unanswerable row is `FALSE_POSITIVE`
  (`docs/rq-006-rejection-arm-matrix.txt`, `arm=B rep=1..3 eval-unanswerable-01..06`).
- **Arm C (translation alone) is INELIGIBLE** — it lowers rejection to `0–1/6` (§2, §4).
  In the raw matrix nearly every `arm=C` unanswerable row is `FALSE_POSITIVE`
  (`docs/rq-006-rejection-arm-matrix.txt`, `arm=C rep=1..3 eval-unanswerable-01..06`).

Their cross-lingual R@4 gain (`0.20–0.40`) is therefore **candidate recall on sections that
should never have been surfaced**, not an answerability gain.

### 7.3 The eligible candidate under pinned decoding — D2

Under the **pinned-decoding** re-run (`temperature=0, seed=0, num_predict=64`; query cache
`docs/rq-006-query-cache.json`), the arm-D guard was split into two variants. The
**eligible candidate is D2**, documented in `docs/rq-006-deterministic-armD-validation.md`:

- **D2** pins the original query's top-1 at rank 1 and fills ranks 2–4 from the fused
  translated ranking, keeping the original-query rejection guard.
- **Result: rejection `6/6`; cross-lingual R@4 `0.00 → 0.10` (`en-vi 0.00 → 0.20`); no
  monolingual regression** (`vi-vi R@1` unchanged `0.80`, `en-en R@1` unchanged `0.50`);
  **zero variance across 3 repeats**.
- D1 (guard + full fusion) is **eligible but not selected**: it regresses `en-en` R@1
  `0.50 → 0.40` (`docs/rq-006-deterministic-armD-validation.md`).

Source of the eligible-candidate evidence: **`docs/rq-006-deterministic-armD-validation.md`**
(§Phase 1 determinism; §Phase 2/3 A/D1/D2 matrix; §Phase 4 acceptance) and the raw rows in
`docs/rq-006-deterministic-matrix.txt` (A/D1/D2 × 3).

### 7.4 Acceptance table — D2 as the single eligible candidate (pinned decoding)

| criterion | D2 | result |
| --- | --- | --- |
| 1. rejection `6/6` | `6/6` (all 3 reps) | ✅ |
| 2. cross-lingual R@4 rises above `0.00` | `0.00 → 0.10` (`en-vi 0.00 → 0.20`) | ✅ |
| 3. no unacceptable monolingual regression | `vi-vi R@1 0.80`, `en-en R@1 0.50` (unchanged) | ✅ |
| 4. repeatable (pinned + cache) | zero variance across 3 reps | ✅ |
| 5. runtime overhead acceptable | `≈ +17 s / 39-case pass` (one-off generation; cache-served after) | ✅ |

**D2 qualifies on all five criteria.** Arm E (cross-language agreement) remains a
non-selected **eligible** alternative (rejection `6/6`, cross-ling R@4 `0.20–0.40`, `vi-vi R@1
0.80 → 0.67`); Arms B/C are **ineligible**. If the small gain is judged not worth the added
complexity, **no change** remains the documented alternative (§6). **RQ-006 makes no
production retrieval change** (RQ-007 concern).

*Traceability:* every claim above is traceable to a captured evidence row —
`docs/rq-006-rejection-arm-matrix.txt` (A–E × 3), `docs/rq-006-deterministic-matrix.txt`
(A/D1/D2 × 3), and `docs/rq-006-deterministic-armD-validation.md`. The consolidated decision
record is `docs/rq-006-decision-record.md`.
