#!/bin/sh
#
# Compare cmd/eval metrics across a matrix of settings, one row per
# configuration. Only the Overall averages are shown; the per-case output is
# discarded.
#
#   make sweep              every axis
#   make sweep AXIS=chunk   one axis: chunk | topk | finalk | rewrite | rerank
#                           | minsim | judge
#
# Every axis needs the database up with the schema applied. The chunk axis
# re-ingests examples/*.md before each step (cmd/ingest replaces a file's
# chunks, so repeating is safe). QUERY_REWRITE is on by default, so every axis
# calls the chat model; run with QUERY_REWRITE=false to sweep without it.

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
	latency=$(printf '%s\n' "$overall" | sed -n '/^Rerank latency:/,$p' | grep -E '^  (off|lexical|llm|cross-encoder)' | tr '\n' ' ' | tr -s ' ' || true)

	printf '%-28s %s%s\n' "$1" "$hybrid" "$extra"

	if [ -n "$latency" ]; then
		printf '%-28s latency: %s\n' "$1" "$latency"
	fi
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

# The rerank axis compares the strategies on recall, precision, evidence recall,
# and latency. The off row is the baseline (no rerank call, zero rerank latency);
# lexical and LLM rows report their measured rerank wall time. A cross-encoder
# row is emitted only if a cross-encoder provider is registered (none is today),
# and cmd/eval marks the variant unavailable otherwise.
if [ "$AXIS" = all ] || [ "$AXIS" = rerank ]; then
	echo
	echo "== rerank strategies (off / lexical / llm) =="

	# off: both rerankers disabled, the baseline row. Every value that shapes a
	# row is set explicitly (never left to the ambient environment) so results do
	# not depend on variables exported by the caller.
	export EVAL_LEXICAL_RERANK=false
	export EVAL_LLM_RERANK=false
	export RERANK_TIMEOUT=0s
	report "rerank=off"

	# lexical only.
	export EVAL_LEXICAL_RERANK=true
	export EVAL_LLM_RERANK=false
	export RERANK_TIMEOUT=0s
	report "rerank=lexical"

	# llm only, guard disabled.
	export EVAL_LEXICAL_RERANK=false
	export EVAL_LLM_RERANK=true
	export RERANK_TIMEOUT=0s
	report "rerank=llm"

	# llm with a bounded latency guard, so a slow reranker falls back to the
	# fused order instead of stalling the run.
	export RERANK_TIMEOUT=10s
	report "rerank=llm+guard"

	unset EVAL_LEXICAL_RERANK EVAL_LLM_RERANK RERANK_TIMEOUT
fi

if [ "$AXIS" = all ] || [ "$AXIS" = minsim ]; then
	echo
	echo "== MIN_SIMILARITY =="

	for value in 0.4 0.6 0.8; do
		export MIN_SIMILARITY=$value
		report "min_similarity=$value"
	done

	unset MIN_SIMILARITY
fi

if [ "$AXIS" = all ] || [ "$AXIS" = judge ]; then
	echo
	echo "== generation judge (eval) =="

	for value in false true; do
		export EVAL_FACT_JUDGE=$value
		report "fact_judge=$value"
	done

	unset EVAL_FACT_JUDGE
fi
