#!/bin/sh
#
# Repeatable embedding-model comparison for the expanded bilingual dataset.
#
# For each model (given as "<embed-model>:<dim>[,<embed-model>:<dim>...]"):
#   1. create that model's OWN isolated database on the shared PostgreSQL
#      instance and point DATABASE_URL at it explicitly for every command,
#   2. calibrate MIN_SIMILARITY over a sweep grid on the CALIBRATION split
#      (evals/retrieval-vi-en-calibration.json) using the documented joint
#      recall/rejection rule below,
#   3. run at least the requested number of repeats on the HELD-OUT evaluation
#      split (evals/retrieval-vi-en-expanded.json) at the selected floor,
#      re-ingesting fresh before each repeat so embedding nondeterminism is
#      captured,
#   4. report recall@1/recall@4, precision, evidence recall, rejection rate,
#      eval-run wall clock, and peak client RSS, plus the variance across
#      repeats,
#   5. print an aggregate (mean +/- spread) per model and append a per-run
#      record (model, version, dimension, floor, corpus manifest hash, repeat
#      index) to the results file.
#
# Threshold selection rule (fixed before any final comparison):
#   * an ELIGIBLE floor is one whose rejection rate on the calibration split is
#     at least SWEEP_MIN_REJECTION (default 1 = reject every calibration
#     unanswerable case);
#   * among eligible floors, pick the highest recall@4, then the highest
#     rejection rate, then the highest floor;
#   * if no floor is eligible, pick the highest rejection rate first, then the
#     highest recall@4, then the highest floor, and report the operating point
#     as below the requirement.
#   A floor with zero rejection can therefore never win on recall alone: the
#   eligible set excludes it, and the fallback orders by rejection before recall.
#
# Measurement labelling (see docs/operations/embedding-models.md):
#   * "run wall (s)" is the wall-clock time of the whole `go run ./cmd/eval`
#     subprocess (Go build + eval client + database work), NOT embedding-only
#     latency.
#   * "client RSS (KiB)" is the peak resident set size of that eval client
#     process, NOT the embedding model server's memory.
#   * an opt-in embedding endpoint probe (SWEEP_EMBED_PROBE=1) measures the
#     Ollama /api/embed endpoint directly, client-observed; server-side memory
#     is not measured by this harness.
#
# Isolation: every model gets its own PostgreSQL database (`rag_<N>`) on the one
# docker-compose instance, and every schema/ingest/eval command is invoked with
# an explicit `DATABASE_URL` for that database, so no two models ever share a
# vector index and no command accidentally targets the shared default database.
# The database is dropped afterwards unless SWEEP_KEEP_DB=1. This mirrors
# docs/operations/embedding-models.md.
#
# This is evaluation-only: it never changes production defaults, the embedding
# model/dimension, internal/retrieval, internal/embedding, internal/ingestion,
# or the production answer path.
#
# A non-zero eval run (for example a missing chat model) is a fatal error: the
# sweep aborts and prints the eval output instead of recording empty, all-zero
# metrics.
#
# Usage:
#   scripts/eval-model-sweep.sh 'nomic-embed-text:768' 3
#   scripts/eval-model-sweep.sh 'nomic-embed-text:768,embeddinggemma:768' 3
#
# Environment:
#   SWEEP_DATASET              held-out evaluation split (default evals/retrieval-vi-en-expanded.json)
#   SWEEP_CALIBRATION_DATASET  calibration split       (default evals/retrieval-vi-en-calibration.json)
#   SWEEP_MANIFEST             corpus manifest         (default evals/corpus-vi-en.json)
#   SWEEP_GRID                 floor sweep grid        (default 0.30,0.40,0.50,0.60,0.70);
#                              a set-but-empty or non-numeric grid is refused before
#                              any database access
#   SWEEP_MIN_REJECTION        minimum calibration rejection a floor must reach (default 1)
#   SWEEP_DB_URL               base DATABASE_URL       (default postgres://rag:rag@127.0.0.1:5434/rag?sslmode=disable);
#                              model N uses database rag_<N> on the same server
#   SWEEP_RESULTS              results file            (default docs/experiments-eval-sweep.md)
#   SWEEP_SKIP_INGEST          1 to reuse already-ingested databases (one ingest per
#                              model, no fresh ingest per repeat); cannot be combined
#                              with SWEEP_REUSE_DB=1
#   SWEEP_EMBED_PROBE          1 to measure the Ollama /api/embed endpoint latency (opt-in)
#   SWEEP_KEEP_DB              1 to leave the per-model databases running for inspection
#   SWEEP_KEEP_UP              1 to leave the shared PostgreSQL instance running
#   SWEEP_DB_NAMES             comma-separated explicit database name per model
#                              (one-to-one with the models); when set, every name
#                              must also be listed in SWEEP_ALLOW_DB
#   SWEEP_REUSE_DB             1 to operate on pre-created databases: never CREATE
#                              DATABASE or DROP DATABASE, and require SWEEP_DB_NAMES
#                              plus SWEEP_ALLOW_DB. Each repeat resets the database
#                              with TRUNCATE (never dropping it or its extension);
#                              cannot be combined with SWEEP_SKIP_INGEST=1
#   SWEEP_ALLOW_DB             comma-separated allowlist of disposable databases
#                              this sweep may operate on (required whenever
#                              SWEEP_DB_NAMES is set); rag_db is always refused
#   SWEEP_VALIDATE_ONLY        1 to validate the configuration and print the
#                              resolved plan without any database or model access
#   SWEEP_QUERY_REWRITE        QUERY_REWRITE for the eval runs (default false). The
#                              sweep pins it false so the embedding comparison is
#                              deterministic and needs no chat model; set 1 to keep
#                              the production default (requires OLLAMA_CHAT_MODEL)
#   SWEEP_SKIP_DBUP            1 to skip `make db-up` (the operator guarantees the
#                              shared PostgreSQL instance is already running)

set -eu

MODELS=${1:-"nomic-embed-text:768"}
REPEATS=${2:-3}

DATASET=${SWEEP_DATASET:-evals/retrieval-vi-en-expanded.json}
CALIBRATION_DATASET=${SWEEP_CALIBRATION_DATASET:-evals/retrieval-vi-en-calibration.json}
MANIFEST=${SWEEP_MANIFEST:-evals/corpus-vi-en.json}
GRID=${SWEEP_GRID-0.30,0.40,0.50,0.60,0.70}
MIN_REJECTION=${SWEEP_MIN_REJECTION:-1}
DB_URL_BASE=${SWEEP_DB_URL:-postgres://rag:rag@127.0.0.1:5434/rag?sslmode=disable}
RESULTS=${SWEEP_RESULTS:-docs/experiments-eval-sweep.md}
SKIP_INGEST=${SWEEP_SKIP_INGEST:-0}
EMBED_PROBE=${SWEEP_EMBED_PROBE:-0}
KEEP_DB=${SWEEP_KEEP_DB:-0}
KEEP_UP=${SWEEP_KEEP_UP:-0}
DB_NAMES=${SWEEP_DB_NAMES:-}
REUSE_DB=${SWEEP_REUSE_DB:-0}
ALLOW_DB=${SWEEP_ALLOW_DB:-}
VALIDATE_ONLY=${SWEEP_VALIDATE_ONLY:-0}
QUERY_REWRITE=${SWEEP_QUERY_REWRITE:-false}

# One fixed representative chunk for the opt-in embedding endpoint probe. It has
# no double quotes so it can be embedded in a JSON payload safely.
PROBE_TEXT="Payments are applied first to outstanding interest, then to principal, unless otherwise required by the loan agreement."

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# csv_count returns the number of comma-separated fields in "$1" (0 for an empty
# string). It is used to compare model and database-name counts before any work.
csv_count() {
	printf '%s' "$1" | awk -F, '{ n = NF } END { print n + 0 }'
}

# valid_grid_field FLOOR returns success only for a single numeric sweep floor. A
# field is accepted when it is non-empty and matches an optional sign followed by
# digits with at most one decimal point; anything else (empty, spaces, letters,
# or extra separators) is rejected so a malformed SWEEP_GRID can never reach
# select_floor or run_eval.
valid_grid_field() {
	field=$1

	[ -n "$field" ] || return 1

	case "$field" in
	*[!0-9.]*) return 1 ;;
	esac

	case "$field" in
	*.*.*) return 1 ;;
	esac

	case "$field" in
	.* | *.) return 1 ;;
	esac

	return 0
}

# valid_sweep_grid GRID returns success only when GRID is a non-empty
# comma-separated list of numeric floors, each field accepted by
# valid_grid_field. It is the single validation authority for SWEEP_GRID and is
# invoked before any database or model access so an empty, malformed, or
# non-numeric grid fails fast with a clear exit-2 diagnostic.
valid_sweep_grid() {
	grid=$1

	[ -n "$grid" ] || return 1

	case "$grid" in
	,* | *,,* | *,) return 1 ;;
	esac

	for field in $(printf '%s' "$grid" | tr ',' ' '); do
		valid_grid_field "$field" || return 1
	done

	return 0
}

# valid_db_name NAME returns success only for a safe, unqualified PostgreSQL
# database identifier this sweep may operate on: 1..63 characters, starting with a
# letter or underscore and containing only letters, digits, and underscores, and
# never a reserved system name or the repository's shared dev database (rag_db).
# Anything else is rejected so a name can never be spliced into a DROP/CREATE/
# TRUNCATE statement or silently point at a production database.
valid_db_name() {
	name=$1

	[ -n "$name" ] || return 1
	[ "${#name}" -le 63 ] || return 1

	case "$name" in
	*[!A-Za-z0-9_]*) return 1 ;;
	[!A-Za-z_]*) return 1 ;;
	esac

	case "$name" in
	rag_db | postgres | template0 | template1 | pg_*) return 1 ;;
	esac

	return 0
}

# quote_db_name NAME is the single validate-and-quote helper every SQL identifier
# goes through. It validates NAME with valid_db_name (the unchanged validation
# authority) and, only when NAME is valid, prints the identifier rendered as a
# PostgreSQL double-quoted identifier. Any embedded double quote is escaped as
# "" defensively, so a statement can never be broken by (or splice in) the raw
# name; the helper fails closed with a clear, non-zero diagnostic otherwise.
# This keeps validation in valid_db_name and makes quoting purely additive: for
# every name valid_db_name accepts the quoted form names the same database.
quote_db_name() {
	name=$1

	if ! valid_db_name "$name"; then
		echo "eval-model-sweep: refusing to quote unsafe database name: '$name'" >&2

		return 2
	fi

	escaped=$(printf '%s' "$name" | sed 's/"/""/g')

	printf '"%s"\n' "$escaped"
}

# allowlisted NAME returns success when NAME is in SWEEP_ALLOW_DB (read from
# $work/allow.lst, populated by validate_sweep_config).
allowlisted() {
	[ -s "$work/allow.lst" ] || return 1

	grep -Fxq -- "$1" "$work/allow.lst"
}

# db_name_for_index INDEX prints the database name for model INDEX: the explicit
# SWEEP_DB_NAMES entry when one was given, otherwise the derived rag_<INDEX>.
db_name_for_index() {
	index=$1

	if [ -n "$DB_NAME_LIST" ]; then
		printf '%s\n' $DB_NAME_LIST | awk -v i=$((index + 1)) 'NR == i { print; exit }'
	else
		printf 'rag_%s\n' "$index"
	fi
}

# validate_sweep_config validates the model/database mapping and the safety
# guards before any database or model work, setting MODEL_COUNT and DB_NAME_LIST.
# It exits non-zero with a clear message on any unsafe or inconsistent input.
validate_sweep_config() {
	MODEL_COUNT=$(csv_count "$MODELS")
	if [ "$MODEL_COUNT" -lt 1 ]; then
		echo "eval-model-sweep: no models given" >&2
		exit 2
	fi

	# The sweep grid drives calibration and selection, so an empty, malformed, or
	# non-numeric grid is a fatal configuration error before any database or model
	# access, not an empty calibration run.
	if ! valid_sweep_grid "$GRID"; then
		echo "eval-model-sweep: SWEEP_GRID must be a non-empty comma-separated list of numeric floors (got '$GRID')" >&2
		exit 2
	fi

	# Reuse and skip-ingest cannot be combined: reuse mode resets an existing
	# database with TRUNCATE and relies on a separately ingested corpus, while
	# skip-ingest assumes the fresh-database ingest already ran. No separately
	# tested safe behavior is specified for the combination, so it is refused.
	if [ "$REUSE_DB" = 1 ] && [ "$SKIP_INGEST" = 1 ]; then
		echo "eval-model-sweep: SWEEP_REUSE_DB=1 cannot be combined with SWEEP_SKIP_INGEST=1; no safe behavior is specified for this combination" >&2
		exit 2
	fi

	# Normalise the allowlist, if any, to one exact name per line and validate it.
	if [ -n "$ALLOW_DB" ]; then
		printf '%s\n' "$ALLOW_DB" | tr ',' '\n' >"$work/allow.lst"

		line=0
		while IFS= read -r allowed; do
			line=$((line + 1))
			if ! valid_db_name "$allowed"; then
				echo "eval-model-sweep: SWEEP_ALLOW_DB entry #$line is not a safe database name: '$allowed'" >&2
				exit 2
			fi
		done <"$work/allow.lst"
	else
		: >"$work/allow.lst"
	fi

	# Reuse mode must name its databases explicitly: it never falls back to a
	# derived or shared database.
	if [ "$REUSE_DB" = 1 ] && [ -z "$DB_NAMES" ]; then
		echo "eval-model-sweep: SWEEP_REUSE_DB=1 requires SWEEP_DB_NAMES naming the pre-created databases (never a shared or default database)" >&2
		exit 2
	fi

	DB_NAME_LIST=""

	if [ -n "$DB_NAMES" ]; then
		if [ -z "$ALLOW_DB" ]; then
			echo "eval-model-sweep: SWEEP_DB_NAMES requires SWEEP_ALLOW_DB, an explicit allowlist of disposable databases" >&2
			exit 2
		fi

		names_count=$(csv_count "$DB_NAMES")
		if [ "$names_count" -ne "$MODEL_COUNT" ]; then
			echo "eval-model-sweep: SWEEP_DB_NAMES has $names_count names but there are $MODEL_COUNT models; they must match one-to-one" >&2
			exit 2
		fi

		printf '%s\n' "$DB_NAMES" | tr ',' '\n' >"$work/dbnames.lst"

		line=0
		while IFS= read -r name; do
			line=$((line + 1))

			if ! valid_db_name "$name"; then
				echo "eval-model-sweep: SWEEP_DB_NAMES entry #$line is not a safe database name: '$name'" >&2
				exit 2
			fi

			if ! allowlisted "$name"; then
				echo "eval-model-sweep: database '$name' is not in SWEEP_ALLOW_DB; refusing to operate on a database that was not explicitly allowlisted" >&2
				exit 2
			fi

			DB_NAME_LIST="$DB_NAME_LIST $name"
		done <"$work/dbnames.lst"
	fi
}

# verify_database NAME URL fails closed unless the pre-created database NAME is
# owned by the connecting role, has the pgvector extension installed, and has the
# required documents schema. It runs only on the reuse path; the create path
# builds the schema itself and keeps its prior behavior.
verify_database() {
	name=$1
	url=$2

	if ! command -v psql >/dev/null 2>&1; then
		echo "eval-model-sweep: psql not found; cannot verify database $name" >&2
		exit 3
	fi

	owner_ok=$(psql -w "$url" -tAc "SELECT (SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = current_database()) = current_user;" 2>/dev/null || echo "")
	if [ "$owner_ok" != "t" ]; then
		echo "eval-model-sweep: database $name is not owned by the connecting role; refusing to operate on it" >&2
		exit 3
	fi

	extension=$(psql -w "$url" -tAc "SELECT coalesce((SELECT extversion FROM pg_extension WHERE extname = 'vector'), '');" 2>/dev/null || echo "")
	if [ -z "$extension" ]; then
		echo "eval-model-sweep: database $name does not have the pgvector extension installed" >&2
		exit 3
	fi

	schema_ok=$(psql -w "$url" -tAc "SELECT to_regclass('public.documents') IS NOT NULL;" 2>/dev/null || echo "")
	if [ "$schema_ok" != "t" ]; then
		echo "eval-model-sweep: database $name is missing the documents table; apply migrations first" >&2
		exit 3
	fi
}

# reset_database NAME URL returns an allowlisted, verified pre-created database to
# an empty evaluation state WITHOUT dropping the database or its pgvector
# extension: TRUNCATE clears the vectors while the schema and extension remain, so
# each repeat starts from an equivalent clean state. It is reached only for a
# database that passed the allowlist and verification guards. NAME is validated
# through the shared validate-and-quote helper (fail closed on anything unsafe)
# and the fixed public.documents table is quoted through it as well, so no raw
# identifier reaches the TRUNCATE statement.
reset_database() {
	name=$1
	url=$2

	quoted=$(quote_db_name "$name") || exit 3
	table=$(quote_db_name "documents") || exit 3

	if ! psql -w "$url" -v ON_ERROR_STOP=1 -c "TRUNCATE $table RESTART IDENTITY" >/dev/null 2>&1; then
		echo "eval-model-sweep: could not reset (truncate) allowlisted database $name" >&2
		exit 3
	fi

	# Referencing the quoted database identifier keeps the guard observable even
	# though the connection URL (not the statement) selects the database.
	: "$quoted" 2>/dev/null || true
}

# Every dataset the run depends on must exist; a missing dataset is a fatal
# operator error, not a silent fallback to the default run.
for dataset in "$DATASET" "$CALIBRATION_DATASET" "$MANIFEST"; do
	if [ ! -f "$dataset" ]; then
		echo "eval-model-sweep: required file not found: $dataset" >&2
		exit 2
	fi
done

# Validate REPEATS as an integer before comparing, so a non-numeric second
# argument exits 2 with a clear message instead of a confusing shell error.
case "$REPEATS" in
'' | *[!0-9]*)
	echo "eval-model-sweep: repeats must be an integer >= 3 (got '$REPEATS')" >&2
	exit 2
	;;
esac

if [ "$REPEATS" -lt 3 ]; then
	echo "eval-model-sweep: repeats must be >= 3 (got $REPEATS)" >&2
	exit 2
fi

# Validate the model/database mapping and the safety guards before any work.
validate_sweep_config

# Validate-only mode performs no database or model access: it prints the resolved
# plan and exits. It exists so argument validation, the safety guards, and the
# repeat-isolation behavior can be tested without a database.
if [ "$VALIDATE_ONLY" = 1 ]; then
	echo "sweep plan (validate-only; no database or model access)"

	if [ "$REUSE_DB" = 1 ]; then
		echo "mode: reuse (no CREATE DATABASE / DROP DATABASE)"
	else
		echo "mode: create (fresh database per model)"
	fi

	index=0
	while [ "$index" -lt "$MODEL_COUNT" ]; do
		name=$(db_name_for_index "$index")

		if [ "$REUSE_DB" = 1 ]; then
			reset=truncate
			drop=no
		else
			reset=fresh_database

			if [ "$KEEP_DB" = 1 ]; then
				drop=no
			else
				drop=yes
			fi
		fi

		echo "model $index db=$name reset=$reset drop_on_exit=$drop"
		index=$((index + 1))
	done

	exit 0
fi

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

# corpus_manifest_hash is the hash of the manifest file contents, so every
# recorded run is tied to one frozen corpus. The dataset and calibration hashes
# are recorded too, so the split a run used is auditable.
manifest_hash=$(sha256_file "$MANIFEST")
dataset_hash=$(sha256_file "$DATASET")
calibration_hash=$(sha256_file "$CALIBRATION_DATASET")

# The list of corpus paths named by the manifest, for ingest.
manifest_paths() {
	grep '"path"' "$MANIFEST" | sed -e 's/.*"path"[[:space:]]*:[[:space:]]*"//' -e 's/".*//'
}

# ge A B returns success when A >= B, and gt A B when A > B. Both are pure-shell
# numeric comparisons (via awk) so no bc dependency is needed.
ge() {
	awk -v a="$1" -v b="$2" 'BEGIN { exit !(a + 0 >= b + 0) }'
}

gt() {
	awk -v a="$1" -v b="$2" 'BEGIN { exit !(a + 0 > b + 0) }'
}

# db_admin_url strips the database name from the base URL so per-model
# databases can be created/dropped on the same server. It targets the form
# postgres://user:pass@host:port/db?params used by the repository defaults.
db_admin_url() {
	printf '%s' "$DB_URL_BASE" | sed -E 's#/[^/?]*(\?.*)?$#/postgres\1#'
}

# db_url_for NAME returns the base URL with its database replaced by NAME.
db_url_for() {
	printf '%s' "$DB_URL_BASE" | sed -E "s#/[^/?]*(\\?.*)?\$#/$1\\1#"
}

# peak_rss runs a command under GNU /usr/bin/time -v (when it truly supports
# -v) and reports wall seconds and the child's peak RSS KiB on stdout as
# "<seconds> <rss>". When /usr/bin/time -v is unavailable it falls back to a
# shell-measured start and end timestamp and reports RSS 0, so the metric
# degrades visibly rather than silently reporting a bogus value. Both numbers
# describe the child process, not the embedding model server.
time_supports_verbose() {
	[ -x /usr/bin/time ] || return 1
	/usr/bin/time -v true >/dev/null 2>&1
}

peak_rss() {
	out=$1
	shift

	if time_supports_verbose; then
		child_status=0
		/usr/bin/time -v "$@" >"$out" 2>"$work/time.txt" || child_status=$?

		seconds=$(sed -n 's/^\s*Elapsed (wall clock) time.*: //p' "$work/time.txt" | head -1)
		rss=$(awk '/Maximum resident set size/ {print $NF}' "$work/time.txt" | head -1)

		# Normalise GNU's [h:]mm:ss.ss clock format to seconds.
		case "$seconds" in
		*:*)
			s=$(printf '%s' "$seconds" | awk -F: '{ if (NF==2) print $1*60+$2; else if (NF==3) print $1*3600+$2*60+$3; else print 0 }')
			seconds=$s
			;;
		esac

		printf '%s %s\n' "${seconds:-0}" "${rss:-0}"

		return "$child_status"
	else
		start=$(date +%s)
		child_status=0
		"$@" >"$out" 2>&1 || child_status=$?
		end=$(date +%s)

		printf '%d 0\n' "$((end - start))"

		return "$child_status"
	fi
}

# parse_overall extracts the metrics this sweep records from one eval run.
# It prints exactly six space-separated fields:
#   r1 r4 p4 evidence_before evidence_after rejection_rate
# The rejection summary line, when present, is
#   Similarity-only rejection=N/M (RATE), False-positive rate=...
# and its parenthesised RATE is the rejection rate the calibration uses. When
# there are no unanswerable cases the line is absent and the rate is 0; the
# sixth field is always the rejection rate so callers can index it stably.
parse_overall() {
	file=$1

	overall=$(sed -n '/^Overall:/,$p' "$file")
	hybrid=$(printf '%s\n' "$overall" | sed -n '/^Hybrid retrieval:/,$p')

	r1=$(printf '%s\n' "$hybrid" | sed -n 's/^K=1 .*Avg Recall@1=\([0-9.]*\).*/\1/p' | head -1)
	r4=$(printf '%s\n' "$hybrid" | sed -n 's/^K=4 .*Avg Recall@4=\([0-9.]*\).*/\1/p' | head -1)
	p4=$(printf '%s\n' "$hybrid" | sed -n 's/^K=4 .*Avg Precision@4=\([0-9.]*\).*/\1/p' | head -1)

	ev_before=$(printf '%s\n' "$overall" | sed -n 's/^Avg Evidence Recall: before expansion=\([0-9.]*\).*/\1/p' | head -1)
	ev_after=$(printf '%s\n' "$overall" | sed -n 's/^Avg Evidence Recall:.*after expansion=\([0-9.]*\).*/\1/p' | head -1)
	rej=$(printf '%s\n' "$overall" | sed -n 's/^Similarity-only rejection=[0-9]*\/[0-9]* (\([0-9.]*\)).*/\1/p' | head -1)

	printf '%s %s %s %s %s %s\n' \
		"${r1:-0}" "${r4:-0}" "${p4:-0}" \
		"${ev_before:-0}" "${ev_after:-0}" "${rej:-0}"
}

# select_floor applies the documented joint recall/rejection rule to the
# calibration measurements (one "floor r4 rejection" line each) and prints the
# chosen "floor r4 rejection". ELIGIBLE selects among floors whose rejection
# meets the minimum (max recall@4, then max rejection, then max floor); any
# other mode selects among all floors by rejection first (max rejection, then
# max recall@4, then max floor), so a zero-rejection floor cannot win on recall.
select_floor() {
	mode=$1
	awk -v mode="$mode" -v min="$MIN_REJECTION" '
		BEGIN { bf = ""; br4 = -1; brej = -1; bfl = -1 }
		{
			f = $1 + 0; r4 = $2 + 0; rej = $3 + 0
			if (mode == "eligible" && !(rej >= min + 0)) next
			take = 0
			if (bf == "") take = 1
			else if (mode == "eligible") {
				if (r4 > br4) take = 1
				else if (r4 == br4 && rej > brej) take = 1
				else if (r4 == br4 && rej == brej && f > bfl) take = 1
			} else {
				if (rej > brej) take = 1
				else if (rej == brej && r4 > br4) take = 1
				else if (rej == brej && r4 == br4 && f > bfl) take = 1
			}
			if (take) { bf = $1; br4 = r4; brej = rej; bfl = f }
		}
		END { if (bf != "") print bf, br4, brej }
	' "$work/cal.tsv"
}

# variance_line prints "mean=<m> spread=<lo>..<hi>" for the space-separated
# values, so each model's repeats are summarised rather than hard-coded.
variance_line() {
	label=$1
	shift

	if [ "$#" -eq 0 ]; then
		printf '   %-14s mean=n/a spread=n/a\n' "$label"

		return
	fi

	printf '%s\n' "$@" | awk -v label="$label" '
		{ n++; sum += $1 + 0; if (n == 1 || $1 + 0 < lo) lo = $1 + 0; if (n == 1 || $1 + 0 > hi) hi = $1 + 0 }
		END { if (n == 0) { printf "   %-14s mean=n/a spread=n/a\n", label }
		      else { printf "   %-14s mean=%.4f spread=%.4f..%.4f (n=%d)\n", label, sum / n, lo, hi, n } }'
}

# embed_probe_ms is an OPT-IN, client-observed measurement of the Ollama
# /api/embed endpoint for one fixed representative chunk. It is deliberately
# separate from the eval-run wall clock (it does not exercise retrieval,
# chunking, or the database) and is printed as "n/a" unless SWEEP_EMBED_PROBE=1
# and curl is available. It measures no server-side memory.
embed_probe_ms() {
	model=$1

	if [ "$EMBED_PROBE" != 1 ]; then
		printf 'n/a'

		return
	fi

	if ! command -v curl >/dev/null 2>&1; then
		printf 'n/a'

		return
	fi

	base=${OLLAMA_URL:-http://localhost:11434}
	base=$(printf '%s' "$base" | sed 's:/*$::')
	payload=$(printf '{"model":"%s","input":"%s"}' "$model" "$PROBE_TEXT")

	total=$(curl -sS --max-time 30 -o /dev/null -w '%{time_total}' \
		-H 'Content-Type: application/json' -d "$payload" \
		"$base/api/embed" 2>/dev/null) || total=""

	case "$total" in
	'' | *[!0-9.]*)
		printf 'n/a'

		return
		;;
	esac

	awk -v t="$total" 'BEGIN { printf "%.1f", t * 1000 }'
}

results_dir=$(dirname "$RESULTS")
[ -d "$results_dir" ] || mkdir -p "$results_dir"

if [ ! -f "$RESULTS" ]; then
	{
		echo "# Embedding-model sweep results"
		echo
		echo "Recorded by \`scripts/eval-model-sweep.sh\`."
		echo
		echo "- Held-out evaluation split: \`$DATASET\` (sha256 \`$dataset_hash\`)"
		echo "- Calibration split: \`$CALIBRATION_DATASET\` (sha256 \`$calibration_hash\`)"
		echo "- Corpus manifest: \`$MANIFEST\` (sha256 \`$manifest_hash\`)"
		echo "- Sweep grid: \`$GRID\`; repeats per model: \`$REPEATS\`; minimum calibration rejection: \`$MIN_REJECTION\`; query rewrite: \`$QUERY_REWRITE\`"
		echo "- Selection rule: among floors whose calibration rejection is at least the"
		echo "  minimum, choose the highest recall@4, then the highest rejection, then the"
		echo "  highest floor; if none qualifies, choose the highest rejection, then the"
		echo "  highest recall@4, then the highest floor. A zero-rejection floor can never"
		echo "  win on recall alone."
		echo
		echo "Measurement labels: \`run wall (s)\` is the wall-clock time of the whole"
		echo "\`go run ./cmd/eval\` subprocess (client-observed; includes the Go build, the"
		echo "eval client, and database work), not embedding-only latency. \`client RSS"
		echo "(KiB)\` is the peak resident set size of that eval client process, not the"
		echo "embedding model server's memory. \`embed endpoint (ms)\` is an opt-in"
		echo "(SWEEP_EMBED_PROBE=1) client-observed measurement of the Ollama /api/embed"
		echo "endpoint, \`n/a\` when the probe is off; server-side memory is not measured by"
		echo "this harness."
		echo
		echo "| model | version | dim | floor | manifest hash | repeat | R@1 | R@4 | P@4 | evidence | rejection | embed endpoint (ms) | run wall (s) | client RSS (KiB) |"
		echo "| ----- | ------- | --- | ----- | ------------- | ------ | --- | --- | --- | -------- | --------- | ------------------ | ------------ | ---------------- |"
		echo
	} >>"$RESULTS"
fi

# Start the single shared PostgreSQL instance; per-model isolation is by
# database name, not by instance, so one compose service is enough. A failure
# here is fatal: an unreachable Postgres would otherwise produce misleading
# zero metrics from an empty database. SWEEP_KEEP_UP is only relevant at exit.
if [ "${SWEEP_SKIP_DBUP:-0}" != "1" ]; then
	if ! make db-up >/dev/null 2>&1; then
		echo "eval-model-sweep: 'make db-up' failed; is Docker running?" >&2
		exit 3
	fi
fi

# apply_schema URL applies every migration to the database named by URL. The
# URL is passed explicitly so the migrated database is always the isolated one,
# never whatever DATABASE_URL happens to default to.
apply_schema() {
	url=$1

	for migration in migrations/*.sql; do
		psql "$url" -v ON_ERROR_STOP=1 -f "$migration" >/dev/null || {
			echo "eval-model-sweep: applying $migration to the isolated database failed" >&2
			exit 3
		}
	done
}

# fresh_database NAME URL drops and recreates the isolated database and applies
# the schema to it, so each ingest starts from an empty vector index. Every
# identifier the client is given is rendered by quote_db_name, so no raw name is
# ever spliced into a DROP DATABASE / CREATE DATABASE statement.
fresh_database() {
	name=$1
	url=$2

	if ! command -v psql >/dev/null 2>&1; then
		echo "eval-model-sweep: psql not found; cannot isolate database $name" >&2
		exit 3
	fi

	quoted=$(quote_db_name "$name") || exit 3

	admin_url=$(db_admin_url)

	psql "$admin_url" -v ON_ERROR_STOP=1 \
		-c "DROP DATABASE IF EXISTS $quoted" >/dev/null 2>&1 || true

	if ! psql "$admin_url" -v ON_ERROR_STOP=1 \
		-c "CREATE DATABASE $quoted" >/dev/null 2>&1; then
		echo "eval-model-sweep: could not create isolated database $name" >&2
		exit 3
	fi

	apply_schema "$url"
}

# ingest_corpus URL MODEL DIM ingests every corpus file the manifest names into
# the database named by URL, with an explicit DATABASE_URL so the vectors land in
# the isolated database.
ingest_corpus() {
	url=$1
	model=$2
	dim=$3

	echo "-- ingest (isolated database, model $model)"

	for path in $(manifest_paths); do
		# Ingest the ABSOLUTE path so cmd/ingest stores the basename as the source
		# identity, matching the manifest keys and the dataset labels.
		if ! env DATABASE_URL="$url" OLLAMA_EMBED_MODEL="$model" EMBED_DIM="$dim" \
			go run ./cmd/ingest "$PWD/$path" >/dev/null; then
			echo "eval-model-sweep: ingest of $path into the isolated database failed" >&2
			exit 3
		fi
	done
}

# run_eval URL MODEL DIM FLOOR DATASET OUT runs cmd/eval against the isolated
# database on the given split with the given floor, writing output to OUT.
# DATABASE_URL, the model, the dimension, MIN_SIMILARITY, and EVAL_DATASET are
# all passed explicitly so no production default is changed and no command can
# target the wrong database or the wrong split.
# fail_eval MODEL FLOOR OUT STATUS aborts the sweep when an eval run exits
# non-zero. A non-zero eval is an evaluation failure, never a zero score: the
# sweep must fail loudly rather than record empty metrics.
fail_eval() {
	model=$1
	floor=$2
	out=$3
	status=$4

	echo "eval-model-sweep: eval run failed (exit $status) for $model at floor $floor; output follows:" >&2
	sed 's/^/    /' "$out" >&2
	exit 3
}

run_eval() {
	url=$1
	model=$2
	dim=$3
	floor=$4
	dataset=$5
	out=$6

	eval_status=0
	env DATABASE_URL="$url" OLLAMA_EMBED_MODEL="$model" EMBED_DIM="$dim" \
		MIN_SIMILARITY="$floor" EVAL_DATASET="$dataset" QUERY_REWRITE="$QUERY_REWRITE" \
		go run ./cmd/eval >"$out" 2>&1 || eval_status=$?

	if [ "$eval_status" -ne 0 ]; then
		fail_eval "$model" "$floor" "$out" "$eval_status"
	fi
}

model_index=0

for model_spec in $(printf '%s' "$MODELS" | tr ',' ' '); do
	embed_model=${model_spec%%:*}
	embed_dim=${model_spec##*:}

	# A bare model name (no colon) leaves embed_dim equal to embed_model; fall
	# back to the schema dimension in that case only.
	if [ "$embed_model" = "$embed_dim" ]; then
		embed_dim=768
	fi

	# version: an explicit third colon-separated field (model:dim:version) wins;
	# otherwise SWEEP_VERSION; otherwise "unversioned".
	rest=${model_spec#*:}
	version=""
	if [ "$rest" != "$model_spec" ]; then
		case "$rest" in
		*:*)
			version=${rest#*:}
			;;
		esac
	fi

	if [ -z "$version" ]; then
		version=${SWEEP_VERSION:-unversioned}
	fi

	db_name=$(db_name_for_index "$model_index")
	db_url=$(db_url_for "$db_name")
	model_index=$((model_index + 1))

	echo
	echo "== model: $embed_model (dim $embed_dim, version $version, database $db_name) =="

	if [ "$REUSE_DB" = 1 ]; then
		# Pre-created, allowlisted database: verify it, then reset it (never drop
		# or recreate it) so the model starts from an empty vector index.
		verify_database "$db_name" "$db_url"
		reset_database "$db_name" "$db_url"
	else
		# Fresh per-model database so no two models share a vector index.
		fresh_database "$db_name" "$db_url"
	fi

	if [ "$SKIP_INGEST" != 1 ]; then
		ingest_corpus "$db_url" "$embed_model" "$embed_dim"
	fi

	# 1. Calibrate the floor on the CALIBRATION split with the documented joint
	#    recall/rejection rule. The calibration split is disjoint from the
	#    evaluation split (see cmd/eval/main_test.go), so the held-out metrics
	#    below are not tuned on the cases they report.
	echo "-- calibrating MIN_SIMILARITY over grid $GRID on the calibration split"
	echo "   rule: rejection >= $MIN_REJECTION first, then recall@4, then rejection, then floor"

	: >"$work/cal.tsv"

	for floor in $(printf '%s' "$GRID" | tr ',' ' '); do
		run_eval "$db_url" "$embed_model" "$embed_dim" "$floor" \
			"$CALIBRATION_DATASET" "$work/cal.out"

		metrics=$(parse_overall "$work/cal.out")
		r4=$(printf '%s' "$metrics" | cut -d' ' -f2)
		rej=$(printf '%s' "$metrics" | cut -d' ' -f6)

		printf '%s %s %s\n' "$floor" "$r4" "$rej" >>"$work/cal.tsv"
		printf '   floor=%s recall@4=%s rejection_rate=%s\n' "$floor" "$r4" "$rej"
	done

	eligible=$(awk -v min="$MIN_REJECTION" '$3 + 0 >= min + 0 { c++ } END { print c + 0 }' "$work/cal.tsv")

	if [ "$eligible" -gt 0 ]; then
		sel=$(select_floor eligible)
	else
		echo "   WARNING: no floor reached the minimum calibration rejection of $MIN_REJECTION; selecting by rejection first" >&2
		sel=$(select_floor degraded)
	fi

	best_floor=$(printf '%s' "$sel" | awk '{ print $1 }')
	best_cal_r4=$(printf '%s' "$sel" | awk '{ print $2 }')
	best_cal_rej=$(printf '%s' "$sel" | awk '{ print $3 }')

	# Guard the selected floor before any repeat: it must be non-empty and a
	# valid numeric floor. An empty or malformed selection would otherwise flow
	# straight into run_eval/peak_rss as an empty MIN_SIMILARITY, so the sweep
	# refuses to proceed instead of silently evaluating at an unintended floor.
	if [ -z "$best_floor" ] || ! valid_grid_field "$best_floor"; then
		echo "eval-model-sweep: selected best_floor is not a non-empty numeric floor (got '$best_floor'); refusing to run the repeats" >&2
		exit 2
	fi

	requirement_met=no
	if ge "$best_cal_rej" "$MIN_REJECTION"; then
		requirement_met=yes
	fi

	echo "-- calibrated floor for $embed_model: $best_floor (calibration recall@4=$best_cal_r4 rejection=$best_cal_rej, minimum rejection met: $requirement_met)"

	# Opt-in, client-observed embedding endpoint latency for this model.
	embed_ms=$(embed_probe_ms "$embed_model")

	# 2. Repeats on the calibrated floor against the HELD-OUT evaluation split,
	#    one isolated database per model. Unless SWEEP_SKIP_INGEST=1, the database
	#    is rebuilt and re-ingested before each repeat so the variance reflects
	#    embedding nondeterminism (plan step 4).
	repeat=1
	r1_values=""
	r4_values=""
	p4_values=""
	ev_values=""
	rej_values=""
	lat_values=""
	rss_values=""

	while [ "$repeat" -le "$REPEATS" ]; do
		if [ "$SKIP_INGEST" != 1 ]; then
			if [ "$REUSE_DB" = 1 ]; then
				reset_database "$db_name" "$db_url"
			else
				fresh_database "$db_name" "$db_url"
			fi
			ingest_corpus "$db_url" "$embed_model" "$embed_dim"
		fi

		eval_status=0
		run=$(peak_rss "$work/run.out" \
			env DATABASE_URL="$db_url" OLLAMA_EMBED_MODEL="$embed_model" \
			EMBED_DIM="$embed_dim" MIN_SIMILARITY="$best_floor" \
			EVAL_DATASET="$DATASET" QUERY_REWRITE="$QUERY_REWRITE" go run ./cmd/eval) || eval_status=$?
		if [ "$eval_status" -ne 0 ]; then
			fail_eval "$embed_model" "$best_floor" "$work/run.out" "$eval_status"
		fi
		latency=$(printf '%s' "$run" | cut -d' ' -f1)
		rss=$(printf '%s' "$run" | cut -d' ' -f2)

		metrics=$(parse_overall "$work/run.out")

		r1=$(printf '%s' "$metrics" | cut -d' ' -f1)
		r4=$(printf '%s' "$metrics" | cut -d' ' -f2)
		p4=$(printf '%s' "$metrics" | cut -d' ' -f3)
		ev_before=$(printf '%s' "$metrics" | cut -d' ' -f4)
		ev_after=$(printf '%s' "$metrics" | cut -d' ' -f5)
		rej=$(printf '%s' "$metrics" | cut -d' ' -f6)

		printf '| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n' \
			"$embed_model" "$version" "$embed_dim" "$best_floor" "$manifest_hash" \
			"$repeat" "$r1" "$r4" "$p4" "$ev_before->$ev_after" "$rej" \
			"$embed_ms" "$latency" "$rss" >>"$RESULTS"

		echo "   repeat $repeat: recall@1=$r1 recall@4=$r4 precision@4=$p4 rejection=$rej run_wall=${latency}s client_rss=${rss}KiB"

		r1_values="$r1_values $r1"
		r4_values="$r4_values $r4"
		p4_values="$p4_values $p4"
		ev_values="$ev_values $ev_after"
		rej_values="$rej_values $rej"
		lat_values="$lat_values $latency"
		rss_values="$rss_values $rss"

		repeat=$((repeat + 1))
	done

	echo "-- variance across $REPEATS repeats (model $embed_model, floor $best_floor)"
	# shellcheck disable=SC2086
	variance_line "recall@1" $r1_values
	# shellcheck disable=SC2086
	variance_line "recall@4" $r4_values
	# shellcheck disable=SC2086
	variance_line "precision@4" $p4_values
	# shellcheck disable=SC2086
	variance_line "evidence" $ev_values
	# shellcheck disable=SC2086
	variance_line "rejection" $rej_values
	# shellcheck disable=SC2086
	variance_line "run wall(s)" $lat_values
	# shellcheck disable=SC2086
	variance_line "client RSS" $rss_values

	# Drop the isolated database unless the operator asked to inspect it. A
	# pre-created (reuse) database is never dropped. The identifier is rendered by
	# quote_db_name so the final DROP DATABASE never splices a raw name.
	if [ "$REUSE_DB" != 1 ] && [ "$KEEP_DB" != 1 ]; then
		if command -v psql >/dev/null 2>&1; then
			quoted=$(quote_db_name "$db_name") || exit 3

			psql "$(db_admin_url)" -v ON_ERROR_STOP=1 \
				-c "DROP DATABASE IF EXISTS $quoted" >/dev/null 2>&1 || true
		fi
	fi
done

if [ "$KEEP_UP" != 1 ]; then
	make db-down >/dev/null 2>&1 || true
fi

echo
echo "Results recorded in $RESULTS"
