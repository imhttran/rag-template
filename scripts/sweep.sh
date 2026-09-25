#!/bin/sh
#
# Compare cmd/eval metrics across a matrix of settings, one row per
# configuration. Only the Overall averages are shown; the per-case output is
# discarded.
#
#   make sweep              every axis
#   make sweep AXIS=chunk   one axis: chunk | topk | finalk | rewrite | rerank
#
# Every axis needs the database up with the schema applied. The chunk axis
# re-ingests examples/*.md before each step (cmd/ingest replaces a file's
# chunks, so repeating is safe); the chunk, rewrite, and rerank axes call
# Ollama, so allow time.

set -eu

DOCS="examples/loan-policy.md examples/large-loan-policy.md examples/member-services-guide.md examples/commercial-servicing-manual.md"

AXIS=${1:-all}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

out="$work/eval.out"

# report LABEL runs cmd/eval with the current environment and prints one row.
# Only the Overall block is read: the per-case lines before it share the same
# prefixes ("Rerank", "LLM Rerank", "Citation Validity", ...), so grepping the
# whole output would collect those too.
report() {
	if ! go run ./cmd/eval >"$out" 2>&1; then
		printf '%-28s FAILED\n' "$1"
		tail -3 "$out"
		return 0
	fi

	overall=$(sed -n '/^Overall:/,$p' "$out")

	hybrid=$(printf '%s\n' "$overall" | sed -n '/^Hybrid retrieval:/,$p' | grep '^K=' | tr '\n' ' ' | tr -s ' ' || true)
	extra=$(printf '%s\n' "$overall" | grep -E '^(Rerank |LLM Rerank |Answerability gate:|Avg Evidence Recall:|Generated Fact Recall:|Similarity-only rejection=|Groundedness:|Citation Validity:|Citation Entailment:)' | tr '\n' ' ' | tr -s ' ' || true)

	printf '%-28s %s%s\n' "$1" "$hybrid" "$extra"
}

# ingest_all chunks every example document with the current chunk settings.
ingest_all() {
	for doc in $DOCS; do
		go run ./cmd/ingest "$doc" >/dev/null
	done
}

if [ "$AXIS" = all ] || [ "$AXIS" = chunk ]; then
	echo "== chunk size / overlap =="

	for size in 50 100 200; do
		export CHUNK_SIZE=$size

		for overlap in 0 20 40; do
			export CHUNK_OVERLAP=$overlap
			ingest_all
			report "chunk=${size}/${overlap}"
		done
	done

	# Restore the default chunking so the later axes share one baseline.
	unset CHUNK_SIZE CHUNK_OVERLAP
	ingest_all
fi

if [ "$AXIS" = all ] || [ "$AXIS" = topk ]; then
	echo
	echo "== TOP_K =="

	for value in 2 4 8; do
		export TOP_K=$value
		report "top_k=$value"
	done

	unset TOP_K
fi

if [ "$AXIS" = all ] || [ "$AXIS" = finalk ]; then
	echo
	echo "== FINAL_K =="

	for value in 1 2 3; do
		export FINAL_K=$value
		report "final_k=$value"
	done

	unset FINAL_K
fi

if [ "$AXIS" = all ] || [ "$AXIS" = rewrite ]; then
	echo
	echo "== query rewrite =="

	for value in false true; do
		export QUERY_REWRITE=$value
		report "rewrite=$value"
	done

	unset QUERY_REWRITE
fi

if [ "$AXIS" = all ] || [ "$AXIS" = rerank ]; then
	echo
	echo "== LLM rerank (eval) =="

	for value in false true; do
		export EVAL_LLM_RERANK=$value
		report "llm_rerank=$value"
	done

	unset EVAL_LLM_RERANK
fi
