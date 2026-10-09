# Comparing embedding models on the evaluation harness

This is an operator-run procedure. Nothing here is applied automatically by
`make db-up`, `make db-schema`, or the `docker-entrypoint-initdb.d` mount in
`docker-compose.yml`; it is deliberately outside the `migrations/*.sql` glob.

## Purpose

Evaluate a candidate embedding model against the current default by scoring the
expanded bilingual dataset with `cmd/eval`, one model per isolated database. The
dataset includes English→English, Vietnamese→Vietnamese, English→Vietnamese,
and Vietnamese→English retrieval cases plus unanswerable and misleading-context
cases, so a model that only handles English is visible in the numbers.

This procedure does **not** change any production default: the embedding model,
`EMBED_DIM`, the default dataset, retrieval, embedding, and ingestion code are
untouched. Comparing a candidate is an evaluation only; promoting a new model is
a separate, operator-run change (see `docs/operations/embedding-dimension.md`
when the candidate's dimension differs).

## Dataset and corpus manifest

`cmd/eval` reads its cases from the file named by `EVAL_DATASET`:

- `EVAL_DATASET` unset or empty loads `evals/retrieval.json`, exactly the
  default run.
- `EVAL_DATASET=<path>` loads that file. A missing, unreadable, or malformed
  file fails fast with an error naming the path.

The expanded bilingual dataset — the held-out split the sweep reports — lives
at `evals/retrieval-vi-en-expanded.json`, with its corpus manifest at
`evals/corpus-vi-en.json`. The manifest maps every dataset source to a
repository-relative `path` and a deterministic `sha256` content hash, and each
run record reports the manifest hash so comparisons are only made against the
same corpus. The smaller historical dataset is `evals/retrieval-vi-en.json`.

`cmd/eval/main_test.go` validates both committed datasets against the manifest:
every expected source must be listed, every referenced section must exist in
that file, every evidence string must be a verbatim substring of the referenced
section, and each dataset must meet its required case counts and language
directions. It also asserts the two datasets share no case id and no question,
so a floor tuned on one split is never scored on the same case it was tuned on.

Two disjoint splits are committed:

- `evals/retrieval-vi-en-calibration.json` — the **calibration split**, used only
  to select the operating `MIN_SIMILARITY` (its cases are labelled, but its
  metrics are not the reported ones).
- `evals/retrieval-vi-en-expanded.json` — the **held-out evaluation split**, whose
  metrics are the ones reported. Threshold selection never sees it.

Both reference the same corpus manifest, `evals/corpus-vi-en.json`.

## Repeatable per-model sweep

`scripts/eval-model-sweep.sh` runs the whole comparison for one or more models:
it calibrates `MIN_SIMILARITY` per model over a sweep grid on the **calibration
split**, runs at least three repeats per model on the **held-out evaluation
split** against a **separate isolated database per model**, and records
recall@1, recall@4, precision, evidence recall, rejection rate, the eval-run wall
clock, and peak client RSS, plus the variance across repeats.

Threshold selection uses a documented joint recall/rejection rule, fixed before
the comparison runs:

- an **eligible** floor is one whose rejection rate on the calibration split is
  at least `SWEEP_MIN_REJECTION` (default `1`, i.e. it rejects every calibration
  unanswerable case);
- among eligible floors, the script picks the highest recall@4, then the highest
  rejection rate, then the highest floor;
- if no floor is eligible, it picks the highest rejection rate first, then the
  highest recall@4, then the highest floor, and reports the operating point as
  below the requirement.

A floor with zero rejection can therefore never win on recall alone: the
eligible set excludes it, and the fallback orders by rejection before recall.

```bash
# Smoke run against one model (three repeats).
scripts/eval-model-sweep.sh 'nomic-embed-text:768' 3

# Full comparison of the incumbent and a candidate.
scripts/eval-model-sweep.sh 'nomic-embed-text:768,embeddinggemma:768' 3
```

Useful environment variables: `SWEEP_DATASET` (held-out evaluation split),
`SWEEP_CALIBRATION_DATASET` (calibration split), `SWEEP_MANIFEST`, `SWEEP_GRID`
(default `0.30,0.40,0.50,0.60,0.70`), `SWEEP_MIN_REJECTION` (default `1`),
`SWEEP_DB_URL` (model N uses database `rag_<N>`, so each model is isolated),
`SWEEP_RESULTS` (default `docs/experiments-eval-sweep.md`),
`SWEEP_SKIP_INGEST=1` to reuse loaded databases, and `SWEEP_EMBED_PROBE=1` for the
opt-in embedding endpoint probe. Recorded results live in
`docs/experiments-eval-sweep.md`.

### What the resource numbers mean

The sweep records two process-level numbers per run, and they are deliberately
labelled to say what they are:

- **`run wall (s)`** is the wall-clock time of the whole `go run ./cmd/eval`
  subprocess on the isolated database. It is **client-observed** and includes the
  Go build, the eval client, and database work — it is **not** embedding-only
  latency.
- **`client RSS (KiB)`** is the peak resident set size of that eval client
  process. It is **not** the embedding model server's memory.

The `SWEEP_EMBED_PROBE=1` probe adds a separate, opt-in measurement of the Ollama
`/api/embed` endpoint for one fixed representative chunk (client-observed
milliseconds). Server-side memory is **not** measured by this harness: that would
require inspecting the Ollama server process and is out of scope for an
evaluation-only sweep. Each run record carries the model, version, dimension,
floor, corpus manifest hash, repeat index, and the dataset hashes are recorded in
the results header, so the configuration a run used is auditable.

## Prerequisites

- A local Ollama server with the embedding and chat models to compare
  (`OLLAMA_EMBED_MODEL`, `OLLAMA_CHAT_MODEL`).
- Two **isolated** PostgreSQL + pgvector databases, one per embedding model.
  Never mix embeddings from different models in one vector index: a vector
  written by one model is meaningless to another and the retrieval scores would
  be silently wrong. Give each model its own database (or its own
  `documents` table / volume) and its own `DATABASE_URL`. The sweep script does
  this automatically by deriving a `rag_<N>` database per model on the one
  shared compose instance and pointing every command's `DATABASE_URL` at it.
- The corpora named by the corpus manifest, ingested into **each** database with
  that database's model.

## Manual procedure (equivalent to one sweep step)

1. Record the baseline. The current default embedding model is
   `nomic-embed-text` (`EMBED_DIM=768`) with `EMBED_PROVIDER=ollama`; this is the
   nomic baseline the candidate is measured against. Do not change these
   defaults.

2. Start the first isolated database and load the default model's environment:

   ```bash
   export DATABASE_URL='postgres://rag:rag@127.0.0.1:5434/rag?sslmode=disable'
   export OLLAMA_EMBED_MODEL=nomic-embed-text
   export EMBED_DIM=768
   make db-up
   ```

3. Ingest the corpora into that database with that model. Ingest each file the
   corpus manifest (`evals/corpus-vi-en.json`) names; today that is
   `loan-policy.md`, `large-loan-policy.md`, `member-services-guide.md`,
   `commercial-servicing-manual.md`, `chinh-sach-khoan-vay.md`,
   `dich-vu-thanh-vien.md`, `quy-trinh-thu-hoi.md`, and `bao-hiem-tai-san.md`:

   ```bash
   go run ./cmd/ingest examples/loan-policy.md
   go run ./cmd/ingest examples/large-loan-policy.md
   go run ./cmd/ingest examples/member-services-guide.md
   go run ./cmd/ingest examples/commercial-servicing-manual.md
   go run ./cmd/ingest examples/vi/chinh-sach-khoan-vay.md
   go run ./cmd/ingest examples/vi/dich-vu-thanh-vien.md
   go run ./cmd/ingest examples/vi/quy-trinh-thu-hoi.md
   go run ./cmd/ingest examples/vi/bao-hiem-tai-san.md
   ```

4. Score the calibration split to choose the operating floor, then score the
   held-out evaluation split for the reported metrics. Never pick the floor on
   the split you report:

   ```bash
   EVAL_DATASET=evals/retrieval-vi-en-calibration.json go run ./cmd/eval
   EVAL_DATASET=evals/retrieval-vi-en-expanded.json go run ./cmd/eval
   ```

5. Repeat steps 2–4 for the candidate model, using a **separate isolated
   database** (a different `DATABASE_URL`, e.g. a `rag_ml` database, or a
   different Compose volume) and the candidate's
   `OLLAMA_EMBED_MODEL`/`EMBED_DIM`. Never ingest both models' chunks into the
   same vector index.

6. Compare the two runs' reported sections:

   - `Vector retrieval` — recall@k and precision@k for plain vector search.
   - `Hybrid retrieval` — recall@k and precision@k for vector + keyword fused
     with RRF, plus the `Correct rejection`/`FALSE POSITIVE` line for each
     unanswerable case.
   - `Avg Evidence Recall` — before and after section expansion, for the cases
     that opt in with an `evidence` array.
   - `Citation Validity` — with `EVAL_FACT_JUDGE=true` and generated answers.
   - `Rerank latency` — off / lexical / LLM wall time when a reranker is
     enabled.

The four bilingual retrieval directions are covered by the dataset cases, and
the unanswerable cases (`expected: []`) exercise the rejection path. To include
the answer-level metrics, set `EVAL_ANSWERABILITY_GATE=true` and/or
`EVAL_FACT_JUDGE=true` as in the main evaluation workflow. The sweep script
leaves these off so its run wall clock stays comparable across models; note that
this wall clock is the whole eval subprocess (Go build, client, and database
work), not embedding-only latency.

7. When both runs are recorded, keep the default model in production unless the
   candidate is deliberately promoted; promotion requires re-ingesting with the
   candidate and, if its dimension differs, following
   `docs/operations/embedding-dimension.md`.
