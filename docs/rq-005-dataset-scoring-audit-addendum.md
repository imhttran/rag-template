# RQ-005 — Dataset & Scoring Audit Addendum (commercial-servicing heading gap)

**Type:** Read-only audit addendum. Supplementary to
`docs/rq-005-dataset-scoring-audit.md` (which is preserved unchanged).
**Scope:** closes the Section 4/5 gap of the original audit — the heading/evidence
verification of `examples/commercial-servicing-manual.md`.
**No dataset, corpus, or scoring file was modified.**

## 1. Method

Read-only: the stored document `examples/commercial-servicing-manual.md` was read and its
`##` headings enumerated; every dataset expectation whose `source` is that document was
cross-referenced (calibration, expanded/held-out, and `retrieval.json`).

## 2. Stored headings (verbatim)

`examples/commercial-servicing-manual.md` (61 lines, ~1,530 words) declares exactly six
sections:

1. `Escrow Administration`
2. `Insurance Requirements`
3. `Property Taxes`
4. `Construction Draw Administration`
5. `Collateral and Lien Documentation`
6. `Servicing Transfer Procedures`

## 3. Expected sections referencing this document

All expected `section` values referencing `commercial-servicing-manual.md` resolve to one of
the six stored headings above — **no mismatch, no missing heading**:

| Dataset | Case id | Expected section | Stored heading present |
|---|---|---|---|
| calibration | `cal-en-en-01` | Escrow Administration | yes |
| calibration | `cal-en-en-02` | Construction Draw Administration | yes |
| calibration | `cal-vi-en-03` | Escrow Administration | yes |
| expanded | `eval-en-en-09` | Insurance Requirements | yes |
| expanded | `eval-en-en-10` | Property Taxes | yes |
| expanded | `eval-vi-en-03` | Servicing Transfer Procedures | yes |
| expanded | `eval-vi-en-05` | Collateral and Lien Documentation | yes |
| retrieval | "How is an escrow shortage resolved?" | Escrow Administration | yes |
| retrieval | "When may the servicer force-place insurance…" | Insurance Requirements | yes |
| retrieval | "What happens if a property tax becomes delinquent…" | Property Taxes | yes |
| retrieval | "When is retainage released…" | Construction Draw Administration | yes |
| retrieval | "When must a UCC financing statement be continued…" | Collateral and Lien Documentation | yes |
| retrieval | "What notice does a borrower receive when servicing…" | Servicing Transfer Procedures | yes |

## 4. Evidence substring check

Every `evidence` string for those expectations is present in the stored document
(case-insensitive substring match) — **14/14 present**. Examples verified: the escrow
cushion/surplus, retainage release, force-placed insurance, property-tax advance, financing-
statement continuation, and servicing-transfer notice texts all resolve to the document.

## 5. Conclusion

The heading-verification gap flagged as "unverified" in the original audit is **closed**:
the document's section headings and evidence strings fully support the dataset expectations.
**No dataset/scoring/corpus artifact is present**, and **no dataset edit is proposed**.

The broader audit limitations of the original report (8-document corpus, anecdotal
cross-lingual power, bounded negative sampling) remain in force and are not changed here.

## 6. Attestation

**Read-only attestation:** this addendum read `examples/commercial-servicing-manual.md` and
`evals/*.json` only. No dataset, corpus, or scoring file was written. The only artifact
created is this addendum; `docs/rq-005-dataset-scoring-audit.md` is unchanged.

**Separation statement:** audit findings only; kept distinct from any improvement
measurement; nothing here relaxes the `0.70` acceptance rule.
