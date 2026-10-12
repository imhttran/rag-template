# RQ-003 — Embedding-model comparison matrix (operator-verified)

**Type:** Operator-run measurement matrix. Distinct from any SOP-executed validation.
**Provenance:** measured 2026-10-12 by the operator against **disposable** databases with
**installed** models only (no downloads). Reproduces the recorded RAG-021/RAG-023D values.

## Fixed inputs

- Held-out split: `evals/retrieval-vi-en-expanded.json`, sha256
  `7f6926dfa7a3e3a63174c6be52bd396aa3fd6efbac36f3022dc0c7ffcbd2d0df` (39 cases: 33 answerable + 6 unanswerable).
- Corpus manifest: `evals/corpus-vi-en.json`, sha256
  `2ddbef2f40870afc92cf2df14f3a7953db1037d1d63c6ee3f39f5708fbc87b21` (8 documents, 83 chunks at 50/20).
- Floor `MIN_SIMILARITY=0.70`, `TOP_K=4`, `CHUNK_SIZE=50`, `CHUNK_OVERLAP=20`,
  `QUERY_REWRITE=false` (no chat model). Embedding dimension 768 (matches `vector(768)`).

## Models

| id | version | dim | notes |
| --- | --- | --- | --- |
| `nomic-embed-text` | `nomic-embed-text:latest` | 768 | production default |
| `embeddinggemma` | `embeddinggemma:latest` | 768 | already installed; **no download** |

## Database isolation

| model | database | chunks ingested | notes |
| --- | --- | --- | --- |
| nomic-embed-text | `rag_021_test` | 83 | truncated + fresh ingest; `rag_db` never touched |
| embeddinggemma | `rag_021_gemma` | 83 | truncated + fresh ingest; `rag_db` never touched |

## Held-out metrics (floor `0.70`, mean of 3 repeats where noted)

| model | R@1 | R@4 | P@4 | evidence (before→after) | rejection |
| --- | --- | --- | --- | --- | --- |
| **nomic-embed-text** | **0.42** | **0.52** | 0.22 | 0.42 → **0.48** | 6/6 (1.00) |
| embeddinggemma | 0.27 | 0.27 | 0.22 | 0.24 → 0.27 | 6/6 (1.00) |

## Per-language-direction hybrid R@1 / R@4 (held-out)

| kind | n | nomic R@1 | nomic R@4 | gemma R@1 | gemma R@4 |
| --- | --- | --- | --- | --- | --- |
| en-en | 10 | 0.50 | 0.60 | 0.30 | 0.30 |
| vi-vi | 10 | 0.80 | 0.90 | 0.60 | 0.60 |
| misleading | 3 | 0.33 | 0.67 | 0.00 | 0.00 |
| **en-vi (cross-lingual)** | 5 | **0.00** | **0.00** | **0.00** | **0.00** |
| **vi-en (cross-lingual)** | 5 | **0.00** | **0.00** | **0.00** | **0.00** |

## Finding (no improvement claimed)

**Switching between the two installed embedding models does not improve cross-lingual
retrieval: both score 0.00 R@1 and 0.00 R@4 on all 10 cross-lingual cases.** nomic-embed-text
is superior overall (R@4 0.52 vs 0.27) and on every monolingual bucket. **Recommendation: no
model switch** — retain `nomic-embed-text`. Closing the cross-lingual gap requires a
genuinely multilingual model (**download not authorized**) or a non-embedding strategy
(query translation / language-aware retrieval).

## Reproduction commands

```
# nomic (rag_021_test) and gemma (rag_021_gemma); DATABASE_URL points at the disposable DB
psql "$DATABASE_URL" -c "TRUNCATE documents"
for f in examples/loan-policy.md examples/large-loan-policy.md examples/member-services-guide.md \
         examples/commercial-servicing-manual.md examples/vi/chinh-sach-khoan-vay.md \
         examples/vi/dich-vu-thanh-vien.md examples/vi/quy-trinh-thu-hoi.md examples/vi/bao-hiem-tai-san.md; do
  OLLAMA_EMBED_MODEL=<model> go run ./cmd/ingest "$f"; done
OLLAMA_EMBED_MODEL=<model> EVAL_DATASET=evals/retrieval-vi-en-expanded.json \
  MIN_SIMILARITY=0.70 QUERY_REWRITE=false go run ./cmd/eval
```

Captures: `docs/rq-001-per-case-eval-capture-nomic-070-expanded-20261012.txt` (nomic),
`docs/rq-003-operator-gemma-capture-expanded-20261012.txt` (gemma).

*Operator-measured; the historical record is unchanged; no production default changed.*
