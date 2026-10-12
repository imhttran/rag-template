# RQ-006 — Bilingual retrieval arms (operator-run)

**Type:** Operator-run experiment. Evaluation-only harness `cmd/expbilingual`; **no production
retrieval change**. Frozen inputs, disposable DB (`rag_021_test`), `nomic-embed-text`,
floor `0.70`, unchanged scoring.

## Setup
- Held-out: `evals/retrieval-vi-en-expanded.json` (39 cases; 33 answerable + 6 unanswerable).
- Corpus: `evals/corpus-vi-en.json` (8 docs, 83 chunks at 50/20), ingested into `rag_021_test`.
- Query generation: installed local model `llama3.2:3b` (no download).
- Arms: **A** baseline; **B** bilingual expansion (one expanded query, fused); **C** translation
  to Vietnamese and English, fused with the original.

## Results (hybrid, floor 0.70; over the 33 answerable cases)

| arm | overall R@1 | overall R@4 | evidence | rejection (6 unanswerable) | cross-lingual R@1 | cross-lingual R@4 | wall |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **A** baseline | **0.42** | 0.52 | 0.52 | **6/6 = 1.00** | 0.00 | 0.00 | 1.9 s |
| **B** expansion | 0.39 | 0.64 | 0.64 | **1/6 = 0.17** | 0.00 | 0.30 | 17.6 s |
| **C** translation | 0.39 | 0.64 | 0.73 | **0/6 = 0.00** | 0.00 | 0.40 | 27.3 s |

Per-direction R@4: `en-vi` A `0.00` → B `0.60` → C `0.80`; **`vi-en` stays `0.00` in all arms**.
Monolingual `vi-vi` R@1 drops `0.80` → `0.70`; `en-en` unchanged.

## Finding
- Arms B and C **raise cross-lingual R@4 from 0.00 to 0.30–0.40** and evidence to 0.30–0.60 —
  driven entirely by **`en-vi`** (English query → Vietnamese doc). **`vi-en` remains 0.00.**
- **But both arms destroy rejection** (`1.00 → 0.17 → 0.00`): unanswerable questions now return
  false positives. Under the acceptance rule (rejection-first) **both arms fail**.
- R@1 is unchanged (`0.00` cross-lingual; overall slightly down). The gain is recall@4 only.

## Recommendation
**No production retrieval change.** The cross-lingual recall gain is real but unacceptable
because it removes the rejection control. Any future work must be **rejection-preserving**
(e.g., gate expansion/translation behind an answerability check, or add a rejection guard)
before it can be considered.

## Caveats / reproducibility
- The evidence metric here expands over **all fused sections**, so it is not byte-identical to
  `cmd/eval`'s FinalK-scoped evidence (arm A shows `0.52` vs the recorded `0.48`); R@1/R@4 match
  the baseline exactly. Comparisons across arms use one consistent method.
- The local model is not pinned for determinism; these are **single runs**. Treat cross-arm
  deltas as directional, and re-run with pinned decoding to confirm.
- Captures: `docs/rq-006-operator-armA-baseline.txt`, `docs/rq-006-operator-armB-expansion-llama.txt`,
  `docs/rq-006-operator-armC-translation-llama.txt`.

*Operator-measured; frozen inputs, the `0.70` threshold, and historical RAG-023 records are unchanged.*
