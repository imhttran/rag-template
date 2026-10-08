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
# Usage:
#   scripts/eval-model-sweep.sh 'nomic-embed-text:768' 3
#   scripts/eval-model-sweep.sh 'nomic-embed-text:768,embeddinggemma:768' 3
#
# Environment:
#   SWEEP_DATASET              held-out evaluation split (default evals/retrieval-vi-en-expanded.json)
#   SWEEP_CALIBRATION_DATASET  calibration split       (default evals/retrieval-vi-en-calibration.json)
#   SWEEP_MANIFEST             corpus manifest         (default evals/corpus-vi-en.json)
#   SWEEP_GRID                 floor sweep grid        (default 0.30,0.40,0.50,0.60,0.70)
#   SWEEP_MIN_REJECTION        minimum calibration rejection a floor must reach (default 1)
#   SWEEP_DB_URL               base DATABASE_URL       (default postgres://rag:rag@127.0.0.1:5433/rag?sslmode=disable);
#                              model N uses database rag_<N> on the same server
#   SWEEP_RESULTS              results file            (default docs/experiments-eval-sweep.md)
#   SWEEP_SKIP_INGEST          1 to reuse already-ingested databases (one ingest per
#                              model, no fresh ingest per repeat)
#   SWEEP_EMBED_PROBE          1 to measure the Ollama /api/embed endpoint latency (opt-in)
#   SWEEP_KEEP_DB              1 to leave the per-model databases running for inspection
#   SWEEP_KEEP_UP              1 to leave the shared PostgreSQL instance running

set -eu

MODELS=${1:-"nomic-embed-text:768"}
REPEATS=${2:-3}

DATASET=${SWEEP_DATASET:-evals/retrieval-vi-en-expanded.json}
CALIBRATION_DATASET=${SWEEP_CALIBRATION_DATASET:-evals/retrieval-vi-en-calibration.json}
MANIFEST=${SWEEP_MANIFEST:-evals/corpus-vi-en.json}
GRID=${SWEEP_GRID:-0.30,0.40,0.50,0.60,0.70}
MIN_REJECTION=${SWEEP_MIN_REJECTION:-1}
DB_URL_BASE=${SWEEP_DB_URL:-postgres://rag:rag@127.0.0.1:5433/rag?sslmode=disable}
RESULTS=${SWEEP_RESULTS:-docs/experiments-eval-sweep.md}
SKIP_INGEST=${SWEEP_SKIP_INGEST:-0}
EMBED_PROBE=${SWEEP_EMBED_PROBE:-0}
KEEP_DB=${SWEEP_KEEP_DB:-0}
KEEP_UP=${SWEEP_KEEP_UP:-0}

# One fixed representative chunk for the opt-in embedding endpoint probe. It has
# no double quotes so it can be embedded in a JSON payload safely.
PROBE_TEXT="Payments are applied first to outstanding interest, then to principal, unless otherwise required by the loan agreement."

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

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

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
		/usr/bin/time -v "$@" >"$out" 2>"$work/time.txt" || true

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
	else
		start=$(date +%s)
		"$@" >"$out" 2>&1 || true
		end=$(date +%s)

		printf '%d 0\n' "$((end - start))"
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
		echo "- Sweep grid: \`$GRID\`; repeats per model: \`$REPEATS\`; minimum calibration rejection: \`$MIN_REJECTION\`"
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
# the schema to it, so each ingest starts from an empty vector index.
fresh_database() {
	name=$1
	url=$2

	if ! command -v psql >/dev/null 2>&1; then
		echo "eval-model-sweep: psql not found; cannot isolate database $name" >&2
		exit 3
	fi

	admin_url=$(db_admin_url)

	psql "$admin_url" -v ON_ERROR_STOP=1 \
		-c "DROP DATABASE IF EXISTS $name" >/dev/null 2>&1 || true

	if ! psql "$admin_url" -v ON_ERROR_STOP=1 \
		-c "CREATE DATABASE $name" >/dev/null 2>&1; then
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
run_eval() {
	url=$1
	model=$2
	dim=$3
	floor=$4
	dataset=$5
	out=$6

	env DATABASE_URL="$url" OLLAMA_EMBED_MODEL="$model" EMBED_DIM="$dim" \
		MIN_SIMILARITY="$floor" EVAL_DATASET="$dataset" go run ./cmd/eval >"$out" 2>&1 || true
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

	db_name="rag_${model_index}"
	db_url=$(db_url_for "$db_name")
	model_index=$((model_index + 1))

	echo
	echo "== model: $embed_model (dim $embed_dim, version $version, isolated database $db_name) =="

	# Fresh per-model database so no two models share a vector index.
	fresh_database "$db_name" "$db_url"

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

	if [ -z "$best_floor" ]; then
		best_floor=$(printf '%s' "$GRID" | cut -d',' -f1)
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
			fresh_database "$db_name" "$db_url"
			ingest_corpus "$db_url" "$embed_model" "$embed_dim"
		fi

		run=$(peak_rss "$work/run.out" \
			env DATABASE_URL="$db_url" OLLAMA_EMBED_MODEL="$embed_model" \
			EMBED_DIM="$embed_dim" MIN_SIMILARITY="$best_floor" \
			EVAL_DATASET="$DATASET" go run ./cmd/eval)
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

	# Drop the isolated database unless the operator asked to inspect it.
	if [ "$KEEP_DB" != 1 ]; then
		if command -v psql >/dev/null 2>&1; then
			psql "$(db_admin_url)" -v ON_ERROR_STOP=1 \
				-c "DROP DATABASE IF EXISTS $db_name" >/dev/null 2>&1 || true
		fi
	fi
done

if [ "$KEEP_UP" != 1 ]; then
	make db-down >/dev/null 2>&1 || true
fi

echo
echo "Results recorded in $RESULTS"
