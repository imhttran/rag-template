# RAG Learning Project

A small Go project for learning Retrieval-Augmented Generation (RAG) from first
principles. It chunks a document, embeds the chunks with a local Ollama server,
stores them in PostgreSQL with pgvector, retrieves the closest chunks for a
question, and asks a local model to answer using only those chunks. A separate
command scores retrieval quality and compares a lexical reranker against plain
vector search.

## Workflow

Three commands:

| Command      | What it does                                                      |
| ------------ | ----------------------------------------------------------------- |
| `cmd/ingest` | Read a document → split into chunks → embed each chunk → store it |
| `cmd/rag`    | Embed a question → retrieve the closest chunks → answer from them |
| `cmd/eval`   | Score retrieval (recall/precision) and compare a lexical reranker |

```mermaid
flowchart TD
    doc[Markdown document] --> parse[Parse sections]
    parse --> chunk[Chunk]
    chunk --> embed[Embed with Ollama]
    embed --> db[(PostgreSQL + pgvector)]
    q[Question] --> qembed[Embed with Ollama]
    qembed --> search[Top-k search]
    db --> search
    search --> filter[Filter by MIN_SIMILARITY]
    filter --> ctx[Build context]
    ctx --> answer[Answer with local model]
```

## Shortcuts

A `Makefile` wraps the same steps:

| Target             | Runs                                                  |
| ------------------ | ----------------------------------------------------- |
| `make db-up`       | start the database and apply all migrations           |
| `make db-down`     | `docker compose down`                                 |
| `make db-schema`   | apply all migrations (existing volume only)           |
| `make test`        | run the unit tests (no database needed)               |
| `make fmt-json`    | rewrite every tracked JSON file with `jq`             |
| `make integration` | run the integration tests against the database        |
| `make ingest`      | ingest `examples/loan-policy.md` (`FILE=…` to change) |
| `make ask Q="…"`   | ask a question (omit `Q` to be prompted)              |
| `make eval`        | score retrieval against `evals/retrieval.json`        |

## Prerequisites

- Go 1.26 or newer
- Ollama running locally, with an embedding model and a chat model:

  ```bash
  ollama pull nomic-embed-text
  ollama pull qwen3.8:27b-mlx   # or set OLLAMA_CHAT_MODEL to a model you have
  ```

- Docker (for the bundled PostgreSQL + pgvector)

## Setup

Start the database and apply the migrations (pgvector extension, `documents`
table, and the vector index):

```bash
make db-up
```

`docker compose` runs every file in `migrations/` in name order the first time
the volume is created, which only covers a fresh database. `make db-up`
therefore re-applies the migrations afterwards, and `make db-schema` does so on
demand for an existing volume. Without `make`:

```bash
docker compose up -d --wait
for f in migrations/*.sql; do
  psql 'postgres://rag:rag@127.0.0.1:5433/rag?sslmode=disable' -f "$f"
done
```

Every migration is idempotent, so re-running is safe.

## 1. Ingest

Chunk a document, embed each chunk, and store it:

```bash
go run ./cmd/ingest examples/loan-policy.md
```

The corpus is markdown; each `## section` is split into overlapping ~100-word
chunks stored with their `source`, `section`, and `chunk_index` for citations.
Output:

```text
Ingested 6 chunks from loan-policy.md.
```

Re-running replaces that file's stored chunks, so the store never accumulates
duplicates.

## 2. Query

Ask a question:

```bash
go run ./cmd/rag "What happens if someone misses a payment?"
```

The question can also come from the `QUESTION` setting, or be typed at the prompt
when the command is run with no argument:

```bash
QUESTION="What happens if someone misses a payment?" go run ./cmd/rag
go run ./cmd/rag   # prompts: Question:
```

Output (scores and the answer vary by model and data):

```text
Question:
What happens if someone misses a payment?

Ollama created a 768-dimensional query vector

Vector retrieval:
KEPT      <score>  [loan-policy.md - Missed Payments - chunk 0] If a borrower misses a payment, ...
KEPT      <score>  [loan-policy.md - Late Fees - chunk 0] A late fee may be assessed ...
FILTERED  <score>  A delinquent loan may eventually be referred to collections ...

Hybrid retrieval:
RRF <score>  vector <score>  keyword <score>  [loan-policy.md - Missed Payments - chunk 0] ...

Expanded context:
[loan-policy.md - Missed Payments - chunk 0] ...
[loan-policy.md - Missed Payments - chunk 1] ...

Context being sent to the LLM:
Source: loan-policy.md
Section: Missed Payments
Chunk: 0
Content: If a borrower misses a payment, ...

Answer:
<the model's answer, citing [loan-policy.md - Missed Payments] where relevant>
```

Retrieval is hybrid: vector search (`TOP_K` candidates) and PostgreSQL full-text
search (`TOP_K` candidates) are combined with reciprocal rank fusion, keeping the
top `FINAL_K` fused chunks, and each matched section is then expanded to its
chunks (`EXPAND_LIMIT` cap) before the context is sent to the model.
Vector-only candidates below `MIN_SIMILARITY` are marked `FILTERED` and left out.

When `QUERY_REWRITE` is set, `cmd/rag` first rewrites the question into a search
query with the chat model (`internal/rag`), then retrieves over both
the original question and the rewritten query. Each query runs a vector search
and a PostgreSQL full-text search, and the four rankings are fused with
reciprocal rank fusion before section deduplication and expansion. Otherwise it
retrieves over the original question only. Either way the model answers the
original question.

The original query is kept alongside the rewrite rather than replaced: the
rewrite is lossy, and retrieving over it alone finds fewer of the expected
documents (see the evaluation below). Fusing both matches the original query's
retrieval while hedging against a bad rewrite.

When `RAG_LLM_RERANK` is set, `cmd/rag` asks the chat model to reorder the fused
candidates by relevance (`reranking.RerankLLM`), keeps the top `FINAL_K`, and
expands those sections before answering — the same reranker the evaluation
compares under `EVAL_LLM_RERANK`. Otherwise it expands the fused sections
directly.

Before answering, `cmd/rag` asks the chat model whether the kept documents can
actually answer the question (`RAG_ANSWERABILITY_GATE`, on by default). If they
cannot, it replies "I do not have enough information." instead of answering.

## 3. Evaluate

`cmd/eval` scores retrieval against `evals/retrieval.json`: a list of questions,
each with the `source`/`section` documents that should be retrieved. Ingest the
example corpora first, then run it:

```bash
go run ./cmd/ingest examples/loan-policy.md
go run ./cmd/ingest examples/large-loan-policy.md
go run ./cmd/ingest examples/member-services-guide.md
go run ./cmd/ingest examples/commercial-servicing-manual.md
go run ./cmd/eval
```

For each case it reports recall and precision at K = 1, 2, `TOP_K` for plain
vector search and for hybrid retrieval (vector and keyword results fused with
RRF). When a reranker is enabled it runs the reranking experiment — retrieve
`TOP_K` candidates, rerank with `internal/reranking`, keep the top `FINAL_K` —
reporting the same metrics, followed by averages across all cases.

It also runs the production pipeline from `internal/retrieval`, the same one
`cmd/rag` uses, and reports evidence recall before and after section expansion so
the effect of expanding a section into all of its chunks is visible. Add an
`evidence` array of substrings to a case in `evals/retrieval.json` to opt in.

The reranker is lexical: it scores a candidate by how many distinct question
words appear in its section and content, and `retrieval.DeduplicateSections`
drops all but the top-ranked chunk per source/section before reranking. It is a
teaching baseline, not a semantic reranker.

Both rerankers are off by default. Set `EVAL_LEXICAL_RERANK=true` for the lexical
one and/or `EVAL_LLM_RERANK=true` for the chat model (`RerankLLM` in the same
package), which asks the model to order the candidates by semantic relevance.
They use the same candidate set, so their results are reported side by side.

An optional answerability gate (`EVAL_ANSWERABILITY_GATE=true`) asks the chat
model whether the expanded evidence can answer each question — the same
documents `cmd/rag` sends to its answerability gate and the model — and reports
a confusion matrix (correct accepts/rejects, false rejects/accepts).

An optional fact judge (`EVAL_FACT_JUDGE=true`) asks the chat model which of a
case's `expected_facts` the expanded evidence supports, and reports the total
supported across cases. Add an `expected_facts` array to a case in
`evals/retrieval.json` to opt in.

An optional query rewrite (`QUERY_REWRITE=true`) rewrites each question into a
search query with the chat model (`internal/rag`), then reports
multi-query retrieval — a vector and a full-text search for both the original
and the rewritten query, four rankings fused with RRF. This is what `cmd/rag`
does with `QUERY_REWRITE` set.

Adding `EVAL_REWRITE_ONLY=true` instead retrieves over the rewritten query alone
— the experiment that justifies keeping the original. Averaged over the
two-document example corpus, hybrid retrieval scores:

| Mode                 | Recall@1 | Precision@1 | Recall@4 | Precision@4 |
| -------------------- | -------- | ----------- | -------- | ----------- |
| original only        | 0.67     | 0.95        | 1.00     | 0.67        |
| rewritten only       | 0.59     | 0.80        | 0.88     | 0.64        |
| original + rewritten | 0.67     | 0.95        | 1.00     | 0.67        |

Rewriting alone loses recall (0.67 → 0.59 at K=1, 1.00 → 0.88 at K=4), while
fusing it with the original recovers the loss and matches original-only
retrieval. That is why the original question is never replaced by its rewrite.

### Comparing settings

`scripts/sweep.sh` (or `make sweep`) runs `cmd/eval` once per configuration and
prints the Overall averages as one row per configuration, so a change is judged
by the numbers instead of by a couple of answers:

```bash
make sweep              # every axis
make sweep AXIS=chunk   # or topk | finalk | rewrite | rerank
```

Each row shows hybrid retrieval's per-K recall and precision plus whichever
optional metrics that configuration produced. The `chunk` axis re-ingests
examples/*.md before each step (cmd/ingest replaces a file's chunks, so
repeating is safe); the `chunk`, `rewrite`, and `rerank` axes call Ollama, and
every axis needs the database up and the corpora ingested.

## Configuration

All three commands read the same settings from the environment. The defaults
work with the bundled `docker-compose.yml` and a local Ollama.

`.env.example` lists every setting; copy it to `.env` and load it into your
shell (Go does not read `.env` files on its own):

```bash
cp .env.example .env
set -a; source .env; set +a
```

| Variable                  | Default                                                 | Used by   |
| ------------------------- | ------------------------------------------------------- | --------- |
| `OLLAMA_URL`              | `http://localhost:11434`                                | all       |
| `OLLAMA_EMBED_MODEL`      | `nomic-embed-text`                                      | all       |
| `OLLAMA_CHAT_MODEL`       | `qwen3.8:27b-mlx`                                       | rag, eval |
| `DATABASE_URL`            | `postgres://rag:rag@127.0.0.1:5433/rag?sslmode=disable` | all       |
| `CHUNK_SIZE`              | `100`                                                   | ingest    |
| `CHUNK_OVERLAP`           | `20`                                                    | ingest    |
| `TOP_K`                   | `4`                                                     | rag, eval |
| `FINAL_K`                 | `2`                                                     | rag, eval |
| `EXPAND_LIMIT`            | `20`                                                    | rag, eval |
| `MIN_SIMILARITY`          | `0.6`                                                   | rag, eval |
| `EVAL_LEXICAL_RERANK`     | `false`                                                 | eval      |
| `EVAL_LLM_RERANK`         | `false`                                                 | eval      |
| `RAG_LLM_RERANK`          | `false`                                                 | rag       |
| `EVAL_ANSWERABILITY_GATE` | `false`                                                 | eval      |
| `EVAL_FACT_JUDGE`         | `false`                                                 | eval      |
| `QUERY_REWRITE`           | `false`                                                 | rag, eval |
| `EVAL_REWRITE_ONLY`       | `false`                                                 | eval      |
| `RAG_ANSWERABILITY_GATE`  | `true`                                                  | rag       |
| `REQUEST_TIMEOUT`         | `5m`                                                    | all       |
| `QUESTION`                | _(none — pass it as an argument, or type it)_           | rag       |

`all` = every command; `rag` = `cmd/rag` only; `eval` = `cmd/eval` only;
`ingest` = `cmd/ingest` only; `rag, eval` = both commands.

## Project structure

```text
rag-template/
├── cmd/
│   ├── eval/              # score retrieval and compare reranking
│   ├── ingest/            # chunk a file into the documents table
│   └── rag/               # answer a question from the stored documents
├── internal/
│   ├── answerability/     # ask the chat model whether the evidence answers the question
│   ├── chunking/          # sections -> chunks
│   ├── config/            # settings + Ollama/Postgres setup
│   ├── document/          # parse markdown into sections
│   ├── embedding/         # text -> vector (Ollama /api/embed)
│   ├── generation/        # prompt -> answer (Ollama /api/generate)
│   ├── ingestion/         # replace a source's chunks + embeddings atomically
│   ├── ollama/            # shared JSON client for the Ollama server
│   ├── rag/               # rewrite the question, build the answer prompt
│   ├── reranking/         # lexical + LLM rerankers
│   └── retrieval/         # pgvector search, RRF fusion, section expansion
├── migrations/
│   └── 001_init.sql       # pgvector extension + documents table + index
├── evals/
│   └── retrieval.json     # questions + expected source/section
├── examples/
│   ├── loan-policy.md                  # sample corpus for cmd/ingest
│   ├── large-loan-policy.md            # longer corpus; several chunks per section
│   ├── member-services-guide.md        # distractor corpus for the evaluation
│   └── commercial-servicing-manual.md  # long sections; exercises CHUNK_SIZE / CHUNK_OVERLAP
├── .agents/
│   └── scripts/                   # agent scripts; project-agnostic
│       ├── audit-agent.sh             # over-engineering audit, writes AUDIT.md
│       ├── integration-test-agent.sh  # run the integration tests
│       └── review-agent.sh            # code review agent
├── .githooks/
│   ├── pre-commit         # tidy / fmt / jq / vet / staticcheck / test / build
│   └── pre-push           # integration tests
├── .env.example
├── .gitignore
├── docker-compose.yml
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

The commands only wire these packages together.

## Git hooks

`.githooks/pre-commit` runs `go mod tidy`, `go fmt ./...`, `go vet ./...`,
`staticcheck ./...`, `go test ./...`, and `go build ./...`, and aborts the commit
if any step fails. It also aborts when `go mod tidy` or `go fmt` changed a file
that is part of the commit, so the fix can be staged before committing again.

It also runs `jq empty` over every tracked `*.json`, so a malformed
`evals/retrieval.json` or `.zed/settings.json` fails the commit instead of the
command that reads it. `jq` is skipped with a note if it is not on `PATH`.
`make fmt-json` is the reformatting counterpart, for when you want jq's layout
rather than just a syntax check.

`staticcheck` must be on `PATH` (`brew install staticcheck` on macOS). It catches
unused code — functions, methods, and types — which `go vet` does not.

`.githooks/pre-push` runs what the pre-commit hook skips, using one
project-agnostic script in `.agents/scripts/`. The hook supplies only what is
specific to this project.

`.agents/scripts/integration-test-agent.sh` runs the database tests
(`internal/retrieval`, `internal/ingestion`) given by `INTEGRATION_TEST_CMD`. It
probes `INTEGRATION_TEST_DB` with `psql` first — this project points that at
`RAG_TEST_DATABASE_URL` (a throwaway database, `rag_test` by default), because the
tests truncate the `documents` table and must never touch `DATABASE_URL`. An
unreachable database is skipped with a note, so a stopped Docker daemon never
blocks a push; `INTEGRATION_TEST_STRICT` (`RAG_REQUIRE_INTEGRATION=1`) fails
instead. It also fails when the command passes but no test ran, so a `-run` filter
that matches nothing cannot turn the gate green.

`.agents/scripts/review-agent.sh` reviews the diff against `origin/main` and is
advisory — a finding never fails anything. It is not wired into the hook either,
so run it by hand when you want a second pair of eyes: it is read-only
(`--tools ""`), budget-capped (`REVIEW_BUDGET`, default `0.25` USD), and skips
itself unless at least `REVIEW_MIN_LINES` lines of what `REVIEW_PATHS` matches
changed.

`.agents/scripts/audit-agent.sh` hunts over-engineering — dependencies the
standard library already ships, single-implementation interfaces, dead flags — and
writes its report to `AUDIT.md` at the root of the repository (gitignored). It is
not wired into the hook, so it never delays a push: run it by hand. It gets
read-only tools (`AUDIT_TOOLS`, default `Read,Grep,Glob`), so it can walk the tree
but cannot change it, and it is budget-capped (`AUDIT_BUDGET`, default `0.50` USD).
It skips itself unless at least `AUDIT_MIN_LINES` lines changed against
`AUDIT_BASE`.

The audit's rulebook is a skill file, `AUDIT_SKILL`, so one file drives the
script and an editor session that invokes the skill. It defaults to the global
ponytail-audit skill at `~/.agents/skills/ponytail-audit/SKILL.md`; point it
somewhere else to audit by different rules. Run it whenever you want a report:

```sh
AUDIT_MIN_LINES=0 sh .agents/scripts/audit-agent.sh
```

The scripts read their settings from environment variables, so another repository
can reuse them by copying the folder somewhere shared and pointing a hook (or a
manual run) at them:

```sh
AUDIT_AGENT=/path/to/shared/audit-agent.sh
INTEGRATION_TEST_AGENT=/path/to/shared/integration-test-agent.sh
REVIEW_AGENT=/path/to/shared/review-agent.sh
```

Set `REVIEW_MIN_LINES=0` to review any change, or `REVIEW_CMD=false` to switch the
review off; `AUDIT_MIN_LINES=0` and `AUDIT_CMD=false` do the same for the audit.
Each script documents the rest.

Git does not pick up `.githooks/` on its own. Enable it once per clone:

```bash
git config core.hooksPath .githooks
```

Bypass the commit hook with `git commit --no-verify`, or the push hook with
`git push --no-verify`.

## Tests

`go test ./...` runs the unit tests. They need no database or model: chunking,
document parsing, reranking, answerability, the eval metrics, and RRF fusion are
pure functions, and `HybridRetrieve` is driven through a fake `Searcher` that
records the arguments each stage received, so the pipeline's stage order and its
use of the similarity floor and section deduplication are pinned down.

The SQL needs PostgreSQL, so `internal/retrieval` and `internal/ingestion` have
integration tests. They connect to `DATABASE_URL`, apply
`migrations/001_init.sql` (idempotent, like `make db-up`), and cover:

- `internal/retrieval` — `Search`, `KeywordSearch`, `SectionChunks`, and the whole
  `HybridRetrieve` pipeline against real data, the part a fake cannot check,
  since column order, scan alignment, and placeholder numbering only fail against
  a real server.
- `internal/ingestion` — `ReplaceDocument`'s delete-then-insert transaction:
  re-running replaces rather than appends, the `$5::vector` cast round-trips, and
  a failed embedding leaves the stored document untouched. Embeddings come from a
  stub Ollama server, so no model is needed.

They are skipped unless `RAG_INTEGRATION=1` is set, so the pre-commit hook stays
fast and database-free. `make integration` brings the database up first:

```bash
make integration
```

Both packages use the `documents` table, and `internal/retrieval` truncates it, so
`make integration` passes `-p 1` to run them one at a time. Because the tests
truncate, run them against a throwaway database rather than the dev one;
`DATABASE_URL` is honoured by both the migration step and the tests:

```bash
psql 'postgres://rag:rag@127.0.0.1:5433/rag?sslmode=disable' -c 'CREATE DATABASE rag_test'
make integration DATABASE_URL='postgres://rag:rag@127.0.0.1:5433/rag_test?sslmode=disable'
```

## Learning path

1. Generate embeddings with Ollama
2. Measure cosine similarity
3. Read raw embedding vectors from the Ollama `/api/embed` response
4. Add document chunking
5. Store embeddings in PostgreSQL + pgvector
6. Retrieve top-k chunks
7. Send retrieved context to a local Qwen model
8. Add metadata and citations
9. Add hybrid search and reranking (`cmd/eval` compares a lexical reranker)
10. Add evaluation (`cmd/eval` reports recall and precision)
