#!/bin/sh
# Code review of the diff about to be pushed, by an agent.
#
# Project-agnostic: every setting comes from the environment and the findings go
# to standard output, so any repository can call it. Nothing here knows about Go,
# PostgreSQL, or this project.
#
#   REVIEW_BASE       default origin/main
#   REVIEW_PATHS      default *      git pathspec counted by REVIEW_MIN_LINES
#   REVIEW_MIN_LINES  default 20     changed lines that trigger a review
#   REVIEW_CMD        default claude (set it to use another agent; the
#                                    instructions and the diff arrive on stdin)
#   REVIEW_BUDGET     default 0.25   USD cap for the default command
#   REVIEW_PROMPT     default a short review instruction
#
# Advisory by construction: a finding never fails the caller, so it can run in
# parallel with other checks. It exits non-zero only if the review command itself
# fails. It exits 0 with a note when there is nothing worth reviewing.

set -e

base="${REVIEW_BASE:-origin/main}"

changed=$(
	git diff --numstat "$base"..HEAD -- "${REVIEW_PATHS:-*}" |
		awk '{ total += $1 + $2 } END { print total + 0 }'
)

min_lines="${REVIEW_MIN_LINES:-20}"

if [ "$changed" -lt "$min_lines" ]; then
	echo "review: skipped: $changed changed lines (min $min_lines)"

	exit 0
fi

if [ -n "$REVIEW_CMD" ]; then
	# Word-splitting is deliberate, so REVIEW_CMD can hold a command and its
	# flags.
	# shellcheck disable=SC2086
	set -- $REVIEW_CMD
elif command -v claude >/dev/null 2>&1; then
	set -- claude -p --tools "" --output-format text \
		--max-budget-usd "${REVIEW_BUDGET:-0.25}"
else
	echo "review: skipped: claude is not on PATH (set REVIEW_CMD)"

	exit 0
fi

prompt="${REVIEW_PROMPT:-Review the diff below for correctness bugs, security problems, missing error handling, and changed behaviour that no test covers. Report only real problems, one per line as file:line - problem - fix. Do not restate the diff. Do not make changes.}"

echo "review: agent reviewing the diff against $base"
echo

{ printf '%s\n\n' "$prompt"; git --no-pager diff "$base"..HEAD; } | "$@"
