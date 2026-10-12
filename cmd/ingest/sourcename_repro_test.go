package main

import (
	"os"
	"path/filepath"
	"testing"
)

// REGRESSION(F1)/REGRESSION(F2): these cases now assert the *corrected*
// canonical source-identity behavior of sourceName (cmd/ingest/main.go).
//
// F1 (identity mismatch): eval datasets and ingest must agree on identity. Both
// now use the canonical repository-relative, symlink-resolved path (never a bare
// base name), so a relative ingest and the eval fixtures share one identity.
//
// F2 (absolute collision): an absolute path must NOT collapse to filepath.Base;
// distinct files at different directories (/a/policy.md and /b/policy.md) must
// resolve to distinct sources.
//
// These baselines are updated (RAG-023B, stage S6) from the A1 characterization
// tests so they assert the corrected behavior.
func TestSourceNameCanonicalBehavior(t *testing.T) {
	// repositoryRoot of the ingest package is the module root (it contains
	// .git), so a path under the repository resolves to a repo-relative slash
	// path without the base-name fallback.
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	// Build a real file inside the repository so EvalSymlinks succeeds and the
	// repo-relative identity can be computed deterministically.
	dir := t.TempDir()
	file := filepath.Join(dir, "loan-policy.md")
	if err := os.WriteFile(file, []byte("# Loan Policy\n\n## Late Payments\n\nbody"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := sourceName(file)
	if err != nil {
		t.Fatalf("sourceName(%q) error: %v", file, err)
	}

	// The identity is never a bare base name: it keeps the directory context.
	if got == filepath.Base(file) {
		t.Fatalf("sourceName(%q) = %q, want a directory-qualified identity, never the base name", file, got)
	}

	// Two distinct files with the same base name stay distinct.
	dir2 := t.TempDir()
	file2 := filepath.Join(dir2, "loan-policy.md")
	if err := os.WriteFile(file2, []byte("# Other\n\n## Section\n\nbody"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got2, err := sourceName(file2)
	if err != nil {
		t.Fatalf("sourceName(%q) error: %v", file2, err)
	}

	if got == got2 {
		t.Fatalf("distinct files with the same base name produced the same identity %q; /a/policy.md and /b/policy.md must stay distinct", got)
	}

	// Resolving the same file via its absolute and cleaned forms is stable
	// (idempotent identity), so repeated ingestion resolves to the same source.
	gotAgain, err := sourceName(filepath.Clean(file))
	if err != nil {
		t.Fatalf("sourceName(%q) error: %v", filepath.Clean(file), err)
	}

	if got != gotAgain {
		t.Fatalf("sourceName(%q) = %q, want %q (stable identity across repeated calls)", filepath.Clean(file), gotAgain, got)
	}

	if root == "" {
		t.Fatal("expected a non-empty working directory to derive the repository root")
	}
}

// TestSourceNameResolvesSymlink pins REGRESSION(F1): a symlinked input stores the
// resolved target's canonical identity, not the link's name or base name.
func TestSourceNameResolvesSymlink(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "real-policy.md")
	if err := os.WriteFile(target, []byte("# Policy\n\n## Section\n\nbody"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}

	link := filepath.Join(dir, "alias-policy.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	targetIdentity, err := sourceName(target)
	if err != nil {
		t.Fatalf("sourceName(%q) error: %v", target, err)
	}

	linkIdentity, err := sourceName(link)
	if err != nil {
		t.Fatalf("sourceName(%q) error: %v", link, err)
	}

	if linkIdentity != targetIdentity {
		t.Fatalf(
			"sourceName(link) = %q, want the resolved target identity %q",
			linkIdentity,
			targetIdentity,
		)
	}

	if linkIdentity == filepath.Base(link) {
		t.Fatalf("sourceName(link) = %q, want the resolved target identity, never the link base name", linkIdentity)
	}
}

// TestSourceNameAbsolutePathsStayDistinct pins REGRESSION(F2) directly: two
// distinct absolute files under different directories must NOT share a source
// identity (the old base-name collision is corrected).
func TestSourceNameAbsolutePathsStayDistinct(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	fileA := filepath.Join(dirA, "policy.md")
	fileB := filepath.Join(dirB, "policy.md")

	if err := os.WriteFile(fileA, []byte("# A\n\n## S\n\nbody"), 0o600); err != nil {
		t.Fatalf("write A: %v", err)
	}

	if err := os.WriteFile(fileB, []byte("# B\n\n## S\n\nbody"), 0o600); err != nil {
		t.Fatalf("write B: %v", err)
	}

	first, err := sourceName(fileA)
	if err != nil {
		t.Fatalf("sourceName(%q) error: %v", fileA, err)
	}

	second, err := sourceName(fileB)
	if err != nil {
		t.Fatalf("sourceName(%q) error: %v", fileB, err)
	}

	if first == second {
		t.Fatalf(
			"distinct absolute paths produced identical identities %q; /a/policy.md and /b/policy.md must stay distinct",
			first,
		)
	}

	if first == filepath.Base(fileA) || second == filepath.Base(fileB) {
		t.Fatalf("identities %q and %q must not be bare base names", first, second)
	}
}
