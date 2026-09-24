#!/bin/sh
# Over-engineering audit of the whole tree, by an agent, written to a file.
#
# Project-agnostic: every setting comes from the environment and the report is
# written to AUDIT_OUTPUT, so any repository can call it. Nothing here knows
# about Go, PostgreSQL, or this project.
#
#   AUDIT_OUTPUT     default AUDIT.md         file the report is written to
#   AUDIT_BASE       default origin/main
#   AUDIT_PATHS      default *                git pathspec counted by AUDIT_MIN_LINES
#   AUDIT_MIN_LINES  default 20               changed lines that trigger an audit
#   AUDIT_CMD        default claude (set it to use another agent; the audit
#                                             instructions arrive on stdin)
#   AUDIT_BUDGET     default 0.50             USD cap for the default command
#   AUDIT_TOOLS      default Read,Grep,Glob   read-only tools, so the agent can
#                                             walk the tree but cannot change it
#   AUDIT_SKILL      default the ponytail-audit skill, see below
#   AUDIT_PROMPT     default the skill, plus how to shape the report
#
# Advisory by construction: a finding never fails the caller, so it can run in the
# background while a push proceeds. It exits 0, with a note, when there is nothing
# worth auditing or the agent is missing, and non-zero only when the audit command
# itself fails.
#
# The report is built in a temporary file and moved into place, so a reader never
# catches a half-written audit, and a failed run leaves the previous report alone.

set -e

out="${AUDIT_OUTPUT:-AUDIT.md}"
base="${AUDIT_BASE:-origin/main}"

changed=$(
	git diff --numstat "$base"..HEAD -- "${AUDIT_PATHS:-*}" |
		awk '{ total += $1 + $2 } END { print total + 0 }'
)

min_lines="${AUDIT_MIN_LINES:-20}"

if [ "$changed" -lt "$min_lines" ]; then
	echo "audit: skipped: $changed changed lines (min $min_lines)"

	exit 0
fi

if [ -n "$AUDIT_CMD" ]; then
	# Word-splitting is deliberate, so AUDIT_CMD can hold a command and its
	# flags.
	# shellcheck disable=SC2086
	set -- $AUDIT_CMD
elif command -v claude >/dev/null 2>&1; then
	set -- claude -p --tools "${AUDIT_TOOLS:-Read,Grep,Glob}" --output-format text \
		--max-budget-usd "${AUDIT_BUDGET:-0.50}"
else
	echo "audit: skipped: claude is not on PATH (set AUDIT_CMD)"

	exit 0
fi

# The rulebook is a skill file rather than a string in here, so one file drives
# this audit and any editor session that invokes the skill.
skill="${AUDIT_SKILL:-$HOME/.agents/skills/ponytail-audit/SKILL.md}"

if [ ! -f "$skill" ]; then
	echo "audit: skipped: no audit instructions at $skill (set AUDIT_SKILL)"

	exit 0
fi

prompt="${AUDIT_PROMPT:-$(cat "$skill")

Audit the repository in the current directory: walk it with the Read, Grep and Glob tools, ignore .git/, vendor/, node_modules/ and generated or lock files, and never modify a file.

Write the report as markdown: one level-2 heading, then the findings in that one-line-per-finding format, then the net: line. No preamble, no bullet points, no bold, and nothing else.}"

mkdir -p "$(dirname "$out")"

tmp="$out.tmp"

trap 'rm -f "$tmp"' EXIT

# The header records which revision the report describes, because the agent reads
# the working tree and the file outlives the run.
{
	printf '# Over-engineering audit\n\n'
	printf 'Commit %s on %s. Advisory: the agent has read-only tools and changed nothing.\n\n' \
		"$(git rev-parse --short HEAD)" "$(date +%Y-%m-%d)"
} >"$tmp"

echo "audit: agent auditing the tree, report -> $out"

# A command that fails must show why, so its partial output is kept and printed.
if ! printf '%s\n' "$prompt" | "$@" >>"$tmp"; then
	echo "audit: the audit command failed" >&2

	cat "$tmp" >&2

	exit 1
fi

mv "$tmp" "$out"

echo "audit: wrote $out"
