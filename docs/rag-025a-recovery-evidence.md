# RAG-025A — Recovery evidence (provider integration)

**Type:** Recovery/acceptance evidence record. The original SOP outcome is preserved untouched;
this document supplements it with independently verified evidence. **No SOP state was rewritten.**
**Baseline:** `main` @ `79ed678`.

## SOP status (unchanged)

- Task **RAG-025A** — SOP status **`LOCAL_DONE`**, gate **`PASS`**.
- Recorded outcome: **`PARTIAL`** — SOP's spec-vs-outcome check marked `deliverable-1` and
  `criteria-1…5` + `objective-acceptance` as **not met** with reason
  **"unverifiable: no deterministic check supplied"**.
- **Cause:** the plan's acceptance criteria were prose, so SOP could not map them to
  deterministic checks. This is a **verification-metadata artifact**, not an implementation
  failure — preserved here as historical evidence.
- No pending approvals.

## Completed deliverables

| # | Deliverable | Status |
| --- | --- | --- |
| D1 | `cmd/rag`, `cmd/eval`, `cmd/ingest` resolve the embedder/generator through `cfg.Embedder()` / `cfg.Generator()` (provider registry) | done |
| D2 | `internal/config/config.go` — fail-fast validation of `EMBED_PROVIDER`/`GEN_PROVIDER` at `Load` (names variable + value) | done |
| D3 | `internal/provider/stub_test.go` + `internal/provider/provider_test.go` — deterministic stub provider; selection + unsupported-provider tests | done (seam moved to `registry_seam_test.go`) |
| D4 | Docs corrected: `docs/reference/configuration.md`, `docs/architecture/OVERVIEW.md`, `docs/reference/capabilities.md` | done |

## Default-Ollama equivalence — structural, not a live byte-identical diff

- The commands now call `cfg.Embedder()` / `cfg.Generator()`; for `EMBED_PROVIDER`/`GEN_PROVIDER`
  unset or `ollama`, these delegate to `provider.Embedder("ollama", client)` /
  `provider.Generator("ollama", client)`, which construct
  `embedding.New(ollama.New(OllamaURL, &http.Client{Timeout: RequestTimeout}), EmbedModel)` /
  `generation.New(…, ChatModel)` — **the same vendor client, URL, model, and timeout the
  commands constructed directly before**.
- **This is a structural equivalence** (identical construction and configuration), **verified
  by inspection and the passing tests**, **not** a byte-identical comparison of live model
  outputs. A byte-identical output check would require a running Ollama model and is the
  **optional smoke test** (not a gate). Do not read it as a proven byte-for-byte equality of
  embeddings/answers.

## Verification commands and results (independently run)

| Command | Result |
| --- | --- |
| `gofmt -l .` | clean |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go test -count=1 ./internal/provider ./internal/config ./cmd/rag ./cmd/ingest ./cmd/eval` | PASS (5 packages ok) |
| `go test -race -count=1 ./internal/provider ./internal/config` | PASS |
| `git diff --check` | exit 0 |
| `go test -run TestLoad ./internal/config -v` | subtests `unknown embed provider fails fast`, `unknown gen provider fails fast` PASS |
| `EMBED_PROVIDER=bogus go run ./cmd/{ingest,rag,eval}` | exit 1, `EMBED_PROVIDER="bogus" is not a registered provider` |
| `GEN_PROVIDER=bogus go run ./cmd/rag` | exit 1, `GEN_PROVIDER="bogus" is not a registered provider` |

## Changed files

- Modified: `cmd/rag/main.go`, `cmd/eval/main.go`, `cmd/ingest/main.go`, `internal/config/config.go`,
  `internal/config/config_test.go`, `internal/provider/provider.go`, `internal/provider/provider_test.go`,
  `docs/reference/configuration.md`, `docs/architecture/OVERVIEW.md`, `docs/reference/capabilities.md`.
- Removed: `internal/provider/registry_test_seam.go` (test-only code that was compiled into the
  production package).
- Added: `internal/provider/stub_test.go`, `internal/provider/registry_seam_test.go`.

## Attestation

RAG-025A remains **`LOCAL_DONE`** with **no pending approvals**; it was **not rerun**. This
document records recovery evidence only and does not override SOP's `PARTIAL` outcome record.
