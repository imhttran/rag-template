# Comparing embedding models on the evaluation harness

This is an operator-run procedure. Nothing here is applied automatically by
`make db-up`, `make db-schema`, or the `docker-entrypoint-initdb.d` mount in
`docker-compose.yml`; it is deliberately outside the `migrations/*.sql` glob.

## Purpose

Evaluate a candidate embedding model against the current default by scoring
the bilingual dataset with `cmd/eval`, one model per isolated database. The
dataset includes English→English, Vietnamese→Vietnamese, English→Vietnamese,
and Vietnamese→English retrieval cases plus one unanswerable case, so a model
that only handles English is visible in the numbers.

RAG-018 adds dataset selection to `cmd/eval` and this procedure. It does **not**
change any production default: the embedding model, `EMBED_DIM`, the default
dataset, retrieval, embedding, and ingestion code are untouched. Comparing a
candidate is an evaluation only; promoting a new model is a separate,
operator-run change (see `docs/operations/embedding-dimension.md` when the
candidate's dimension differs).

## Dataset selection

`cmd/eval` reads its cases from the file named by `EVAL_DATASET`:

- `EVAL_DATASET` unset or empty loads `evals/retrieval.json`, exactly the
default run.
- `EVAL_DATASET=<path>` loads that file. A missing, unreadable, or
malformed file fails fast with an error naming the path.

The bilingual dataset lives at `evals/retrieval-vi-en.json`.

## Prerequisites

- A local Ollama server with the embedding and chat models to compare
  (`OLLAMA_EMBED_MODEL`, `OLLAMA_CHAT_MODEL`).
- Two **isolated** PostgreSQL + pgvector databases, one per embedding model.
  Never mix embeddings from different models in one vector index: a vector
  written by one model is meaningless to another and the retrieval scores would
  be silently wrong. Give each model its own database (or its own
  `documents` table / volume) and its own `DATABASE_URL`.
- The corpora the bilingual dataset references, ingested into **each** database
  with that database's model.

## Procedure

1. Record the baseline. The current default embedding model is
   `nomic-embed-text` (`EMBED_DIM=768`) with `EMBED_PROVIDER=ollama`; this is the
   nomic baseline the candidate is measured against. Do not change these
   defaults.

2. Start the first isolated database and load the default model's environment:

   ```bash
   export DATABASE_URL='postgres://rag:rag@127.0.0.1:5433/rag?sslmode=disable'
   export OLLAMA_EMBED_MODEL=nomic-embed-text
   export EMBED_DIM=768
   make db-up
   ```

3. Ingest the corpora into that database with that model. Ingest each file the
   bilingual dataset references (`loan-policy.md`, `chinh-sach-khoan-vay.md`,
   `large-loan-policy.md`, `dich-vu-thanh-vien.md`, plus the distractor corpora):

   ```bash
   go run ./cmd/ingest examples/loan-policy.md
   go run ./cmd/ingest examples/large-loan-policy.md
   go run ./cmd/ingest examples/member-services-guide.md
   go run ./cmd/ingest examples/commercial-servicing-manual.md
   go run ./cmd/ingest examples/vi/chinh-sach-khoan-vay.md
   go run ./cmd/ingest examples/vi/dich-vu-thanh-vien.md
   ```

4. Score the bilingual dataset against the baseline model:

   ```bash
   EVAL_DATASET=evals/retrieval-vi-en.json go run ./cmd/eval
   ```

5. Repeat steps 2–4 for the candidate model, using a **separate isolated
   database** (a different `DATABASE_URL`, e.g. a `rag_ml` database, or a
   different Compose volume) and the candidate's
   `OLLAMA_EMBED_MODEL`/`EMBED_DIM`. Never ingest both models' chunks into the
   same vector index.

6. Compare the two runs' reported sections:

   - `Vector retrieval` — recall@k and precision@k for plain vector search.
   - `Hybrid retrieval` — recall@k and precision@k for vector + keyword fused
     with RRF, plus the `Correct rejection`/`FALSE POSITIVE` line for the
     unanswerable case.
   - `Avg Evidence Recall` — before and after section expansion, for the cases
     that opt in with an `evidence` array.
   - `Citation Validity` — with `EVAL_FACT_JUDGE=true` and generated answers.
   - `Rerank latency` — off / lexical / LLM wall time when a reranker is
     enabled.

The four bilingual retrieval directions are covered by the dataset cases, and
the sixth case (`expected: []`) exercises the unanswerable path. To include the
answer-level metrics, set `EVAL_ANSWERABILITY_GATE=true` and/or
`EVAL_FACT_JUDGE=true` as in the main evaluation workflow.

7. When both runs are recorded, keep the default model in production unless the
   candidate is deliberately promoted; promotion requires re-ingesting with the
   candidate and, if its dimension differs, following
   `docs/operations/embedding-dimension.md`.
