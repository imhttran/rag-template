// Command ingest chunks a document and stores it in PostgreSQL so the rag
// command can retrieve it. Re-running an unchanged file is a no-op; re-running
// a changed file replaces that file's stored chunks.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"rag-template/internal/chunking"
	"rag-template/internal/config"
	"rag-template/internal/ingestion"
	"rag-template/internal/loader"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <file>", os.Args[0])
	}

	if err := run(context.Background(), os.Args[1]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, path string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
	defer cancel()

	chunks, err := loadChunks(path, cfg)
	if err != nil {
		return err
	}

	// A document that yields no chunks (an empty file, or one without any
	// "## " section) would otherwise reach the database, delete the source's
	// rows, insert nothing, and report success. Fail with an explicit error
	// instead so the problem is visible.
	if len(chunks) == 0 {
		return fmt.Errorf("no sections found in %s", path)
	}

	conn, err := cfg.Connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	// Fail fast when EMBED_DIM does not match the stored documents.embedding
	// column, before any embedding is requested. On the default 768 path this is
	// a silent no-op.
	if err := ingestion.CheckEmbeddingDim(ctx, conn, cfg.EmbedDim); err != nil {
		return err
	}

	// Resolve the embedder through the provider registry so EMBED_PROVIDER
	// selects the implementation. An empty provider keeps the ollama default; an
	// unregistered provider is an error, never a silent fallback.
	embedder, err := cfg.Embedder()
	if err != nil {
		return err
	}

	service := ingestion.NewWithOptions(
		conn,
		embedder,
		cfg.EmbedWorkers,
		cfg.EmbedRetries,
	)

	// source is the canonical repository-relative identity, with symlinks
	// resolved, so a/policy.md and b/policy.md stay distinct and a symlinked
	// input stores its resolved target.
	source, err := sourceName(path)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	// The fingerprint covers both the file's bytes and the chunker configuration,
	// so a content edit or a CHUNK_SIZE/CHUNK_OVERLAP change both invalidate it.
	chunkerConfig := cfg.ChunkerConfig()
	provenance := ingestion.Provenance{
		// Optional corpus identity (RAG-SOP-004 POC). Empty keeps the previous
		// behavior (NULL corpus). Read from the environment so no flag/parser change
		// is required and existing invocations are unchanged.
		Corpus:        os.Getenv("RAG_CORPUS"),
		ContentHash:   contentFingerprint(data, chunkerConfig),
		EmbedModel:    cfg.EmbedModel,
		ChunkerConfig: chunkerConfig,
		Language:      cfg.Language,
		IngestedAt:    time.Now().UTC(),
	}

	upToDate, err := service.IsUpToDate(ctx, source, provenance)
	if err != nil {
		return err
	}

	if upToDate {
		fmt.Printf("Skipped %s: unchanged (%d chunks).\n", source, len(chunks))

		return nil
	}

	if err := service.ReplaceDocumentWithProvenance(ctx, source, chunks, provenance); err != nil {
		return err
	}

	fmt.Printf("Ingested %s (%d chunks).\n", source, len(chunks))

	return nil
}

// loadChunks reads the corpus and splits it into chunks. The document's format
// is detected by the loader registry, which returns []document.Section;
// unknown formats yield an explicit "no loader" error naming the path.
func loadChunks(path string, cfg config.Config) ([]chunking.Chunk, error) {
	sections, err := loader.Dispatch(path)
	if err != nil {
		return nil, err
	}

	return chunking.FromSections(
		sections,
		cfg.ChunkSize,
		cfg.ChunkOverlap,
	), nil
}

// sourceName returns the canonical identity stored in the source column: the
// input path resolved against symlinks and made relative to the nearest
// repository root (an ancestor containing a .git directory). It never falls
// back to the base name, so a/policy.md and b/policy.md stay distinct and an
// absolute or symlinked input yields the same identity as its canonical
// relative form. When no repository root is found the resolved absolute path is
// used, so the identity is always directory-qualified and never a bare base
// name.
func sourceName(path string) (string, error) {
	// Resolve symlinks first so a symlinked input stores the resolved target.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		// A path that cannot be resolved (for example a name that does not
		// exist yet) falls back to its absolute form so identity stays stable.
		resolved, err = filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", path, err)
		}
	}

	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}

	root, ok := repositoryRoot(resolved)
	if !ok {
		// No repository root: keep the resolved absolute path so the identity
		// still carries its full directory context and never collapses to a
		// bare base name.
		return filepath.ToSlash(resolved), nil
	}

	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		// Fall back to the absolute path only when the resolved path is not
		// under the root; the base name is never used.
		return filepath.ToSlash(resolved), nil
	}

	return filepath.ToSlash(rel), nil
}

// repositoryRoot walks up from the resolved path looking for a directory that
// contains a .git entry. It reports ok=false when no such ancestor exists, so
// callers can fall back to the absolute path instead of collapsing to a base
// name.
func repositoryRoot(resolved string) (string, bool) {
	// Start from the resolved path's directory when it names a file, and from
	// the path itself when it names a directory.
	current := resolved
	if info, err := os.Stat(resolved); err == nil && !info.IsDir() {
		current = filepath.Dir(resolved)
	}

	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, true
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}

		current = parent
	}
}

// contentFingerprint hashes the file's bytes together with the chunker config,
// so either a content change or a chunker-config change produces a new value.
func contentFingerprint(data []byte, chunkerConfig string) string {
	hash := sha256.New()
	hash.Write([]byte(chunkerConfig))
	hash.Write([]byte{0})
	hash.Write(data)

	return hex.EncodeToString(hash.Sum(nil))
}
