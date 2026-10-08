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
	"rag-template/internal/embedding"
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

	service := ingestion.NewWithOptions(
		conn,
		embedding.New(cfg.OllamaClient(), cfg.EmbedModel),
		cfg.EmbedWorkers,
		cfg.EmbedRetries,
	)

	// source keeps a file's directory context so a/policy.md and b/policy.md are
	// distinct sources instead of colliding as "policy.md".
	source := sourceName(path)

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	// The fingerprint covers both the file's bytes and the chunker configuration,
	// so a content edit or a CHUNK_SIZE/CHUNK_OVERLAP change both invalidate it.
	chunkerConfig := cfg.ChunkerConfig()
	provenance := ingestion.Provenance{
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

// sourceName returns the identity stored in the source column. A relative path
// keeps a file's directory context (a/policy.md is distinct from b/policy.md)
// while staying stable across working directories for absolute inputs.
func sourceName(path string) string {
	if cleaned := filepath.Clean(path); !filepath.IsAbs(cleaned) {
		return cleaned
	}

	return filepath.Base(path)
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
