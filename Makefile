# Convenience targets for the ingest/query workflow.
#
#   make db-up        start PostgreSQL and apply the schema
#   make db-schema    (re)apply every migration in migrations/
#   make test         unit tests (no database needed)
#   make fmt-json     rewrite every tracked JSON file with jq
#   make integration  tests against the docker-compose database (needs Docker)
#   make ingest       load examples/loan-policy.md
#   make ask Q="…"    ask a question (omit Q to be prompted)
#   make eval         score retrieval against evals/retrieval.json
#   make sweep        compare eval metrics across settings
#
# Override inputs on the command line, e.g.
#   make ingest FILE=examples/other.md
#   make ask Q="When is a late fee assessed?"

DATABASE_URL ?= postgres://rag:rag@127.0.0.1:5433/rag?sslmode=disable
FILE ?= examples/loan-policy.md
Q ?=
AXIS ?= all

.PHONY: db-up db-down db-schema test fmt-json integration ingest ask eval sweep

# Start the database and make sure the schema is applied.
db-up:
	docker compose up -d --wait
	$(MAKE) db-schema

# Stop the database.
db-down:
	docker compose down

# (Re)apply every migration in order.
db-schema:
	for f in migrations/*.sql; do psql '$(DATABASE_URL)' -f "$$f"; done

# Run the unit tests; no database required.
test:
	go test ./...

# Rewrite every tracked JSON file with jq's formatting. This expands the
# hand-compacted one-line arrays in .zed/settings.json and evals/retrieval.json,
# so the first run produces a large diff. Keys are left in the order written;
# add -S to jq below if you would rather sort them.
fmt-json:
	for f in $$(git ls-files -- '*.json'); do \
		jq . "$$f" > "$$f.tmp" || { rm -f "$$f.tmp"; echo "invalid json: $$f" >&2; exit 1; }; \
		mv "$$f.tmp" "$$f"; \
	done

# Run the PostgreSQL integration tests against the docker-compose database,
# starting it first. The tests truncate or delete from the documents table, so
# override DATABASE_URL to point at a throwaway database instead of the dev one.
#
# -p 1 runs the packages one at a time: internal/retrieval truncates the whole
# table, which would fight internal/ingestion's tests if they ran in parallel.
integration: db-up
	RAG_INTEGRATION=1 go test ./internal/retrieval/ ./internal/ingestion/ -run Integration -p 1 -v

# Chunk FILE and store it (safe to re-run).
ingest:
	go run ./cmd/ingest $(FILE)

# Ask a question; omit Q to be prompted.
ask:
	go run ./cmd/rag "$(Q)"

# Score retrieval against evals/retrieval.json.
eval:
	go run ./cmd/eval

# Compare cmd/eval metrics across a matrix of settings (see scripts/sweep.sh).
sweep:
	./scripts/sweep.sh $(AXIS)
