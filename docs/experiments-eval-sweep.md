# Embedding-model sweep results

Recorded by `scripts/eval-model-sweep.sh`.

- Held-out evaluation split: `evals/retrieval-vi-en-expanded.json`
  (sha256 `72d2c15b3b91966c9471357e3abbec75f71b5859b5f36120ced24949f0539451`).
- Calibration split: `evals/retrieval-vi-en-calibration.json`
  (sha256 `a6e4bddd6a998cdbbd2034f4bb1fa7b0f9759883a9db2e09b055cfb2f01d24a0`).
- Corpus manifest: `evals/corpus-vi-en.json`
  (sha256 `7833e235b90470cd49eb7428f0d08c64145690347d02d53ba39d9a962ef7789b`).

Selection rule (fixed before any final comparison): among floors whose
calibration rejection is at least the minimum (`SWEEP_MIN_REJECTION`, default
`1`), choose the highest recall@4, then the highest rejection, then the highest
floor; if none qualifies, choose the highest rejection, then the highest
recall@4, then the highest floor. A floor with zero rejection can never win on
recall alone.

Measurement labels: `run wall (s)` is the wall-clock time of the whole
`go run ./cmd/eval` subprocess (client-observed; includes the Go build, the eval
client, and database work), not embedding-only latency. `client RSS (KiB)` is the
peak resident set size of that eval client process, not the embedding model
server's memory. `embed endpoint (ms)` is an opt-in (`SWEEP_EMBED_PROBE=1`)
client-observed measurement of the Ollama `/api/embed` endpoint, `n/a` when the
probe is off; server-side memory is not measured by this harness.

Columns: model, version, dimension, floor, corpus manifest hash, repeat index,
recall@1, recall@4, precision@4, evidence recall (before -> after expansion),
rejection rate, embedding endpoint latency, eval-run wall clock, client peak RSS.

| model | version | dim | floor | manifest hash | repeat | R@1 | R@4 | P@4 | evidence | rejection | embed endpoint (ms) | run wall (s) | client RSS (KiB) |
| ----- | ------- | --- | ----- | ------------- | ------ | --- | --- | --- | -------- | --------- | ------------------ | ------------ | ---------------- |

| nomic-embed-text | unversioned | 768 | 0.60 | 7833e235b90470cd49eb7428f0d08c64145690347d02d53ba39d9a962ef7789b | 1 | 0.48 | 0.67 | 0.28 | 0.58->0.64 | 0.67 | n/a | 1.09 | 33712 |
| nomic-embed-text | unversioned | 768 | 0.60 | 7833e235b90470cd49eb7428f0d08c64145690347d02d53ba39d9a962ef7789b | 2 | 0.48 | 0.67 | 0.28 | 0.58->0.64 | 0.67 | n/a | 1.12 | 34320 |
| nomic-embed-text | unversioned | 768 | 0.60 | 7833e235b90470cd49eb7428f0d08c64145690347d02d53ba39d9a962ef7789b | 3 | 0.48 | 0.67 | 0.28 | 0.58->0.64 | 0.67 | n/a | 1.08 | 33568 |
| embeddinggemma | unversioned | 768 | 0.30 | 7833e235b90470cd49eb7428f0d08c64145690347d02d53ba39d9a962ef7789b | 1 | 0.67 | 0.94 | 0.28 | 0.79->0.85 | 0.00 | n/a | 1.37 | 33472 |
| embeddinggemma | unversioned | 768 | 0.30 | 7833e235b90470cd49eb7428f0d08c64145690347d02d53ba39d9a962ef7789b | 2 | 0.67 | 0.94 | 0.28 | 0.79->0.85 | 0.00 | n/a | 1.41 | 32572 |
| embeddinggemma | unversioned | 768 | 0.30 | 7833e235b90470cd49eb7428f0d08c64145690347d02d53ba39d9a962ef7789b | 3 | 0.67 | 0.94 | 0.28 | 0.79->0.85 | 0.00 | n/a | 1.42 | 33260 |

> The six rows above were recorded with the earlier **recall-only** floor rule and
> are retained as historical observations. Under the current joint rule a floor
> with a zero rejection rate is ineligible while any floor with a positive
> rejection rate exists, so the `embeddinggemma` rows at floor `0.30`
> (rejection `0.00`) are **not** a valid operating point under the current
> criterion. Re-run `scripts/eval-model-sweep.sh` to record a selection made under
> the joint rule; the version column also defaults to `unversioned` unless a
> `model:dim:version` spec or `SWEEP_VERSION` is supplied.
