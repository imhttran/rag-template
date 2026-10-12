# PLAN — RAG-023 decisions: F9–F12 architecture inventory (A5)

**Type:** Decision inventory / documentation only.
**Status:** Recorded (RAG-023-A5).
**Scope:** F9–F12 dead/unreachable surface — recommendations and roadmap rationale.
**No code is changed by RAG-023-A5.** Validation for this task is documentation only;
there is no code gate (no build/test/vet gate applies to this deliverable).

This record is self-contained so that RAG-023E can reference it without
rediscovering the findings. Each of F9, F10, F11, and F12 carries exactly one
recommendation from the allowed set — *wire up* / *document only* / *remove* — with
explicit roadmap rationale. Provider (F9) and metadata-filter (F10) capabilities are
recommended for removal **only if the roadmap supersedes them**; otherwise they default
to *document only* or *wire up*. Consistent with `PLAN-RAG-023.md` (“does not remove
provider/filter capabilities”), **any removal is proposed as a separate follow-up task
requiring operator authorization**, and no removal is performed by this plan.

## Allowed recommendations

- **wire up** — make the existing surface reachable from a real caller.
- **document only** — keep the surface as-is; record the decision, no code change.
- **remove** — delete the surface; permitted for F9/F10 only where the roadmap supersedes
the capability, and always proposed as a separate operator-authorized task.

## F9 — provider registry (`internal/provider`)

- **Symbol / location:** `package provider`; `provider.Default`;
  `provider.Embedder(name, Client)` and `provider.Generator(name, Client)`
  (`internal/provider/provider.go`). Constructs `embedding.Embedder` /
  `generation.Generator` from `EMBED_PROVIDER` / `GEN_PROVIDER` (default `ollama`).
- **Reference status at HEAD:** The registry is reachable through the config factories —
  `internal/config/config.go` resolves both interfaces via
  `provider.Embedder(c.EmbedProvider, c.providerClient())` and
  `provider.Generator(c.GenProvider, c.providerClient())`. It is covered by
  `internal/provider/provider_test.go`. No `cmd/*` package imports `internal/provider`
  directly; callers reach it only through `config.Config` factories. It is therefore
  wired through config, not a dead package, but it is not exercised by any `cmd/*` call
  site of its own.
- **Roadmap rationale (RAG-004/005):** RAG-004C1/C2 declare the provider registry
  (`internal/provider`) as **delivered**; `PLAN-RAG-004C12.md` records RAG-004C1/C2 as
  **COMPLETE**. `PLAN-RAG-004ABC.md` (parent of the 004 series) is
  **“Historicalized — archived as `SUPERSEDED`”**, but its supersession is an *archival*
  supersession by the split into 004A/004B/004C — not a supersession of the provider
  capability itself. `PLAN-RAG-005.md` is
  **“Historicalized — archived as `SUPERSEDED`”** over the embedding-dimension
  decoupling; it likewise does not supersede the provider registry. RAG-005 is not the
  provider capability.
- **Supersession test:** **No roadmap document supersedes the provider registry
  capability.** The archival `SUPERSEDED` markers on RAG-004ABC/005 concern plan
  bookkeeping (split/archival) and the embedding-dimension work, not removal of the
  provider abstraction.
- **Recommendation:** **document only.**
  The capability is delivered and reachable via config; the roadmap does not supersede
  it, so a *remove* recommendation is not permissible. Any future consolidation is out of
  scope for RAG-023, which explicitly does not remove provider capabilities.

## F10 — unused `Filter` (`retrieval.Filter` / `retrieval.PipelineOptions.Filter`)

- **Symbol / location:** `internal/retrieval/filter.go` — `type Filter struct`,
  `(f Filter) IsZero()`, `filterFragment(...)`; threaded through
  `retrieval.PipelineOptions.Filter` (`internal/retrieval/pipeline.go`) and the
  `*Filtered` search entry points (`SearchFiltered`, `KeywordSearchFiltered`,
  `SectionChunksFiltered`) in `internal/retrieval/retrieval.go`.
- **Reference status at HEAD:** The zero value is used internally by the non-filtered
  entry points (`Search` / `KeywordSearch` / `SectionChunks` call the `*Filtered` forms
  with `Filter{}`), and `PipelineOptions.Filter` is threaded through `HybridRetrieve`.
  The only non-zero `Filter` usages live in tests —
  `internal/retrieval/integration_test.go` (`SourcePrefix`, `Language`, `IngestedFrom`,
  `IngestedTo`, combined filters) and `internal/retrieval/pipeline_test.go`
  (`TestHybridRetrieveThreadsFilter`). No `cmd/*` caller sets a non-zero `Filter`:
  a search of `cmd/` finds no `Filter` usage, so no production caller reaches the
  filtering capability.
- **Roadmap rationale (RAG-009/011):** `PLAN-RAG-011.md` defines the optional
  parameterized `Filter` across `Search` / `KeywordSearch` / `SectionChunks`, threaded
  through `retrieval.PipelineOptions`; its status is
  **“Historicalized — archived as COMPLETE.”** `PLAN-RAG-009.md` is likewise
  **“Historicalized — archived as COMPLETE.”** Both record the capability as delivered;
  neither supersedes or withdraws metadata filtering.
- **Supersession test:** **No roadmap document supersedes the metadata-filter
  capability.** RAG-009/011 are COMPLETE (delivered), not superseded.
- **Recommendation:** **document only.**
  The capability is delivered and its zero-value path is live; only a non-zero caller is
  absent from `cmd/*`. Because the roadmap does not supersede it, a *remove*
  recommendation is not permissible. Wiring it up (giving `cmd/rag` or `cmd/eval` a real
  non-zero filter) is a plausible future task but is a behavior change beyond A5’s
  documentation scope.

## F11 — `HumanReporter` (`internal/observability`)

- **Symbol / location:** `internal/observability/observability.go` —
  `type HumanReporter struct`, `NewHumanReporter()`, and its methods `RunID()`, `Stage()`,
  `Counts()`, `Usage()`, `Finish()`; `New(false, ...)` returns the default
  `HumanReporter`.
- **Reference status at HEAD:** Reachable — `New(false, &bytes.Buffer{})` returns
  `*HumanReporter`, exercised by `internal/observability/observability_test.go`
  (`TestHumanReporterWritesNothing`). It is the default reporter (human-readable stage
  output); the JSON path is the alternate reporter. The type is not a dead symbol, but the
  default human path is the non-exercised-by-cmd surface referenced by F11.
- **Roadmap rationale:** No provider/filter roadmap (RAG-004/005, RAG-009/011) governs
  the observability reporter surface. RAG-023A3 references `internal/observability`
  scope (`json_answer_repro_test.go`) for F3/F4 baselines; RAG-023 itself does not target
  reporter removal. **No RAG-004/005/009/011 supersession applies.**
- **Supersession test:** Not applicable — the observability reporter is outside the
  provider/filter roadmap; no supersession is evidenced.
- **Recommendation:** **document only.**
  It is the default reporter, so *remove* would change default behavior (out of scope for
  RAG-023); no roadmap item supersedes it, so *wire up* is likewise not justified here.

## F12 — dead wrappers; `parseEntailment` duplicate

- **Symbol / location:**
  - `internal/answerability/answerability.go` — `parseEntailment(response string)` at
    line 293, called at line 290 (`return parseEntailment(response)`). This is the
    canonical/shared entailment parser.
  - The F12 “dead wrappers” are the thin pass-through/adapter wrappers named by the PRD
    around the citation/claim and retrieval surfaces; `PLAN-RAG-023.md` A5 names F12 as
    “dead wrappers; `parseEntailment` duplicate”, and RAG-023A4/C scope names
    `internal/citations` as the shared-parser target and `cmd/eval` as a duplicate-parser
    holder (`extractCitedClaims` reuses the shared parser).
- **Reference status at HEAD:** A search of `cmd/` for `parseEntailment` returns no
  matches — the `cmd/eval` duplicate parser referenced by F12 is the citation-claim
  parser (`extractCitedClaims`), which RAG-023C replaces with the exported shared parser
  in `internal/citations`. The `internal/answerability` `parseEntailment` is the retained
  canonical one. The duplicated/duplicate-parser site is the dead surface; the canonical
  parser is live.
- **Roadmap rationale:** RAG-023C is the governing roadmap item — it directs that
  `cmd/eval`’s `extractCitedClaims` **reuse the shared parser**, and that
  `internal/citations/citations.go` exposes the shared parser. RAG-023A4 records the
  F7/F8 baselines over `internal/citations` and `cmd/eval`. No RAG-004/005/009/011 item
  governs this; the applicable roadmap is **RAG-023/A4+C**, not the provider/filter
  series.
- **Supersession test:** The duplicate parser is superseded **by RAG-023C’s shared-parser
  consolidation** (the roadmap explicitly directs reuse of the shared parser). The dead
  wrappers are superseded by the same consolidation. This is a documentation-recorded
  consolidation target; the actual code change belongs to RAG-023C.
- **Recommendation:** **remove** (of the duplicate/dead surface) — executed under
  **RAG-023C**, not by A5.
  The duplicate parser and dead wrappers are superseded by the roadmap’s shared-parser
  consolidation, so *remove* is the permitted disposition; the canonical
  `internal/answerability.parseEntailment` is retained. Per the acceptance criteria, this
  removal is proposed as a **separate follow-up task requiring operator authorization**
  (see below) and is **not** performed by A5.

## Summary table

| Item | Surface | Status at HEAD | Superseded by roadmap? | Recommendation |
| --- | --- | --- | --- | --- |
| F9 | provider registry (`internal/provider`) | Delivered; reachable via `config` factories | No (RAG-004C1/C2 COMPLETE; 004ABC/005 archival SUPERSEDED only) | document only |
| F10 | unused `Filter` (`retrieval.Filter`) | Delivered; zero-value live, no non-zero `cmd/*` caller | No (RAG-009/011 COMPLETE) | document only |
| F11 | `HumanReporter` (`internal/observability`) | Default reporter, reachable | N/A — no applicable roadmap | document only |
| F12 | dead wrappers; `parseEntailment` duplicate | Duplicate parser in `cmd/eval`; canonical parser in `internal/answerability` | Yes — RAG-023C shared-parser consolidation | remove (under RAG-023C, separately authorized) |

## Proposed follow-up tasks (operator authorization required)

The following is a **proposal only**. It is not executed by RAG-023-A5. Every removal
below requires explicit **operator authorization** before any code is deleted.

### Proposed task — remove the duplicate citation/claim parser and dead wrappers (F12)

- **Scope:** Consolidate the citation/claim parsing onto the exported shared parser in
  `internal/citations/citations.go`; delete the duplicate parser/dead wrappers in
  `cmd/eval` (the `extractCitedClaims` duplicate) and any thin wrappers superseded by the
  shared parser. Retain the canonical `internal/answerability.parseEntailment`.
- **Authority:** Requires explicit operator authorization; aligns with **RAG-023C**
  (shared-parser reuse). **Not performed by A5.**
- **Note:** No removal of provider (F9) or metadata-filter (F10) capabilities is proposed;
  both are recorded as **document only** because the roadmap does not supersede them.

## No code changed; documentation-only validation

RAG-023-A5 changes **no code**. This record is the sole deliverable. Validation for A5 is
**documentation only** — there is no code gate (no build/test/vet gate applies). Any
removal is proposed above as a separate task that requires operator authorization; nothing
is removed, wired, or otherwise modified by this plan.
