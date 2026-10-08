package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"rag-template/internal/config"
)

// clearConfigEnv unsets every setting config.Load reads, so the developer's
// exported environment (a sourced .env) cannot change the expectations.
func clearConfigEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"OLLAMA_URL",
		"OLLAMA_EMBED_MODEL",
		"OLLAMA_CHAT_MODEL",
		"DATABASE_URL",
		"QUESTION",
		"CHUNK_SIZE",
		"CHUNK_OVERLAP",
		"TOP_K",
		"FINAL_K",
		"EXPAND_LIMIT",
		"MIN_SIMILARITY",
		"EVAL_LEXICAL_RERANK",
		"EVAL_LLM_RERANK",
		"RAG_LLM_RERANK",
		"EVAL_ANSWERABILITY_GATE",
		"EVAL_FACT_JUDGE",
		"EVAL_REWRITE_ONLY",
		"RAG_ANSWERABILITY_GATE",
		"REQUEST_TIMEOUT",
		"QUERY_REWRITE",
	} {
		t.Setenv(key, "")
	}
}

// writeDocument writes text to a file in a fresh temp dir and returns the path.
func writeDocument(t *testing.T, text string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "doc.md")

	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	return path
}

// A document that produces no chunks must fail with an explicit error rather
// than reporting success. run returns that error, which main turns into a
// nonzero exit through log.Fatal.
func TestRunRejectsDocumentWithoutSections(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"empty file", ""},
		{"file without headings", "Just prose with no markdown headings at all.\n"},
		{"content before the first heading", "intro text\n\nstill not a heading\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearConfigEnv(t)

			path := writeDocument(t, test.text)

			err := run(context.Background(), path)
			if err == nil {
				t.Fatalf("run(%q) = nil error, want an explicit no-sections error", path)
			}

			if !strings.Contains(err.Error(), "no sections found") {
				t.Fatalf("run(%q) error = %q, want it to mention %q", path, err, "no sections found")
			}

			if !strings.Contains(err.Error(), path) {
				t.Fatalf("run(%q) error = %q, want it to name the path", path, err)
			}
		})
	}
}

// A file that does have a section must still chunk normally, so the guard does
// not change valid documents.
func TestLoadChunksKeepsSections(t *testing.T) {
	clearConfigEnv(t)

	path := writeDocument(t, "## Payment\nPay twice and the second payment is refunded.\n")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	chunks, err := loadChunks(path, cfg)
	if err != nil {
		t.Fatalf("loadChunks(%q): %v", path, err)
	}

	if len(chunks) == 0 {
		t.Fatalf("loadChunks(%q) = 0 chunks, want at least one", path)
	}
}

// The regression is checked end to end as well: the built command must exit
// nonzero and print the explicit error, not merely return one from run.
func TestIngestCommandExitsNonzeroWithoutSections(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess build in short mode")
	}

	binary := filepath.Join(t.TempDir(), "ingest")

	build := exec.Command("go", "build", "-o", binary, ".")
	build.Stderr = os.Stderr

	if err := build.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}

	tests := []struct {
		name string
		text string
	}{
		{"empty file", ""},
		{"file without headings", "Just prose with no markdown headings at all.\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeDocument(t, test.text)

			command := exec.Command(binary, path)
			// An empty environment makes the child use config defaults, so a
			// developer's exported .env cannot change the outcome.
			command.Env = []string{}

			output, err := command.CombinedOutput()

			if _, ok := err.(*exec.ExitError); !ok {
				t.Fatalf("command error = %v (output %q), want a nonzero exit", err, output)
			}

			if !strings.Contains(string(output), "no sections found") {
				t.Fatalf("output = %q, want it to mention %q", output, "no sections found")
			}
		})
	}
}
