#!/bin/sh
# Runs a project's integration tests against a real dependency.
#
# Project-agnostic: the command and the database URL come from the environment,
# so any repository can call it. Nothing here knows about Go or this project.
#
#   INTEGRATION_TEST_CMD     required  command that runs the tests (run by sh -c)
#   INTEGRATION_TEST_DB      optional  connection URL, probed with psql before the
#                                      tests and exported to them as DATABASE_URL
#   INTEGRATION_TEST_STRICT  optional  fail, instead of skipping, when the
#                                      database cannot be reached
#   INTEGRATION_TEST_PASS    default   pattern that proves a test ran: ^--- PASS
#   INTEGRATION_TEST_HINT    optional  one line to print when it skips
#
# It exits 0 with a note when the database is unreachable (unless STRICT is set),
# and non-zero when the command fails or when nothing ran: a filter that matches
# no test must not look like a pass.

set -e

if [ -z "${INTEGRATION_TEST_CMD:-}" ]; then
	echo "integration: INTEGRATION_TEST_CMD is not set" >&2

	exit 1
fi

skip() {
	echo "integration: skipped: $1"

	if [ -n "${INTEGRATION_TEST_HINT:-}" ]; then
		printf '%s\n' "$INTEGRATION_TEST_HINT"
	fi

	exit 0
}

db="${INTEGRATION_TEST_DB:-}"

if [ -n "$db" ]; then
	if ! command -v psql >/dev/null 2>&1; then
		echo "integration: psql is not on PATH, so $db cannot be checked" >&2

		exit 1
	fi

	if ! PGCONNECT_TIMEOUT=3 psql "$db" -c 'SELECT 1' >/dev/null 2>&1; then
		if [ -n "${INTEGRATION_TEST_STRICT:-}" ]; then
			echo "integration: cannot reach the database at $db" >&2

			exit 1
		fi

		skip "cannot reach the database at $db"
	fi

	DATABASE_URL="$db"
	export DATABASE_URL
fi

# A command that fails must show why, so the output is kept and printed.
failed=""

output=$(sh -c "$INTEGRATION_TEST_CMD" 2>&1) || failed="yes"

if [ -n "$failed" ]; then
	printf '%s\n' "$output"

	exit 1
fi

# A test runner can exit 0 having run nothing -- `go test -run Integration` does
# when the filter matches no test -- so count the passes instead of trusting it.
ran=$(printf '%s\n' "$output" | grep -c -- "${INTEGRATION_TEST_PASS:-^--- PASS}" || true)

if [ "$ran" -eq 0 ]; then
	echo "integration: the command passed but no test ran: check the filter and the test names" >&2

	exit 1
fi

echo "integration: $ran tests passed"
