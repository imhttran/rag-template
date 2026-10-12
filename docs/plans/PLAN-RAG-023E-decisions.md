# PLAN — RAG-023E: Consolidated F9–F12 architecture decision record

**Type:** Architecture assessment and decision record — documentation only.
**Status:** Recorded (RAG-023E).
**Depends on:** RAG-023-A5 (`docs/plans/PLAN-RAG-023-decisions.md`).
**Scope:** Consolidate the A5 F9–F12 inventory into a single operator-facing record.
**No code is changed by RAG-023E.** Validation for this task is documentation only; there
is no code gate (no build/test/vet gate applies to this deliverable). Any removal is a
**separately operator-authorized task** and is **not** performed here.

This record consolidates — and references rather than re-derives — the authoritative
inventory recorded by **RAG-023-A5** in
[`docs/plans/PLAN-RAG-023-decisions.md`](PLAN-RAG-023-decisions.md). The A5 record is
self-contained and explicitly written so that RAG-023E can reference it without
rediscovering the findings; this document does not restate the symbol-level analysis
beyond the disposition and roadmap rationale required by the RAG-023E acceptance
criteria.

## Source of record

- **Primary input:** `docs/plans/PLAN-RAG-023-decisions.md` (RAG-023-A5, “Recorded”).
- **Governing plan:** [`docs/plans/PLAN-RAG-023.md`](PLAN-RAG-023.md), section
  *RAG-023E — Architecture assessment and decision record (no implementation)*.
- **Roadmap rationale sources:** `PLAN-RAG-004C12.md`, `PLAN-RAG-004ABC.md`,
  `PLAN-RAG-005.md`, `PLAN-RAG-009.md`, `PLAN-RAG-011.md`, plus RAG-023C for F12.

## Disposition terms

The PRD/plan terms are used verbatim:

- **wire up** — make the existing surface reachable from a real caller.
- **document only** — keep the surface as-is; record the decision; no code change.
- **remove** — delete the surface; permissible only where the roadmap supersedes the
  capability, and always proposed as a **separate operator-authorized task**.

Consistent with `PLAN-RAG-023.md` (“**does not** remove provider/filter capabilities”),
F9 and F10 default to *document only*, and no provider/filter capability is recommended
for removal.

## F9 — provider registry (`internal/provider`) — **document only**

- **Disposition:** **document only.**
- **Supersession test:** **No roadmap document supersedes the provider registry.**
  RAG-004C1/C2 declare the registry delivered and `PLAN-RAG-004C12.md` records them as
  **COMPLETE**. The `SUPERSEDED` markers on `PLAN-RAG-004ABC.md` / `PLAN-RAG-005.md` are
  *archival* supersessions (plan split/archival and embedding-dimension decoupling), not a
  supersession of the provider capability.
- **Roadmap rationale (RAG-004/005):** The capability is delivered and reachable via the
  `config.Config` factories; RAG-004/005 do not withdraw it.
- **Why not remove / wire up:** A *remove* is not permissible because the roadmap does
  not supersede the capability, and `PLAN-RAG-023.md` explicitly does not remove provider
  capabilities. Wiring is unnecessary because it is already reachable through config.

## F10 — unused `Filter` (`retrieval.Filter`) — **document only**

- **Disposition:** **document only.**
- **Supersession test:** **No roadmap document supersedes the metadata-filter
  capability.** RAG-009 and RAG-011 are **COMPLETE** (delivered), not superseded.
- **Roadmap rationale (RAG-009/011):** `PLAN-RAG-011.md` defines the optional
  parameterized `Filter` threaded through `retrieval.PipelineOptions`; `PLAN-RAG-009.md`
  is likewise COMPLETE. The zero-value path is live; only a non-zero `cmd/*` caller is
  absent.
- **Why not remove / wire up:** A *remove* is not permissible because the roadmap does
  not supersede the capability and RAG-023 does not remove filter capabilities. Wiring a
  non-zero caller is a plausible future task but is a behavior change out of scope here.

## F11 — `HumanReporter` (`internal/observability`) — **document only**

- **Disposition:** **document only.**
- **Supersession test:** **Not applicable** — the observability reporter is outside the
  provider/filter roadmap; no RAG-004/005/009/011 supersession is evidenced.
- **Roadmap rationale:** No provider/filter roadmap governs the reporter surface, and
  RAG-023 does not target reporter removal.
- **Why not remove / wire up:** `HumanReporter` is the default reporter, so *remove*
  would change default behavior (out of scope); no roadmap item supersedes it, so *wire
  up* is likewise not justified here.

## F12 — dead wrappers; `parseEntailment` duplicate — **remove (under RAG-023C, separately authorized)**

- **Disposition:** **remove** of the duplicate/dead surface — executed under **RAG-023C**,
  not by RAG-023E.
- **Supersession test:** The duplicate citation/claim parser and dead wrappers are
  **superseded by RAG-023C’s shared-parser consolidation**, which explicitly directs
  `cmd/eval`’s duplicate parser to reuse the exported shared parser in
  `internal/citations/citations.go`. The canonical
  `internal/answerability.parseEntailment` is **retained**.
- **Roadmap rationale (RAG-023C):** The applicable roadmap is RAG-023/A4+C, not the
  provider/filter series.
- **Authorization status:** This removal is a **proposed, separately operator-authorized
  follow-up**. It is **not performed by RAG-023E** and requires explicit operator
  authorization before any code is deleted.

## Summary table

| Item | Surface | Disposition | Superseded by roadmap? | Roadmap rationale |
| --- | --- | --- | --- | --- |
| F9 | provider registry (`internal/provider`) | document only | No | RAG-004C1/C2 COMPLETE; 004ABC/005 archival SUPERSEDED only |
| F10 | unused `Filter` (`retrieval.Filter`) | document only | No | RAG-009/011 COMPLETE |
| F11 | `HumanReporter` (`internal/observability`) | document only | N/A | No applicable roadmap |
| F12 | dead wrappers; `parseEntailment` duplicate | remove (under RAG-023C) | Yes | RAG-023C shared-parser consolidation |

## Removal is a separately authorized follow-up

The **F12 remove** disposition is a **proposal only**. It is **not** executed by
RAG-023E. Per the RAG-023E acceptance criteria (“any removal is a separately authorized
task”), the removal is expressed strictly as a follow-up task that requires **explicit
operator authorization** before any code is deleted, aligned with **RAG-023C**
(shared-parser reuse). No removal task is created or executed by this record.

F9, F10, and F11 remain **document only**. No provider (F9) or metadata-filter (F10)
capability is proposed for removal.

## No code changed; documentation-only validation

RAG-023E changes **no code**. This consolidated decision record is the sole deliverable.
Validation is **documentation only** — there is no build/test/vet gate for this
deliverable. No removal, wiring, or behavioral change is executed. The stage records
no-code-change evidence by inspecting `git status` / `git diff` at its boundary; the only
change attributable to RAG-023E is this documentation file.

## Relationship to PLAN-RAG-023-decisions.md

This record **references** and **consolidates** `PLAN-RAG-023-decisions.md`
(RAG-023-A5); it does not re-derive or alter A5’s findings. A5 remains the authoritative
symbol-level inventory; RAG-023E is the operator-facing consolidated disposition record
derived from it.
