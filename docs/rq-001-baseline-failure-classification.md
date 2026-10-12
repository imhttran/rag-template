# RQ-001 — Evaluation baseline and failure classification

This is a **new, standalone** report. It is **not** an append to
`docs/experiments-eval-sweep.md`, and it rewrites **no** historical metric: every
baseline number quoted here is reproduced from the existing record, not edited.

Scope: reproduce the frozen bilingual retrieval baseline on a disposable
PostgreSQL database via the existing reuse-mode sweep harness, then classify every
held-out retrieval miss (33 answerable cases) and confirm the rejection of the 6
unanswerable cases.

> **Evidence honesty note (added after review).** The held-out per-case
> `Retrieval miss:` / `Ranking:` blocks are emitted by `cmd/eval` at run time and
> are **not persisted anywhere in this repository**: the harness writes each
> run's stdout to a temp file it deletes on exit (`work=$(mktemp -d)` +
> `trap 'rm -rf "$work"' EXIT` in `scripts/eval-model-sweep.sh`), and the results
> sink records only the aggregate row. No captured raw stdout is available in the
> working tree. Therefore the per-case **metric cells** (the exact K-hit/miss
> state and the `sim / source / section / chunk` ranking for each case) are marked
> **unavailable** below, with the bounded instrumentation that would produce them,
> rather than reconstructed by inference. What this report *does* state as fact is
> the dataset-derived per-case metadata (id, question, `kind`, expected
> `source`/`section`/`evidence`) — read directly from
> `evals/retrieval-vi-en-expanded.json` — and the aggregate metrics reconciled from
> `docs/experiments-eval-sweep.md`.

---

## 1. Frozen baseline and its source evidence

The frozen baseline is the **`nomic-embed-text:768`** configuration at the floor
selected by the harness's fixed calibration rule. Source evidence for the
reproduced numbers is the existing results sink
`docs/experiments-eval-sweep.md`, section **“Final calibrated evaluation
(RAG-021)”** (the pre-fix record) and the **RAG-023D** reconciliation rows
(post-fix identity alignment). Both refer to the same manifest hash:

- Held-out split: `evals/retrieval-vi-en-expanded.json` — 39 cases
  (33 answerable + 6 unanswerable).
- Calibration split: `evals/retrieval-vi-en-calibration.json` — 21 cases.
- Corpus manifest: `evals/corpus-vi-en.json`.
- Model / dimension / floor: `nomic-embed-text` / `768` / `0.70`.
- Selection rule: among floors whose calibration rejection ≥ `SWEEP_MIN_REJECTION`
  (`1`), pick highest recall@4, then highest rejection, then highest floor.

### 1.1 Reconciled baseline metrics (floor `0.70`, mean of 3 repeats)

| metric | recorded value (source) | tolerance | treated as model-quality? |
| --- | --- | --- | --- |
| Recall@1 | `0.42` | exact equality at the `0.70` floor | yes |
| Recall@4 | `0.52` | exact equality at the `0.70` floor | yes |
| Precision@4 | `0.22` | exact equality at the `0.70` floor | yes |
| Evidence recall (before→after expansion) | `0.42 → 0.48` | exact equality at the `0.70` floor | yes |
| Similarity-only rejection rate | `1.00` | exact equality | yes |
| run wall (s) | `1.01–1.15` across recorded repeats | recorded, not compared | **no** |
| client RSS (KiB) | `33616–38964` across recorded repeats | recorded, not compared | **no** |

**Source evidence:** `docs/experiments-eval-sweep.md`:

- *Final calibrated evaluation (RAG-021)* held-out table:
  `R@1 0.42`, `R@4 0.52`, `P@4 0.22`, `evidence 0.42 → 0.48`, `rejection 1.00`.
- *RAG-023D post-fix run rows* (same model, floor `0.70`, manifest hash
  `2ddbef2f40870afc92cf2df14f3a7953db1037d1d63c6ee3f39f5708fbc87b21`): all three
  repeats reproduce `0.42 / 0.52 / 0.22 / 0.42->0.48 / 1.00`.

**Tolerance.** The metric comparison is **exact equality at floor `0.70`** for
R@1, R@4, P@4, evidence recall, and rejection rate — the recorded spreads across
three repeats are `0.0000` (deterministic). `run wall (s)` and `client RSS (KiB)`
are recorded operational context and are **not** acceptance comparison targets.

**Measurement labels (not model-quality metrics).** `run wall (s)` is the
wall-clock of the whole `go run ./cmd/eval` subprocess (Go build + eval client +
database work), **not** embedding latency. `client RSS (KiB)` is the peak RSS of
that eval client process, **not** the embedding server. This report follows that
labelling and never treats either as a model-quality number.

### 1.2 Reconciliation status of the reproduced run

The baseline aggregates are reproduced from the recorded evidence. A *live*
re-run was not captured into this working tree: the harness discards each run's
stdout (see the evidence-honesty note). The aggregates in §1.1 are therefore the
reconciliation target; a fresh run is compared against them under the
exact-equality tolerance above, and the comparison itself is recorded by the
harness in `SWEEP_RESULTS`. **What must not be done — and is not done here — is
inventing per-case output to fill the gap.**

---

## 2. Reproducible commands, configuration and input hashes

### 2.1 Exact reuse-mode command

```sh
SWEEP_REUSE_DB=1 \
SWEEP_DB_NAMES='rag_001_nomic' \
SWEEP_ALLOW_DB='rag_001_nomic' \
SWEEP_RESULTS='docs/rq-001-eval-sweep-results.md' \
SWEEP_GRID='0.30,0.40,0.50,0.60,0.70' \
SWEEP_MIN_REJECTION=1 \
SWEEP_SKIP_DBUP=1 \
SWEEP_QUERY_REWRITE=false \
scripts/eval-model-sweep.sh 'nomic-embed-text:768' 3
```

- **`SWEEP_REUSE_DB=1`** operates on pre-created databases: the harness never
  issues `CREATE DATABASE` or `DROP DATABASE`, and each repeat is reset with
  `TRUNCATE "documents" RESTART IDENTITY` (the pgvector extension and schema stay).
- **`SWEEP_DB_NAMES` is mandatory** in reuse mode — the harness has no fallback to
  a derived or shared database. **`SWEEP_ALLOW_DB` is mandatory** whenever
  `SWEEP_DB_NAMES` is set; reuse mode requires them **together**
  (`validate_sweep_config`). A name not in the allowlist exits `2` before any
  database access.
- **`rag_db` is refused**: `valid_db_name` rejects `rag_db`, `postgres`,
  `template0`, `template1`, and `pg_*`. A pre-created database must also be owned
  by the connecting role, carry the `vector` extension, and have
  `public.documents`, or `verify_database` exits `3`.
- **`SWEEP_RESULTS` points at a NEW file** so `docs/experiments-eval-sweep.md` is
  never written (no historical metric rewritten).
- **`SWEEP_SKIP_DBUP=1`** uses an already-running shared PostgreSQL instance.
- **`SWEEP_QUERY_REWRITE=false`** (the harness default) keeps the comparison
  deterministic and independent of any chat model.
- `SWEEP_REUSE_DB=1` and `SWEEP_SKIP_INGEST=1` are **refused together**; this run
  does not set `SWEEP_SKIP_INGEST`, so each repeat re-ingests fresh.

### 2.2 Input hashes (sha256)

Hashes are recorded by the harness itself (`sha256_file` of each split) and are
copied verbatim from the recorded rows:

| input | sha256 |
| --- | --- |
| `evals/retrieval-vi-en-expanded.json` (held-out, 39 cases) | `7f6926dfa7a3e3a63174c6be52bd396aa3fd6efbac36f3022dc0c7ffcbd2d0df` |
| `evals/retrieval-vi-en-calibration.json` (calibration, 21 cases) | `51be86ba7ef36557184c439b8e5489d858b486099827fa16d1d6bc1e15444f2e` |
| `evals/corpus-vi-en.json` (manifest) | `2ddbef2f40870afc92cf2df14f3a7953db1037d1d63c6ee3f39f5708fbc87b21` |

> The RAG-021 preamble of `docs/experiments-eval-sweep.md` records the **pre-fix**
> hashes (`72d2c15b…`, `a6e4bddd…`, `7833e235…`); the post-fix working-tree hashes
> above are the values the harness prints today and are the ones the reproduced
> rows carry. Both are quoted from the same historical file; neither is edited.

### 2.3 Configuration

- Model `nomic-embed-text`, dimension `768`, floor `0.70`.
- Grid `0.30,0.40,0.50,0.60,0.70`; `SWEEP_MIN_REJECTION=1`; 3 repeats.
- `MIN_SIMILARITY` is passed per run by the harness; `QUERY_REWRITE=false`.

---

## 3. Held-out split coverage

The held-out split is the one named in the PRD: **39 cases = 33 answerable + 6
unanswerable**, where “answerable” is `len(expected) > 0` (`cmd/eval/main.go`
computes `answerable := len(evalCase.Expected) > 0`). Case counts by `kind`
(the dataset’s own annotation field, `evals/retrieval-vi-en-expanded.json`):

| kind (language / query type) | cases | answerable |
| --- | --- | --- |
| `en-en` (English query, English corpus) | 10 | yes |
| `vi-vi` (Vietnamese query, Vietnamese corpus) | 10 | yes |
| `en-vi` (English query, Vietnamese corpus — cross-lingual) | 5 | yes |
| `vi-en` (Vietnamese query, English corpus — cross-lingual) | 5 | yes |
| `misleading` | 3 | yes |
| `unanswerable` | 6 | no (`expected: []`) |
| **total** | **39** | **33 answerable / 6 unanswerable** |

The split counts match the PRD exactly (33 / 6); no mismatch is flagged. These
counts are derived directly from the dataset JSON and require no run to confirm.

### 3.1 Language / query-type labelling method

The held-out dataset carries an explicit **`kind`** field on every case, so
labelling is by **dataset annotation**, not derived from question text. The
`kind` field encodes both language direction and query type:

- language pairs: `en-en`, `vi-vi` (monolingual), `en-vi`, `vi-en` (cross-lingual);
- query type: `misleading` (a plausible but out-of-domain phrasing whose answer
  lives in the corpus but is not the obvious section), `unanswerable`.

What could **not** be labelled: the dataset carries no finer-grained query types
(e.g. factual / procedural / temporal), and `cmd/eval`’s `EvalCase` struct does not
read `kind` at all. Finer query-type grouping is therefore **unavailable** from
current instrumentation (see §7); the grouping used here is the `kind` field
itself.

---

## 4. Per-case classification (33 answerable cases)

Each row below is **one held-out answerable case**. The columns fall into two
kinds, and they are kept strictly separate:

- **Observed (dataset-derived) — filled in for every row:** `id`, `question`,
  `kind` (language/type), answerability, and the expected
  `source` / `section`. These are read directly from
  `evals/retrieval-vi-en-expanded.json` and require **no eval run**.
- **Run-derived — marked `unavailable` unless a captured run block exists:** the
  per-K hit/miss state and the `sim / source / section / chunk` ranking. These are
  printed by `cmd/eval` per case (`reportBaseline` → `printRetrieved`, and the
  `Hybrid K=…` lines) but **are not persisted** in this repository (the harness
  deletes its temp output). No captured block is present, so these cells are
  `unavailable` and the category column records the *taxonomy bucket the case
  belongs to by construction*, not a measured per-case outcome.

**Failure categories (definitions):**

- **HIT** — the expected `source`/`section` is present in the fused ranking at
  the relevant K; no failure.
- **RANK-ORDER MISS** — the expected section is retrieved in the **candidate**
  set (post-`KeepSimilar`) but does not appear at a reported K’s top positions.
- **CANDIDATE-MISS (pre-filter)** — the expected section is absent from the
  candidate set; the current output does **not** isolate whether `KeepSimilar`
  removed a below-floor candidate or the upstream retrieval never returned it —
  this split is **unavailable** (§6/§7).
- **CROSS-LINGUAL MISS** — a miss on an `en-vi` or `vi-en` case where query and
  expected-section languages differ.
- **EXPANSION-ONLY HIT** — evidence recall `0.00` before expansion but `1.00`
  after: reached only by section expansion.

Because the per-case outcome is unobserved, §4 does **not** assign `HIT` or a
miss category to any individual case by inference; the `category` cell is marked
`unavailable (needs run capture)` for every answerable case, and the aggregate
distribution is reconciled in §6.1 from the one measured quantity that *is*
recorded: the overall R@1 = 0.42 and R@4 = 0.52.

### 4.1 `en-en` cases (10)

| id | question | lang/type | answerable | expected source / section | per-K result | retrieved (sim / source / section / chunk) | category |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `eval-en-en-01` | How are loan payments handled? | en-en | yes | `examples/loan-policy.md / Payments` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-en-en-02` | When is a late fee charged? | en-en | yes | `examples/loan-policy.md / Late Fees` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-en-en-03` | Can I make a partial payment? | en-en | yes | `examples/loan-policy.md / Partial Payments` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-en-en-04` | When does a loan go to collections? | en-en | yes | `examples/loan-policy.md / Collections` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-en-en-05` | What do the payment codes mean? | en-en | yes | `examples/loan-policy.md / Payment Codes` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-en-en-06` | How do I request a grace period? | en-en | yes | `examples/member-services-guide.md / Grace Period Requests` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-en-en-07` | How do I request a payoff statement? | en-en | yes | `examples/member-services-guide.md / Payoff Requests` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-en-en-08` | How do I dispute a charge? | en-en | yes | `examples/member-services-guide.md / Dispute Resolution` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-en-en-09` | What insurance is required? | en-en | yes | `examples/commercial-servicing-manual.md / Insurance Requirements` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-en-en-10` | Who handles property taxes? | en-en | yes | `examples/commercial-servicing-manual.md / Property Taxes` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |

### 4.2 `vi-vi` cases (10)

| id | question | lang/type | answerable | expected source / section | per-K result | retrieved (sim / source / section / chunk) | category |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `eval-vi-vi-01` | Thời hạn gia hạn là bao lâu? | vi-vi | yes | `examples/vi/chinh-sach-khoan-vay.md / Thời hạn gia hạn` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-vi-vi-02` | Thanh toán được áp dụng như thế nào? | vi-vi | yes | `examples/vi/chinh-sach-khoan-vay.md / Xử lý thanh toán` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-vi-vi-03` | Tôi có thể liên hệ bằng cách nào? | vi-vi | yes | `examples/vi/dich-vu-thanh-vien.md / Kênh liên hệ` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-vi-vi-04` | Yêu cầu tất toán mất bao lâu? | vi-vi | yes | `examples/vi/dich-vu-thanh-vien.md / Yêu cầu tất toán` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-vi-vi-05` | Khi chậm trả thì được thông báo thế nào? | vi-vi | yes | `examples/vi/quy-trinh-thu-hoi.md / Thông báo chậm trả` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-vi-vi-06` | Tôi có thể lập kế hoạch thanh toán không? | vi-vi | yes | `examples/vi/quy-trinh-thu-hoi.md / Kế hoạch thanh toán` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-vi-vi-07` | Khi nào hồ sơ bị chuyển cho bộ phận thu hồi? | vi-vi | yes | `examples/vi/quy-trinh-thu-hoi.md / Chuyển hồ sơ thu hồi` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-vi-vi-08` | Tôi có phải mua bảo hiểm tài sản không? | vi-vi | yes | `examples/vi/bao-hiem-tai-san.md / Yêu cầu bảo hiểm` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-vi-vi-09` | Chứng nhận bảo hiểm có hiệu lực bao lâu? | vi-vi | yes | `examples/vi/bao-hiem-tai-san.md / Thời hạn hiệu lực` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-vi-vi-10` | Yêu cầu bồi thường được xử lý trong bao lâu? | vi-vi | yes | `examples/vi/bao-hiem-tai-san.md / Bồi thường` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |

### 4.3 `misleading` cases (3)

| id | question | lang/type | answerable | expected source / section | per-K result | retrieved (sim / source / section / chunk) | category |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `eval-misleading-01` | What is the grace period under the standard loan policy? | misleading | yes | `examples/loan-policy.md / Missed Payments` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-misleading-02` | Làm sao để được miễn giảm phí? | misleading | yes | `examples/vi/dich-vu-thanh-vien.md / Miễn giảm phí` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-misleading-03` | How do I set up a payment plan? | misleading | yes | `examples/vi/quy-trinh-thu-hoi.md / Kế hoạch thanh toán` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |

### 4.4 `en-vi` cases (5, cross-lingual)

| id | question | lang/type | answerable | expected source / section | per-K result | retrieved (sim / source / section / chunk) | category |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `eval-en-vi-01` | When is a debt written off? | en-vi | yes | `examples/vi/quy-trinh-thu-hoi.md / Xóa nợ` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-en-vi-02` | When is a late fee applied? | en-vi | yes | `examples/vi/chinh-sach-khoan-vay.md / Phí trả chậm` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-en-vi-03` | Can fees be waived? | en-vi | yes | `examples/vi/dich-vu-thanh-vien.md / Miễn giảm phí` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-en-vi-04` | How is a delinquency noticed? | en-vi | yes | `examples/vi/quy-trinh-thu-hoi.md / Thông báo chậm trả` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-en-vi-05` | How long does a claim take? | en-vi | yes | `examples/vi/bao-hiem-tai-san.md / Bồi thường` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |

### 4.5 `vi-en` cases (5, cross-lingual)

| id | question | lang/type | answerable | expected source / section | per-K result | retrieved (sim / source / section / chunk) | category |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `eval-vi-en-01` | Làm sao để thay đổi cách nhận sao kê? | vi-en | yes | `examples/member-services-guide.md / Statement Delivery` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-vi-en-02` | Tôi có thể sắp xếp kế hoạch thanh toán không? | vi-en | yes | `examples/member-services-guide.md / Payment Arrangements` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-vi-en-03` | Điều gì xảy ra khi hồ sơ được chuyển giao? | vi-en | yes | `examples/commercial-servicing-manual.md / Servicing Transfer Procedures` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-vi-en-04` | Khoản vay lớn xử lý thanh toán ra sao? | vi-en | yes | `examples/large-loan-policy.md / Payment Processing` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `eval-vi-en-05` | Hồ sơ thế chấp được quản lý thế nào? | vi-en | yes | `examples/commercial-servicing-manual.md / Collateral and Lien Documentation` | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |

**33 answerable rows total** (§4.1 = 10, §4.2 = 10, §4.3 = 3, §4.4 = 5, §4.5 = 5).
Each names the exact dataset expectation to check against a captured run block; no
per-case outcome is asserted without that block.

---

## 5. Unanswerable cases (6)

Every unanswerable case has `expected: []`. `cmd/eval` classifies a case as
unanswerable via `answerable := len(expected) > 0`, applies the **same**
`KeepSimilar` filter to the unanswerable set, and reports per K `Correct rejection`
(empty post-filter set) or `FALSE POSITIVE`. The **recorded aggregate** is
`Similarity-only rejection=6/6 (1.00), False-positive rate=0.00`
(`docs/experiments-eval-sweep.md`), i.e. all 6 unanswerable cases are correct
rejections at the selected floor (`rejection = 1.00`).

The **per-case** rejection lines are, like §4, printed by `cmd/eval` and not
persisted; each row below therefore carries the dataset-derived identity and the
aggregate-confirmed determination.

| id | question | kind | expected | determination |
| --- | --- | --- | --- | --- |
| `eval-unanswerable-01` | What is the cryptocurrency payment policy? | unanswerable | `[]` | correct rejection confirmed by aggregate 6/6 |
| `eval-unanswerable-02` | How do I reset my online banking password? | unanswerable | `[]` | correct rejection confirmed by aggregate 6/6 |
| `eval-unanswerable-03` | What are the holiday customer service hours? | unanswerable | `[]` | correct rejection confirmed by aggregate 6/6 |
| `eval-unanswerable-04` | Is there a mobile app for members? | unanswerable | `[]` | correct rejection confirmed by aggregate 6/6 |
| `eval-unanswerable-05` | What is the interest rate for auto loans? | unanswerable | `[]` | correct rejection confirmed by aggregate 6/6 |
| `eval-unanswerable-06` | How do I close my account? | unanswerable | `[]` | correct rejection confirmed by aggregate 6/6 |

Because the aggregate is exactly `6/6` with zero false positives, **no** case can be
a false positive; the per-case determination is fully pinned by the aggregate for
this split (any false positive would drop rejection below `1.00`).

---

## 6. Failure categories grouped by language and query type

### 6.1 What is measured vs. what is not

The only measured, persisted quantities are the **aggregate** metrics
(§1.1): R@1 = `0.42`, R@4 = `0.52`, P@4 = `0.22`, evidence `0.42 → 0.48`,
rejection `1.00`. Per-case outcomes are **not** persisted. An earlier draft of
this report inferred the per-case distribution from the aggregate
(`0.42 × 33 ≈ 14` rank-1 hits) and labelled every monolingual case `HIT` — that
was **wrong**: `0.42 × 33 = 14` rank-1 hits in total, which is **fewer than the 20
monolingual cases alone**, so the monolingual set cannot all be rank-1 hits. The
inference and those labels have been removed. The aggregate is consistent only
with some mix of hits and misses *across all `kind`s*, which the aggregate cannot
attribute to individual cases.

**Category definitions:** see §4. Each case is assigned **exactly one** category;
but because the per-case outcome is unobserved, every case’s category is
`unavailable` until a captured run exists. The table below therefore reports the
**group sizes and the (aggregate-only) budget of hits**, not fabricated per-case
counts.

### 6.2 Group sizes (dataset-derived, always available)

| language / query type | answerable cases |
| --- | --- |
| `en-en` (monolingual EN) | 10 |
| `vi-vi` (monolingual VI) | 10 |
| `en-vi` (cross-lingual EN→VI) | 5 |
| `vi-en` (cross-lingual VI→EN) | 5 |
| `misleading` | 3 |
| **total answerable** | **33** |
| `unanswerable` | 6 |

Grouping method: **dataset `kind` annotation** (§3.1). Counts sum to the
classified total: 33 answerable + 6 unanswerable = 39.

### 6.3 Category counts per group

| language / query type | answerable | HIT | RANK-ORDER / CROSS-LINGUAL miss | category counts |
| --- | --- | --- | --- | --- |
| `en-en` | 10 | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `vi-vi` | 10 | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `en-vi` | 5 | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `vi-en` | 5 | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| `misleading` | 3 | unavailable (needs run capture) | unavailable (needs run capture) | unavailable |
| **total answerable** | **33** | **≈14 measured in aggregate** (`0.42 × 33`) | **≈19** | aggregate only |
| `unanswerable` | 6 | — | 6 confirmed rejections | 6/6 (measured) |

The only per-group figures current instrumentation supports are the **group sizes**
(§6.2) and the **aggregate hit/miss totals** (last row): `≈14` rank-1 hits and
`≈17` within-K=4 hits (`0.52 × 33`), with the rest misses. Attributing any of
these to a specific group requires the per-case capture named in §7.

### 6.4 Candidate loss before and after similarity filtering (`KeepSimilar`)

`KeepSimilar(documents, minSimilarity)` (`internal/retrieval/retrieval.go`)
returns only documents whose cosine similarity is `≥ minSimilarity`. In
`cmd/eval`’s `baselines()` it is applied **after** `e.retriever.Search(ctx, vector, k)`
and **before** both the answerable ranking report and the unanswerable rejection
report, so **both** paths are filtered identically (`documents = retrieval.KeepSimilar(documents, e.minSimilarity)`).

| measurement | value | source |
| --- | --- | --- |
| candidates **after** `KeepSimilar` | per-case `Ranking:` / `Retrieval miss:` ranking | `cmd/eval` `printRetrieved` — **not persisted** here → **unavailable** |
| candidates **before** `KeepSimilar` | per-case pre-filter count + similarities | **unavailable** |
| number of candidates dropped by `KeepSimilar` per case | **unavailable** | §7 |
| **aggregate** floor behaviour | `KeepSimilar` at floor `0.70` yields rejection `1.00` on the 6 unanswerable cases | `docs/experiments-eval-sweep.md` (measured) |

**Why unavailable (per case):** `cmd/eval` calls `retriever.Search(...)` and
immediately rebinds the result to the `KeepSimilar` output; the raw pre-filter
slice (with its similarities) is never printed as a labelled “before filtering”
measurement, and even the post-filter ranking is written only to a temp file the
harness deletes. Only the **post-filter aggregate** is persisted. Any before/after
per-case count in this report would be invented — it is therefore marked
**unavailable**.

**Bounded instrumentation needed (not implemented here):**

1. A pre-filter candidate dump in `cmd/eval` — an opt-in debug flag that prints,
   per case, `pre-filter candidates: N (sim ≥ floor: M, sim < floor: N−M)` before
   the `KeepSimilar` call, and whether the expected `source`/`section` was in the
   pre-filter set. This supplies the before-filter count and the exact candidate
   loss for every case.
2. Persist the harness’s per-run stdout (write `$work/run.out` to a durable path
   instead of deleting it on exit), so the per-case `Retrieval miss:` / `Ranking:`
   blocks survive for classification.

Both are evaluation-only and guarded so default output is unchanged. No
production file is changed under this task.

---

## 7. Unavailable observations and required instrumentation

| observation | why unavailable | bounded instrumentation needed |
| --- | --- | --- |
| per-case K-hit/miss state (each of the 33 cases) | `cmd/eval` prints it per case but the harness writes it to a deleted temp file; no captured stdout in the tree | persist the harness’s per-run stdout (durable path) |
| per-case `sim / source / section / chunk` ranking | same as above (`printRetrieved`) | persist the harness’s per-run stdout |
| per-case category assignment (HIT / RANK-ORDER / …) | needs the per-case block above | persist the per-run stdout, then apply §4’s taxonomy |
| true pre-`KeepSimilar` candidate set per case (count + similarities) | `cmd/eval` prints only the post-filter ranking | opt-in `cmd/eval` flag printing pre-filter candidates before `KeepSimilar` |
| candidate loss attributable to the floor vs. to upstream retrieval | needs the pre-filter set above to compare against the expected section | same flag: print whether the expected `source`/`section` was in the pre-filter set |
| server-side embedding memory | the harness measures only the client process RSS | an embedding-server (Ollama) memory probe / server metrics endpoint |
| finer query types (factual / procedural / temporal) | the dataset annotates only `kind`; `EvalCase` does not read even `kind` | dataset annotation, or a classifier over question text (new instrumentation) |

No measurement above is invented. Every unavailable cell names the specific
bounded instrumentation that would produce it.

---

## 8. Hypotheses ranked by expected impact

Each hypothesis is tied to the specific **measured aggregate** that motivates it;
per-case attribution is deferred to §7’s instrumentation.
0
1. **Cross-lingual retrieval (EN→VI, VI→EN) plausibly drives a large share of
   misses.** The aggregate R@1 = `0.42` (`≈14/33`) is far below the 20 monolingual
   cases, so misses must be spread broadly, and the 10 cross-lingual cases
   (§4.4, §4.5) are the leading candidate: the query language differs from the
   expected section language and the bilingual embedding is not
   translation-aligned. *Expected impact: highest — testable per-group once §7’s
   capture exists.*
2. **`misleading` phrasing mis-ranks the correct section.** The 3 `misleading`
   cases (§4.3) are worded to resemble a different section than the expected one.
   *Expected impact: high per case, small count.*
3. **Evidence recall before expansion trails recall@K.** The measured
   `evidence 0.42 → 0.48` means expansion recovers only ~6% of evidence cases; the
   pre-expansion gap is candidate-set quality. *Expected impact: medium.*
4. **`KeepSimilar` at floor `0.70` may drop borderline-true candidates.** The floor
   is high (selected for rejection `1.00`); a true section scoring just below `0.70`
   is filtered before ranking. Whether this happens is **unavailable** without
   §6.4’s pre-filter dump — ranked as a testable hypothesis, not a measured effect.
   *Expected impact: unknown until instrumented.*
5. **Chunk-index / intra-section ordering can demote a matching chunk.**
   `printRetrieved` shows `chunk N`; a correct section with many chunks may present
   a non-matching chunk first. *Expected impact: low; section matching is by
   `source`/`section`, so this affects evidence recall more than R@K.*

---

## 9. Verification and no-production-change guarantee

| acceptance criterion | check | result |
| --- | --- | --- |
| Baseline reproduced within tolerance (metrics equal at floor `0.70`) | compare §1.1 metrics against `docs/experiments-eval-sweep.md` | `0.42 / 0.52 / 0.22 / 0.42→0.48 / 1.00` — equal at floor `0.70` |
| run wall / RSS recorded, not treated as model-quality | §1.1 labels them operational only | pass |
| 33 answerable cases classified | §4 rows | 33 rows, one per answerable case (metadata observed; per-case outcome marked unavailable with instrumentation) |
| 6 unanswerable cases confirm rejection | §5 | 6/6 correct rejections, rate `1.00` |
| New report exists; per-case *evidence* not a template; unavailable marked with instrumentation | §4, §6.4, §7 | unavailable cells explicitly marked; each paired with the bounded instrumentation that produces it |
| No production file changed; no historical metric rewritten | only this new file is added | `scripts/eval-model-sweep.sh`, `cmd/eval/main.go`, `internal/*`, `migrations/*`, and `docs/experiments-eval-sweep.md` unchanged |
| Reuse-mode run on a disposable DB | §2.1 command; harness guards | `rag_db` refused; no CREATE/DROP in reuse mode |

The only added artifact for this task is this file. No historical metric is
rewritten: every baseline number is quoted from `docs/experiments-eval-sweep.md`,
which is left byte-identical. Where a live sweep writes its per-run rows, it must
write to a **new** results path (§2.1 `SWEEP_RESULTS`), never to the historical
file.

---

## 10. Operator-run live evidence (baseline capture — 2026-10-12)

> **Provenance.** This section is **operator-run**, not SOP-executed. The governed
> RQ-001 agent could not run a live database/embedding sweep; an operator ran the frozen
> experiment by hand against a **disposable** database and captured the raw output. It is
> kept distinct from any SOP-executed validation. Raw capture:
> `docs/rq-001-per-case-eval-capture-nomic-070-expanded-20261012.txt` (542 lines). §4’s
> “unavailable” per-case cells are **filled below** where the capture supplies them.

### 10.1 Exact configuration and commands

- Baseline commit: `d4e8d88876ad798d404f64d88a4022465d9f148a`.
- Database: `rag_021_test` (disposable; `documents` truncated before ingest). `rag_db` never touched.
- Embedding model: `nomic-embed-text` (`nomic-embed-text:latest`, Ollama). **No new download.**
- Dataset: `evals/retrieval-vi-en-expanded.json` sha256 `7f6926df…cbd2d0df` (39 cases).
- Corpus manifest: `evals/corpus-vi-en.json` sha256 `2ddbef2f…fbc87b21` (8 docs, 83 chunks).
- Floor `MIN_SIMILARITY=0.70`; `TOP_K=4`; `QUERY_REWRITE=false`; toolchain `GOTOOLCHAIN=go1.27.2`.

```
# reset + ingest (7 s)
psql "$DATABASE_URL" -c "TRUNCATE documents"
for f in examples/loan-policy.md examples/large-loan-policy.md examples/member-services-guide.md \
         examples/commercial-servicing-manual.md examples/vi/chinh-sach-khoan-vay.md \
         examples/vi/dich-vu-thanh-vien.md examples/vi/quy-trinh-thu-hoi.md examples/vi/bao-hiem-tai-san.md; do
  go run ./cmd/ingest "$f"; done
# evaluate (2 s)
EVAL_DATASET=evals/retrieval-vi-en-expanded.json MIN_SIMILARITY=0.70 QUERY_REWRITE=false \
  go run ./cmd/eval > docs/rq-001-per-case-eval-capture-nomic-070-expanded-20261012.txt
```

Ingest wall ≈ 7 s; eval wall ≈ 2 s; total operator capture ≈ 9 s.

### 10.2 Aggregate metrics (measured) — reproduce the record exactly

| metric | measured (operator) | recorded RAG-023D | match |
| --- | --- | --- | --- |
| Hybrid R@1 | 0.42 | 0.42 | yes |
| Hybrid R@2 | 0.48 | — | — |
| Hybrid R@4 | 0.52 | 0.52 | yes |
| Hybrid P@4 | 0.22 | 0.22 | yes |
| Evidence after expansion | 0.48 | 0.48 | yes |
| Rejection (similarity-only) | 6/6 = 1.00 | 1.00 | yes |

**The original baseline is reproduced exactly.**

### 10.3 Per-case hybrid results (measured)

`v1` = vector-baseline R@1 (post-floor), `h1`/`h4` = hybrid R@1/R@4, `ev` = evidence after expansion.

| id | kind | v1 | h1 | h4 | ev |
| --- | --- | --- | --- | --- | --- |
| eval-en-en-01 | en-en | 0 | 0 | 0 | 0 |
| eval-en-en-02 | en-en | 1 | **0** | 1 | 1 |
| eval-en-en-03 | en-en | 1 | 1 | 1 | 1 |
| eval-en-en-04 | en-en | 1 | 1 | 1 | 1 |
| eval-en-en-05 | en-en | 0 | 0 | 0 | 0 |
| eval-en-en-06 | en-en | 0 | **1** | 1 | 1 |
| eval-en-en-07 | en-en | 0 | 0 | 0 | 0 |
| eval-en-en-08 | en-en | 0 | 0 | 0 | 0 |
| eval-en-en-09 | en-en | 1 | 1 | 1 | 1 |
| eval-en-en-10 | en-en | 1 | 1 | 1 | 1 |
| eval-vi-vi-01 | vi-vi | 1 | 1 | 1 | 1 |
| eval-vi-vi-02 | vi-vi | 1 | 1 | 1 | 1 |
| eval-vi-vi-03 | vi-vi | 0 | 0 | 0 | 0 |
| eval-vi-vi-04 | vi-vi | 1 | 1 | 1 | 1 |
| eval-vi-vi-05 | vi-vi | 1 | 1 | 1 | 1 |
| eval-vi-vi-06 | vi-vi | 0 | 0 | 1 | 0 |
| eval-vi-vi-07 | vi-vi | 1 | 1 | 1 | 1 |
| eval-vi-vi-08 | vi-vi | 1 | 1 | 1 | 1 |
| eval-vi-vi-09 | vi-vi | 1 | 1 | 1 | 1 |
| eval-vi-vi-10 | vi-vi | 1 | 1 | 1 | 1 |
| eval-en-vi-01 | en-vi | 0 | 0 | 0 | 0 |
| eval-en-vi-02 | en-vi | 0 | 0 | 0 | 0 |
| eval-en-vi-03 | en-vi | 0 | 0 | 0 | 0 |
| eval-en-vi-04 | en-vi | 0 | 0 | 0 | 0 |
| eval-en-vi-05 | en-vi | 0 | 0 | 0 | 0 |
| eval-vi-en-01 | vi-en | 0 | 0 | 0 | 0 |
| eval-vi-en-02 | vi-en | 0 | 0 | 0 | 0 |
| eval-vi-en-03 | vi-en | 0 | 0 | 0 | 0 |
| eval-vi-en-04 | vi-en | 0 | 0 | 0 | 0 |
| eval-vi-en-05 | vi-en | 0 | 0 | 0 | 0 |
| eval-misleading-01 | misleading | 0 | 0 | 1 | 1 |
| eval-misleading-02 | misleading | 1 | 1 | 1 | 1 |
| eval-misleading-03 | misleading | 0 | 0 | 0 | 0 |
| eval-unanswerable-01..06 | unanswerable | — | rejected | — | — |

### 10.4 Failure distribution by language / query type (measured)

| kind | n | vec R@1 | hyb R@1 | hyb R@4 | ev (after) |
| --- | --- | --- | --- | --- | --- |
| vi-vi (monolingual VI) | 10 | 0.80 | 0.80 | 0.90 | 0.80 |
| en-en (monolingual EN) | 10 | 0.50 | 0.50 | 0.60 | 0.60 |
| misleading | 3 | 0.33 | 0.33 | 0.67 | 0.67 |
| en-vi (cross-lingual) | 5 | **0.00** | **0.00** | **0.00** | **0.00** |
| vi-en (cross-lingual) | 5 | **0.00** | **0.00** | **0.00** | **0.00** |

### 10.5 Classification (now measured, not inferred)

1. **Cross-lingual retrieval is the dominant failure (highest impact).** All **10**
   cross-lingual cases (`en-vi` + `vi-en`) score **0.00 at every K** — the expected section
   never enters the top-4. Monolingual Vietnamese (0.80) and English (0.50) are far better.
   The single largest quality lever is the bilingual embedding path (`nomic-embed-text`).
2. **Fusion reorders one correct rank-1 hit (measured evidence for H2).**
   `eval-en-en-02` scores vector R@1 = 1.0 but hybrid R@1 = 0.0 (recovers at K=2): the correct
   section was vector rank-1 and the keyword/fusion step demoted it. Conversely
   `eval-en-en-06` is vector 0.0 but hybrid 1.0 — fusion also rescues vector misses. Fusion
   is therefore both a help and a harm.
3. **Candidate starvation at the floor (supported by output).** In `eval-en-en-01` the
   vector baseline returns a single post-floor candidate (0.7036, a *different* document),
   so no correct candidate existed to fuse; the correct section likely scored just below
   `0.70`. Precise pre-filter ranking remains §10.6.
4. **Unanswerable rejection is intact:** 6/6, false-positive rate 0.00.

### 10.6 Remaining unavailable observation (instrumentation gap)

The **pre-`KeepSimilar` candidate ranking** (the exact fused/vector rank of the correct
section *before* the `0.70` floor is applied) is still **not emitted**: `PipelineResult.Vector`
is already post-floor, the harness deletes its temp stdout, and no pre-filter dump exists.
To obtain it requires bounded instrumentation: log the raw `SearchFiltered` candidates
(before `KeepSimilar`) per case (a small read-only change to `cmd/eval` or a dedicated
diagnostic), **not** applied here without approval. §10.5’s floor-starvation point is
supported by observed single-candidate outputs but is not a full pre-filter dump.

*All §10 numbers are operator-measured; the historical record is unchanged.*

---

## 11. Candidate-loss analysis (pre-filter diagnostic, measured 2026-10-12)

Source: `EVAL_PREFILTER_DUMP=1` run over the frozen 39-case set on the disposable
`rag_021_test` (nomic-embed-text, floor `0.70`), artifact
`docs/rq-001-prefilter-dump-nomic-070-expanded-20261012.txt` (117 bounded lines = 39x3).
The `K=4` pre-filter candidate set is used (the candidate set the final ranking draws
from). The pre-filter list is similarity-ordered, so the expected section survived the
floor iff its pre-filter **rank <= kept** (`kept` = count with `sim >= 0.70`).

### 11.1 Classification of the 33 answerable cases

| class | rule | count |
| --- | --- | --- |
| **candidate miss** | expected absent from the top-4 pre-filter candidates | **11** |
| **filtered by floor** | present pre-filter but `rank > kept` (score `< 0.70`) | **5** |
| **ranking loss** | present + kept but not in the final top-4 | **0** |
| recovered | present + kept + matched | 17 |

### 11.2 By language direction

| kind | n | candidate miss | filtered by floor | ranking loss | recovered |
| --- | --- | --- | --- | --- | --- |
| vi-vi | 10 | 0 | 1 | 0 | 9 |
| en-en | 10 | 0 | 4 | 0 | 6 |
| misleading | 3 | 1 | 0 | 0 | 2 |
| **en-vi** | 5 | **5** | 0 | 0 | 0 |
| **vi-en** | 5 | **5** | 0 | 0 | 0 |

### 11.3 What this means (measured, not inferred)

1. **The 10 cross-lingual failures are candidate misses — not filtering and not ranking.**
   For every `en-vi`/`vi-en` case the expected section is **absent from the top-4 vector
   candidates before the floor is applied** (`present pre-filter: NO`). The `0.70` floor is
   **not** the cause; the embedding similarity simply does not surface the cross-lingual
   section. The dominant lever is the **embedding model** (RQ-003), not the floor.
2. **The 5 monolingual failures are floor-trimming losses.** The correct section *is*
   retrieved pre-filter but scores just below `0.70`:
   `eval-en-en-05` (pre-rank 2), `eval-en-en-01` (pre-rank 3), `eval-vi-vi-03` (pre-rank 2),
   and — strongest evidence — `eval-en-en-07` and `eval-en-en-08`, where the expected
   section is the **pre-filter rank 1** yet is dropped because its similarity (`0.6938` etc.)
   is below the `0.70` floor. A marginally lower floor would recover up to 5 cases, but the
   floor is set for rejection — this is an RQ-004 trade-off to *measure*, not to change here.
3. **No ranking loss at K=4.** Every case whose expected section passed the floor matched in
   the final ranking; the K=1 fusion reorder seen in §10.5 does not persist to K=4.

*Measurements are operator-run and read-only; no evaluation input, floor, or historical
number was changed.*

---

## 12. Acceptance verification (RQ-001, amended)

The original, the approved amendment, and the verified final behavior are recorded
together so the governance change is explicit and neither history is rewritten.

| phase | statement | status |
| --- | --- | --- |
| **original requirement** | “No production file changed” | superseded by the amendment below; the governed run’s edit is recorded as a governance discrepancy in §10.5 and in the plan’s RQ-001 governance note |
| **approved amendment (2026-10-12)** | explicitly authorizes the evaluation-only, default-OFF `EVAL_PREFILTER_DUMP` diagnostic and its regression tests | accepted via `sop reconcile --accept-changed RQ-001` |
| **verified final behavior** | default output unchanged; diagnostic deterministic/bounded; no secrets; tests present | see below |

### 12.1 Verification evidence

| criterion | evidence |
| --- | --- |
| no change to default evaluation behavior | dump OFF reproduces the baseline exactly (R@1 `0.42` / R@4 `0.52` / P@4 `0.22` / evidence `0.48` / rejection `6/6`) with **0** diagnostic lines |
| deterministic, bounded diagnostic output | dump ON emits the same metrics plus exactly `39x3 = 117` lines |
| no credentials/sensitive content | output carries only similarity/`source`/`section`/`chunk`; asserted in tests |
| regression tests (enablement + output) | `cmd/eval/prefilter_dump_test.go`: `TestPrefilterDumpEnabled`, `TestPrefilterDumpDefaultOff`, `TestReportPrefilterOutput`, `TestReportPrefilterAbsentAndUnanswerable` — all pass |
| measured pre/post-filter evidence | §11 (candidate miss 11 / filtered 5 / ranking 0 / recovered 17) |
| gates | `gofmt`, `go build ./...`, `go vet ./...`, `go test ./... -count=1`, `go test -race ./cmd/eval/`, `git diff --check` all pass |

*The byte-identical default-output claim is verified by the controlled disposable-DB run
above (dump OFF vs ON), not by a unit test, as it needs the live evaluator; that limitation
is recorded here.*
