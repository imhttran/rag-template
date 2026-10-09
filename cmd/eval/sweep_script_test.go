package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests exercise scripts/eval-model-sweep.sh. Most run it in validate-only
// mode (SWEEP_VALIDATE_ONLY=1), which performs argument validation and the safety
// guards and prints the resolved plan without any database or model access. The
// regression test at the end runs the script's create/reuse SQL paths with a
// recording psql shim on PATH so the emitted DROP/CREATE/TRUNCATE statements can
// be asserted without a live PostgreSQL server.

// sweepScript is the evaluation sweep runner, relative to this package's test
// working directory (cmd/eval).
var sweepScript = filepath.Join("..", "..", "scripts", "eval-model-sweep.sh")

const (
	sweepOneModel  = "nomic-embed-text:768"
	sweepTwoModels = "nomic-embed-text:768,embeddinggemma:768"
)

// runSweepValidate runs the sweep runner in validate-only mode with a controlled
// environment and returns the exit code and combined output.
func runSweepValidate(t *testing.T, models string, overrides map[string]string) (int, string) {
	t.Helper()

	script, err := filepath.Abs(sweepScript)
	if err != nil {
		t.Fatalf("resolve sweep script: %v", err)
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}

	cmd := exec.Command("sh", script, models, "3")
	cmd.Dir = repoRoot
	cmd.Env = sweepEnv(overrides)

	out, err := cmd.CombinedOutput()

	code := 0

	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run sweep script: %v", err)
		}

		code = exitErr.ExitCode()
	}

	return code, string(out)
}

// sweepEnv builds the child environment, dropping every SWEEP_* variable,
// DATABASE_URL, and PGCONNECT_TIMEOUT so a test never depends on ambient state,
// then forcing validate-only and appending the overrides.
func sweepEnv(overrides map[string]string) []string {
	var env []string

	for _, kv := range os.Environ() {
		name := kv[:strings.IndexByte(kv, '=')]
		if strings.HasPrefix(name, "SWEEP_") || name == "DATABASE_URL" || name == "PGCONNECT_TIMEOUT" {
			continue
		}

		env = append(env, kv)
	}

	env = append(env, "SWEEP_VALIDATE_ONLY=1")

	for key, value := range overrides {
		env = append(env, key+"="+value)
	}

	return env
}

func requireContains(t *testing.T, out, want string) {
	t.Helper()

	if !strings.Contains(out, want) {
		t.Errorf("output does not contain %q:\n%s", want, out)
	}
}

// TestSweepScriptDefaultPlanCreatesAndDrops pins the unchanged default behavior:
// with the new options unset, each model gets its own derived rag_<N> database
// that is created fresh and dropped on exit.
func TestSweepScriptDefaultPlanCreatesAndDrops(t *testing.T) {
	code, out := runSweepValidate(t, sweepTwoModels, nil)
	if code != 0 {
		t.Fatalf("exit=%d, want 0:\n%s", code, out)
	}

	for _, want := range []string{
		"mode: create",
		"model 0 db=rag_0",
		"model 1 db=rag_1",
		"reset=fresh_database",
		"drop_on_exit=yes",
	} {
		requireContains(t, out, want)
	}
}

// TestSweepScriptReusePlanTruncatesAndNeverDrops proves the reuse path maps each
// model to its explicit pre-created database, resets it with TRUNCATE between
// repeats (repeat isolation without dropping the database or extension), and
// never drops it.
func TestSweepScriptReusePlanTruncatesAndNeverDrops(t *testing.T) {
	code, out := runSweepValidate(t, sweepTwoModels, map[string]string{
		"SWEEP_REUSE_DB": "1",
		"SWEEP_DB_NAMES": "rag_021_nomic,rag_021_gemma",
		"SWEEP_ALLOW_DB": "rag_021_nomic,rag_021_gemma",
	})
	if code != 0 {
		t.Fatalf("exit=%d, want 0:\n%s", code, out)
	}

	for _, want := range []string{
		"mode: reuse",
		"model 0 db=rag_021_nomic",
		"model 1 db=rag_021_gemma",
		"reset=truncate",
		"drop_on_exit=no",
	} {
		requireContains(t, out, want)
	}

	if strings.Contains(out, "reset=fresh_database") {
		t.Errorf("reuse plan must not recreate databases:\n%s", out)
	}

	if strings.Contains(out, "drop_on_exit=yes") {
		t.Errorf("reuse plan must never drop a pre-created database:\n%s", out)
	}
}

// TestSweepScriptRejectsDatabaseNameCountMismatch covers the count guard: the
// database-name list must be one-to-one with the models.
func TestSweepScriptRejectsDatabaseNameCountMismatch(t *testing.T) {
	code, out := runSweepValidate(t, sweepTwoModels, map[string]string{
		"SWEEP_DB_NAMES": "rag_021_nomic",
		"SWEEP_ALLOW_DB": "rag_021_nomic",
	})
	if code == 0 {
		t.Fatalf("expected failure, got success:\n%s", out)
	}

	requireContains(t, out, "must match one-to-one")
}

// TestSweepScriptReuseRequiresExplicitMapping covers the guard that reuse mode
// never falls back to a derived or shared database.
func TestSweepScriptReuseRequiresExplicitMapping(t *testing.T) {
	code, out := runSweepValidate(t, sweepTwoModels, map[string]string{
		"SWEEP_REUSE_DB": "1",
	})
	if code == 0 {
		t.Fatalf("expected failure, got success:\n%s", out)
	}

	requireContains(t, out, "requires SWEEP_DB_NAMES")
}

// TestSweepScriptRequiresAllowlistForExplicitNames covers the guard that an
// explicitly named database must also be allowlisted.
func TestSweepScriptRequiresAllowlistForExplicitNames(t *testing.T) {
	code, out := runSweepValidate(t, sweepTwoModels, map[string]string{
		"SWEEP_DB_NAMES": "rag_021_nomic,rag_021_gemma",
	})
	if code == 0 {
		t.Fatalf("expected failure, got success:\n%s", out)
	}

	requireContains(t, out, "requires SWEEP_ALLOW_DB")
}

// TestSweepScriptRejectsUnlistedDatabase covers the allowlist membership guard.
func TestSweepScriptRejectsUnlistedDatabase(t *testing.T) {
	code, out := runSweepValidate(t, sweepTwoModels, map[string]string{
		"SWEEP_REUSE_DB": "1",
		"SWEEP_DB_NAMES": "rag_021_nomic,rag_021_gemma",
		"SWEEP_ALLOW_DB": "rag_021_nomic,rag_021_test",
	})
	if code == 0 {
		t.Fatalf("expected failure, got success:\n%s", out)
	}

	requireContains(t, out, "not in SWEEP_ALLOW_DB")
}

// TestSweepScriptRejectsUnsafeDatabaseNames covers identifier validation: unsafe
// or reserved names are rejected so a name can never be injected into a
// statement.
func TestSweepScriptRejectsUnsafeDatabaseNames(t *testing.T) {
	for _, name := range []string{
		"bad-name",
		"rag;DROP TABLE documents;--",
		"-leading",
		"has space",
		`quote"x`,
		"rag_db",
	} {
		t.Run(name, func(t *testing.T) {
			code, out := runSweepValidate(t, sweepOneModel, map[string]string{
				"SWEEP_DB_NAMES": name,
				"SWEEP_ALLOW_DB": "rag_021_nomic",
			})
			if code == 0 {
				t.Fatalf("name %q: expected failure, got success:\n%s", name, out)
			}

			requireContains(t, out, "not a safe database name")
		})
	}
}

// TestSweepScriptRejectsUnsafeAllowlist covers the allowlist's own identifier
// validation: a reserved or malformed allowlist entry is refused.
func TestSweepScriptRejectsUnsafeAllowlist(t *testing.T) {
	code, out := runSweepValidate(t, sweepOneModel, map[string]string{
		"SWEEP_DB_NAMES": "rag_021_nomic",
		"SWEEP_ALLOW_DB": "rag_db",
	})
	if code == 0 {
		t.Fatalf("expected failure, got success:\n%s", out)
	}

	requireContains(t, out, "SWEEP_ALLOW_DB entry #1 is not a safe database name")
}

// TestSweepScriptRejectsEmptyGrid covers RAG-022B finding 2: an empty
// SWEEP_GRID is refused (exit 2) by the pre-database grid validation, with a
// clear message, before any database or model access.
func TestSweepScriptRejectsEmptyGrid(t *testing.T) {
	code, out := runSweepValidate(t, sweepOneModel, map[string]string{
		"SWEEP_GRID": "",
	})
	if code != 2 {
		t.Fatalf("exit=%d, want 2:\n%s", code, out)
	}

	requireContains(t, out, "SWEEP_GRID must be a non-empty comma-separated list of numeric floors")
}

// TestSweepScriptRejectsMalformedGrid covers the malformed cases called out by
// the plan: a doubled or trailing separator, and a non-comma separator, are all
// refused with exit 2 and a clear message.
func TestSweepScriptRejectsMalformedGrid(t *testing.T) {
	for _, grid := range []string{
		"0.30,,0.50",
		"0.30;0.40",
		",0.30",
		"0.30,",
	} {
		t.Run(grid, func(t *testing.T) {
			code, out := runSweepValidate(t, sweepOneModel, map[string]string{
				"SWEEP_GRID": grid,
			})
			if code != 2 {
				t.Fatalf("grid %q: exit=%d, want 2:\n%s", grid, code, out)
			}

			requireContains(t, out, "SWEEP_GRID must be a non-empty comma-separated list of numeric floors")
		})
	}
}

// TestSweepScriptRejectsNonNumericGrid covers the non-numeric cases: a fully
// non-numeric grid and a grid with one non-numeric field are both refused with
// exit 2 and a clear message before any database access.
func TestSweepScriptRejectsNonNumericGrid(t *testing.T) {
	for _, grid := range []string{
		"abc",
		"0.30,x",
		"0.30,0.40,oops",
	} {
		t.Run(grid, func(t *testing.T) {
			code, out := runSweepValidate(t, sweepOneModel, map[string]string{
				"SWEEP_GRID": grid,
			})
			if code != 2 {
				t.Fatalf("grid %q: exit=%d, want 2:\n%s", grid, code, out)
			}

			requireContains(t, out, "SWEEP_GRID must be a non-empty comma-separated list of numeric floors")
		})
	}
}

// TestSweepScriptAcceptsValidGrid pins that a valid numeric grid is unchanged:
// the default grid and other valid numeric grids still validate and print the
// create-mode plan.
func TestSweepScriptAcceptsValidGrid(t *testing.T) {
	for _, grid := range []string{
		"0.30,0.40,0.50,0.60,0.70",
		"0.5",
		"0.1,0.9",
	} {
		t.Run(grid, func(t *testing.T) {
			code, out := runSweepValidate(t, sweepTwoModels, map[string]string{
				"SWEEP_GRID": grid,
			})
			if code != 0 {
				t.Fatalf("grid %q: exit=%d, want 0:\n%s", grid, code, out)
			}

			for _, want := range []string{
				"mode: create",
				"model 0 db=rag_0",
				"reset=fresh_database",
			} {
				requireContains(t, out, want)
			}
		})
	}
}

// TestSweepScriptRejectsReuseWithSkipIngest covers RAG-022B finding 3: reuse and
// skip-ingest cannot be combined, so the sweep refuses the combination with exit
// 2 and a clear message rather than proceeding on unspecified behavior.
func TestSweepScriptRejectsReuseWithSkipIngest(t *testing.T) {
	code, out := runSweepValidate(t, sweepTwoModels, map[string]string{
		"SWEEP_REUSE_DB":    "1",
		"SWEEP_SKIP_INGEST": "1",
		"SWEEP_DB_NAMES":    "rag_021_nomic,rag_021_gemma",
		"SWEEP_ALLOW_DB":    "rag_021_nomic,rag_021_gemma",
	})
	if code != 2 {
		t.Fatalf("exit=%d, want 2:\n%s", code, out)
	}

	requireContains(t, out, "SWEEP_REUSE_DB=1 cannot be combined with SWEEP_SKIP_INGEST=1")
}

// TestSweepScriptReuseWithoutSkipIngestStillValidates pins that the refusal is
// specific to the combination: reuse mode on its own (no skip-ingest) still
// validates and prints the unchanged reuse plan.
func TestSweepScriptReuseWithoutSkipIngestStillValidates(t *testing.T) {
	code, out := runSweepValidate(t, sweepTwoModels, map[string]string{
		"SWEEP_REUSE_DB":    "1",
		"SWEEP_SKIP_INGEST": "0",
		"SWEEP_DB_NAMES":    "rag_021_nomic,rag_021_gemma",
		"SWEEP_ALLOW_DB":    "rag_021_nomic,rag_021_gemma",
	})
	if code != 0 {
		t.Fatalf("exit=%d, want 0:\n%s", code, out)
	}

	for _, want := range []string{
		"mode: reuse",
		"reset=truncate",
		"drop_on_exit=no",
	} {
		requireContains(t, out, want)
	}
}

// TestSweepScriptRunPathRejectsEmptyGridBestFloor exercises RAG-022B's
// best_floor guard as far as it is observable off the validate-only surface: it
// runs the real create/reuse path with the recording psql and go shims, but only
// for a grid that is rejected up front, so the guard path (or the earlier grid
// guard) fails the run with exit 2 before run_eval is reached. The create SQL is
// still emitted, proving the grid check is the only failure and no repeats ran.
func TestSweepScriptRunPathRejectsEmptyGridBestFloor(t *testing.T) {
	rec := newSweepRecorder(t)

	results := filepath.Join(t.TempDir(), "results.md")
	_, out := rec.run(t, sweepOneModel, map[string]string{
		"SWEEP_RESULTS": results,
		"SWEEP_GRID":    "abc",
	})

	requireContains(t, out, "SWEEP_GRID must be a non-empty comma-separated list of numeric floors")

	if strings.Contains(out, "calibrating MIN_SIMILARITY") {
		t.Errorf("grid rejection must happen before calibration/run_eval:\n%s", out)
	}
}

// sweepRecorder is a recording environment: a temporary directory containing an
// executable `psql` (plus `make` and `go` stubs) prepended to PATH. The `psql`
// shim records its argv and answers the script's verification queries, so the
// sweep's create/reuse SQL paths run without a live PostgreSQL server, Docker, or
// a model while capturing the exact DROP/CREATE/TRUNCATE statements emitted.
type sweepRecorder struct {
	dir    string
	record string
}

// newSweepRecorder builds the recording shim directory and returns it.
func newSweepRecorder(t *testing.T) *sweepRecorder {
	t.Helper()

	dir := t.TempDir()
	record := filepath.Join(dir, "psql.log")

	// The shim appends every argument (including the -c statements) to the record
	// file, one invocation per line, then exits 0 so the script proceeds. The
	// script's reuse-mode verify_database runs `psql -tAc` ownership/extension/
	// schema probes and fails closed unless they answer `t`; the shim answers `t`
	// for those so the reuse path reaches reset_database's TRUNCATE.
	shim := "#!/bin/sh\n" +
		"{ printf 'psql %s\\n' \"$*\"; } >>\"" + record + "\"\n" +
		"case \" $* \" in\n" +
		"*\" -tAc \"*) echo t ;;\n" +
		"esac\n" +
		"exit 0\n"

	if err := os.WriteFile(filepath.Join(dir, "psql"), []byte(shim), 0o755); err != nil {
		t.Fatalf("write psql shim: %v", err)
	}

	// `make` is shimmed too (SWEEP_SKIP_DBUP=1 normally avoids db-up, but the
	// teardown `make db-down` is unconditional, so succeed anyway).
	makeShim := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "make"), []byte(makeShim), 0o755); err != nil {
		t.Fatalf("write make shim: %v", err)
	}

	// `go` is shimmed so the ingest/eval subprocesses succeed without a database
	// or model. That lets the model loop advance past model 0, so the regression
	// test observes every model's DROP/CREATE (and model 1 in particular) rather
	// than aborting on the first ingest. The eval output is empty, so the parsed
	// metrics are zero and harmless.
	goShim := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(goShim), 0o755); err != nil {
		t.Fatalf("write go shim: %v", err)
	}

	return &sweepRecorder{dir: dir, record: record}
}

// run executes the sweep script with the shim on PATH and the given overrides.
// It returns the record file contents and the combined script output. The
// script's `go run` steps are shimmed, but a real run cannot complete without a
// database, so the exit code is deliberately not required to be zero: the SQL
// statements under test are emitted before any such failure.
func (r *sweepRecorder) run(t *testing.T, models string, overrides map[string]string) (string, string) {
	t.Helper()

	script, err := filepath.Abs(sweepScript)
	if err != nil {
		t.Fatalf("resolve sweep script: %v", err)
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}

	cmd := exec.Command("sh", script, models, "3")
	cmd.Dir = repoRoot
	cmd.Env = sweepRecorderEnv(r.dir, overrides)

	out, _ := cmd.CombinedOutput()

	recorded, err := os.ReadFile(r.record)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read psql record: %v", err)
	}

	return string(recorded), string(out)
}

// sweepRecorderEnv is sweepEnv minus the forced validate-only flag, plus the
// shim directory prepended to PATH, SWEEP_SKIP_DBUP=1, and a results file under a
// temp dir so the run never writes the repository's results file.
func sweepRecorderEnv(shimDir string, overrides map[string]string) []string {
	var env []string

	for _, kv := range os.Environ() {
		name := kv[:strings.IndexByte(kv, '=')]
		if strings.HasPrefix(name, "SWEEP_") || name == "DATABASE_URL" || name == "PGCONNECT_TIMEOUT" {
			continue
		}

		if name == "PATH" {
			env = append(env, "PATH="+shimDir+string(os.PathListSeparator)+kv[len(name)+1:])

			continue
		}

		env = append(env, kv)
	}

	env = append(env, "SWEEP_SKIP_DBUP=1")
	env = append(env, "SWEEP_KEEP_UP=1")

	for key, value := range overrides {
		env = append(env, key+"="+value)
	}

	return env
}

// TestSweepScriptQuotesEmittedDatabaseIdentifiers is the RAG-022A regression
// test: it runs the create path and the reuse path with a recording psql shim on
// PATH and asserts that every DROP DATABASE / CREATE DATABASE / TRUNCATE
// statement the script emits names the target database as the validated,
// double-quoted identifier — never as a raw, unquoted name. It fails if a
// statement bypasses the shared validate-and-quote helper (for example if a
// splice site is reverted to `$name`/`$db_name`) and needs no PostgreSQL, Docker,
// or model access.
func TestSweepScriptQuotesEmittedDatabaseIdentifiers(t *testing.T) {
	t.Run("create mode quotes DROP and CREATE", func(t *testing.T) {
		rec := newSweepRecorder(t)

		results := filepath.Join(t.TempDir(), "results.md")
		recorded, out := rec.run(t, sweepTwoModels, map[string]string{
			"SWEEP_RESULTS": results,
		})

		if strings.TrimSpace(recorded) == "" {
			t.Fatalf("no psql invocations recorded; script output:\n%s", out)
		}

		for _, want := range []string{
			`DROP DATABASE IF EXISTS "rag_0"`,
			`CREATE DATABASE "rag_0"`,
			`DROP DATABASE IF EXISTS "rag_1"`,
			`CREATE DATABASE "rag_1"`,
		} {
			if !strings.Contains(recorded, want) {
				t.Errorf("recorded statements do not contain %q:\n%s", want, recorded)
			}
		}

		assertNoRawIdentifier(t, recorded, "rag_0")
		assertNoRawIdentifier(t, recorded, "rag_1")
	})

	t.Run("reuse mode quotes TRUNCATE and never drops", func(t *testing.T) {
		rec := newSweepRecorder(t)

		results := filepath.Join(t.TempDir(), "results.md")
		recorded, out := rec.run(t, "nomic-embed-text:768", map[string]string{
			"SWEEP_RESULTS":  results,
			"SWEEP_REUSE_DB": "1",
			"SWEEP_DB_NAMES": "rag_021_nomic",
			"SWEEP_ALLOW_DB": "rag_021_nomic",
		})

		if strings.TrimSpace(recorded) == "" {
			t.Fatalf("no psql invocations recorded; script output:\n%s", out)
		}

		if !strings.Contains(recorded, `TRUNCATE "documents" RESTART IDENTITY`) {
			t.Errorf("recorded statements do not contain the quoted TRUNCATE:\n%s", recorded)
		}

		if strings.Contains(recorded, "DROP DATABASE") {
			t.Errorf("reuse mode must never emit DROP DATABASE:\n%s", recorded)
		}

		if strings.Contains(recorded, "CREATE DATABASE") {
			t.Errorf("reuse mode must never emit CREATE DATABASE:\n%s", recorded)
		}
	})
}

// assertNoRawIdentifier fails when a DROP/CREATE/TRUNCATE statement names the
// database with an unquoted identifier, which would mean a splice site bypassed
// the shared validate-and-quote helper.
func assertNoRawIdentifier(t *testing.T, recorded, name string) {
	t.Helper()

	for _, prefix := range []string{
		"DROP DATABASE IF EXISTS ",
		"CREATE DATABASE ",
		"TRUNCATE ",
	} {
		raw := prefix + name

		if strings.Contains(recorded, raw) {
			t.Errorf("statement %q interpolates the raw database name; it must use the quoted helper output:\n%s", raw, recorded)
		}
	}
}
