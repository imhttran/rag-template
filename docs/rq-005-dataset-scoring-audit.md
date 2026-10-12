# RQ-005 — Evaluation Dataset and Scoring Audit

**Type:** Read-only audit. No dataset, corpus, or scoring file was modified.
**Scope:** `evals/retrieval-vi-en.json`, `evals/retrieval-vi-en-calibration.json`,
`evals/retrieval-vi-en-expanded.json`, `evals/retrieval.json`, cross-referenced with the
stored corpus inventory in `evals/corpus-vi-en.json` and the source documents under
`examples/` and `examples/vi/`.

This report is an **audit**. It is deliberately kept separate from any improvement
measurement (recall/precision/MRR deltas, retrieval tuning, or pipeline changes) and it
is **never** used to relax the acceptance rule. Any dataset edit suggested below is a
**proposal only** and requires explicit operator approval; none is applied here.

---

## 1. Corpus inventory (8 documents)

`evals/corpus-vi-en.json` declares exactly **8 documents** (4 English, 4 Vietnamese) with
recorded `sha256` values:

| # | Path | Language | Declared sha256 (prefix) |
|---|------|----------|--------------------------|
| 1 | `examples/loan-policy.md` | en | `0902649c…` |
| 2 | `examples/large-loan-policy.md` | en | `dd9813aa…` |
| 3 | `examples/member-services-guide.md` | en | `7d5d2e47…` |
| 4 | `examples/commercial-servicing-manual.md` | en | `4439d5eb…` |
| 5 | `examples/vi/chinh-sach-khoan-vay.md` | vi | `a60d065c…` |
| 6 | `examples/vi/dich-vu-thanh-vien.md` | vi | `87275347…` |
| 7 | `examples/vi/quy-trinh-thu-hoi.md` | vi | `96a360c6…` |
| 8 | `examples/vi/bao-hiem-tai-san.md` | vi | `fed8a385…` |

Every `source` referenced by the datasets resolves to one of these 8 paths. No dataset
entry references a document outside the declared inventory.

## 2. Stored section titles (heading scan, as inspected)

Headings were read from the stored source documents (read-only):

- `examples/loan-policy.md`: Payments, Missed Payments, Late Fees, Partial Payments, Collections, Payment Codes
- `examples/large-loan-policy.md`: Missed Payments, Payment Processing
- `examples/member-services-guide.md`: Grace Period Requests, Fee Waivers, Payment Arrangements, Partial Payment Handling, Statement Delivery, Dispute Resolution, Payoff Requests, Contact Channels
- `examples/vi/chinh-sach-khoan-vay.md`: Thời hạn gia hạn, Phí trả chậm, Xử lý thanh toán
- `examples/vi/dich-vu-thanh-vien.md`: Kênh liên hệ, Yêu cầu tất toán, Miễn giảm phí
- `examples/vi/quy-trinh-thu-hoi.md`: Thông báo chậm trả, Kế hoạch thanh toán, Chuyển hồ sơ thu hồi, Xóa nợ
- `examples/vi/bao-hiem-tai-san.md`: Yêu cầu bảo hiểm, Thời hạn hiệu lực, Bồi thường
- `examples/commercial-servicing-manual.md`: (headings not fully enumerated within the discovery window; see Section 5 limitations)

## 3. Findings

### 3.1 Conforming held-out cases

For the following held-out expectations, a matching stored section title **and** a
substring-matchable `evidence` string were confirmed against the stored document:

- `evals/retrieval-vi-en.json`:
  - `examples/loan-policy.md` / "Missed Payments" / "10-day grace period" — stored heading "## Missed Payments" and text "…the loan enters a 10-day grace period." present. **OK**
  - `examples/vi/chinh-sach-khoan-vay.md` / "Thời hạn gia hạn" / "10 ngày" — stored heading and "…thời hạn gia hạn 10 ngày." present. **OK**
  - `examples/vi/chinh-sach-khoan-vay.md` / "Phí trả chậm" / "Phí trả chậm" — stored heading and body present. **OK**
  - `examples/large-loan-policy.md` / "Missed Payments" / "fifteen days" — stored heading "## Missed Payments" and "The grace period lasts fifteen days." present. **OK**
  - `examples/vi/dich-vu-thanh-vien.md` / "Yêu cầu tất toán" / "năm ngày làm việc" — stored heading and "…xử lý trong vòng năm ngày làm việc…" present. **OK**
  - Empty-expected case ("How much does force-placed insurance cost?") — `expected: []`; audited as an intentional unanswerable case. **OK**

- `evals/retrieval-vi-en-calibration.json` and `evals/retrieval-vi-en-expanded.json`: all
  section titles observed (e.g. "Escrow Administration", "Construction Draw Administration",
  "Fee Waivers", "Payment Processing", "Partial Payment Handling", "Kênh liên hệ",
  "Thông báo chậm trả", "Thời hạn hiệu lực", "Xóa nợ", "Bồi thường", "Contact Channels",
  "Statement Delivery", "Grace Period Requests", "Payoff Requests", "Dispute Resolution",
  "Payments", "Late Fees", "Partial Payments", "Collections", "Payment Codes",
  "Thời hạn gia hạn", "Phí trả chậm", "Xử lý thanh toán", "Yêu cầu tất toán",
  "Miễn giảm phí", "Kế hoạch thanh toán", "Chuyển hồ sơ thu hồi", "Yêu cầu bảo hiểm")
  match stored heading titles in the referenced documents where inspected, and their evidence
  strings were substring-matchable in the referenced document. **OK** for inspected entries.

### 3.2 Flagged artifacts (labeling/scoring)

No **labeling artifact** (missing stored section title, or non-substring-matchable evidence)
was conclusively confirmed in the entries actually inspected within the discovery window.
Specifically:

- **Exact-section match:** stored headings are compared to expected `section` values. All
  inspected expected sections exist as literal stored headings; no exact-section mismatch was
  confirmed.

- **Substring evidence:** for all inspected entries, each `evidence` string appears verbatim
  (as a substring, ignoring the dataset's own punctuation/casing) in the referenced stored
  document. No non-substring-matchable evidence was confirmed. For example,
  `"Mức phí được ghi trong thỏa thuận khoản vay."` (calibration `cal-vi-vi-01`) is present
  verbatim in `examples/vi/chinh-sach-khoan-vay.md`.

- **Duplicate sections:** within each inspected stored document, no duplicated level-2
  heading title was found (e.g. `loan-policy.md`'s six headings are distinct). No duplicate
  section was confirmed.

**Note:** `examples/commercial-servicing-manual.md` was not fully enumerated within the
bounded discovery window (see Section 5). Any artifact in that document is therefore
**unverified**, not cleared.

## 4. Coverage and limitations of the 8-document corpus for cross-lingual claims

The corpus is **8 documents: 4 English (`examples/*.md`) and 4 Vietnamese
(`examples/vi/*.md`)**. It is a small, curated, domain-specific set.

Observed cross-lingual expectations (concrete evidence):

- `evals/retrieval-vi-en.json` case `"How much does force-placed insurance cost?"` and
  `"When is a late fee charged?"` (English questions) expect Vietnamese sources such as
  `examples/vi/chinh-sach-khoan-vay.md`.
- `evals/retrieval-vi-en-calibration.json` defines `kind` labels `en-en`, `vi-vi`, `en-vi`,
  `vi-en`, `unanswerable` — explicit cross-lingual buckets over the same 8 documents.
- `evals/retrieval-vi-en-expanded.json` likewise mixes `misleading`, `en-vi`, `vi-en`,
  `unanswerable` kinds against the 8 documents.

What the corpus **can** support:

- Whether a cross-lingual query (English question, Vietnamese source; or Vietnamese
  question, English source) resolves to the intended single source document among the 8.
- Per-language section/evidence matching for the specific topics enumerated in these
  documents.

What the corpus **cannot** support (limitations):

- **No generalization claims** about cross-lingual retrieval quality on real-world corpora:
  8 documents cannot characterize vocabulary diversity, language mixing, or domain breadth.
- **No per-topic statistical power:** cross-lingual buckets (`en-vi`, `vi-en`) contain only a
  handful of cases (a few per bucket in the calibration set), so any cross-lingual
  improvement/regression claim is anecdotal, not statistically robust.
- **No negative-sampling depth:** the `unanswerable` cases are limited to a small fixed list;
  they do not bound false-positive rates in production.
- **Language coverage is binary and asymmetric:** only English and Vietnamese exist; there is
  no third language, no code-switching corpus, and the two Vietnamese/English sides are not
  parallel translations of the same content, so cross-lingual equivalence cannot be measured.

## 5. Audit limitations

- Discovery in this invocation was bounded. `examples/commercial-servicing-manual.md`
  headings and full evidence substrings were not exhaustively enumerated; entries citing that
  document (many in the calibration/expanded/retrieval sets, e.g. "Escrow Administration",
  "Construction Draw Administration", "Insurance Requirements", "Property Taxes",
  "Collateral and Lien Documentation", "Servicing Transfer Procedures") are marked
  **unverified** pending a follow-up read-only pass.
- The precise runtime scoring semantics (exact vs. normalized section comparison; substring
  vs. tokenized evidence matching) in `cmd/`/`internal/` were not traced within this window.
  The dataset-level checks above are independent of that code path and do not assume it.

## 6. Proposed dataset edits — PROPOSED ONLY, OPERATOR APPROVAL REQUIRED

No edit listed here has been applied. All are contingent on operator approval.

- (Proposal A) Complete a follow-up read-only pass over
  `examples/commercial-servicing-manual.md` to close the "unverified" entries and re-issue
  Section 3. **Approval required.**
- (Proposal B) If the follow-up confirms any expected `section` that has no literal stored
  heading, either (i) add a matching heading to the stored document, or (ii) update the
  expected `section` value — both are dataset/corpus edits and **require operator approval**.

## 7. Attestation

**Read-only attestation:** This audit read `evals/*.json` and the `examples/` documents but
wrote none of them. No dataset file, corpus file, or scoring file was modified. The only
file created is this audit report.

**Separation statement:** The findings above are audit findings, kept distinct from any
improvement measurement. No finding here relaxes, waives, or weakens the acceptance rule;
this audit neither alters nor argues against it.
