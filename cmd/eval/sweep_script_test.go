package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests exercise scripts/eval-model-sweep.sh through its validate-only mode
// (SWEEP_VALIDATE_ONLY=1). That mode performs the argument validation and safety
// guards and prints the resolved plan, but never touches a database or a model,
// so the sweep's mapping, allowlist, identifier, and repeat-isolation rules are
// covered without provisioning PostgreSQL.

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
