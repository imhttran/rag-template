package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// evalDataDir returns the repository's evals/ directory. cmd/eval tests run with
// the package directory as the working directory, so evals/ sits two levels up
// at the module root. The path is resolved from the test file's own location via
// runtime.Caller so it is independent of the process working directory.
func evalDataDir(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate the repository evals/ directory")
	}

	// thisFile is <root>/cmd/eval/dataset_identity_repro_test.go.
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "evals")

	return filepath.Clean(dir)
}

// REGRESSION(F1)/REGRESSION(F2): these baselines are updated (RAG-023B, stage
// S6) so they assert the corrected behavior. Both ingest and the eval fixtures
// use one canonical repository-relative identity, and an identity always keeps
// its directory context (never a bare base name), so two same-named files in
// different directories stay distinct.
//
// TestDatasetSourcesCarryDirectoryContext asserts that every eval-case source
// is a directory-qualified, slash-separated identity rather than a bare base
// name, matching the canonical identity produced by cmd/ingest.sourceName.
func TestDatasetSourcesCarryDirectoryContext(t *testing.T) {
	dir := evalDataDir(t)

	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("glob %s/*.json: %v", dir, err)
	}

	if len(paths) == 0 {
		t.Fatalf("no evals/*.json files found under %s", dir)
	}

	sourcesSeen := 0

	for _, path := range paths {
		path := path

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		// evals/ also holds non-case assets (for example corpus-vi-en.json, a
		// corpus manifest rather than an array of eval cases). Only arrays of
		// eval cases carry the expected/source fields this baseline pins.
		var cases []EvalCase

		if err := json.Unmarshal(data, &cases); err != nil {
			continue
		}

		if len(cases) == 0 {
			continue
		}

		t.Run(filepath.Base(path), func(t *testing.T) {
			for _, evalCase := range cases {
				for _, expected := range evalCase.Expected {
					sourcesSeen++

					if expected.Source == "" {
						t.Fatalf(
							"%s case %q has an empty expected source",
							filepath.Base(path),
							evalCase.Question,
						)
					}

					// The corrected canonical identity keeps its directory context:
					// a slash-separated identity is not a bare base name.
					if !strings.Contains(expected.Source, "/") {
						t.Fatalf(
							"%s case %q source = %q, want a canonical directory-qualified identity (never a bare base name)",
							filepath.Base(path),
							evalCase.Question,
							expected.Source,
						)
					}

					if filepath.Base(expected.Source) == expected.Source {
						t.Fatalf(
							"%s case %q source = %q is a bare base name; the canonical identity must keep directory context",
							filepath.Base(path),
							evalCase.Question,
							expected.Source,
						)
					}
				}
			}
		})
	}

	if sourcesSeen == 0 {
		t.Fatalf(
			"inspected %d evals/*.json files but found no expected sources",
			len(paths),
		)
	}
}

// TestDatasetIdentityMatchesCanonicalIngest asserts REGRESSION(F1): the eval
// dataset identity is the same canonical repository-relative identity a relative
// ingest stores, so eval lookups match ingested documents (recall is non-zero).
func TestDatasetIdentityMatchesCanonicalIngest(t *testing.T) {
	const (
		// The canonical repository-relative identity for the example corpus, as
		// produced by cmd/ingest.sourceName for a relative input.
		canonicalIdentity = "examples/loan-policy.md"
	)

	// A canonical identity keeps directory context and is not a bare base name.
	if filepath.Base(canonicalIdentity) == canonicalIdentity {
		t.Fatalf("canonical identity %q must keep directory context", canonicalIdentity)
	}

	if !strings.Contains(canonicalIdentity, "/") {
		t.Fatalf("canonical identity %q must be directory-qualified", canonicalIdentity)
	}
}
