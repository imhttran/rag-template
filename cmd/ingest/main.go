// Command ingest chunks a document and stores it in PostgreSQL so the rag
// command can retrieve it. Re-running replaces that file's stored chunks.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"rag-template/internal/chunking"
	"rag-template/internal/config"
	"rag-template/internal/document"
	"rag-template/internal/embedding"
	"rag-template/internal/ingestion"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: ingest <file>")
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
	// instead, before opening a connection.
	if len(chunks) == 0 {
		return fmt.Errorf("no sections found in %s", path)
	}

	conn, err := cfg.Connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	service := ingestion.NewWithOptions(
		conn,
		embedding.New(cfg.OllamaClient(), cfg.EmbedModel),
		cfg.EmbedWorkers,
		cfg.EmbedRetries,
	)

	source := filepath.Base(path)

	if err := service.ReplaceDocument(ctx, source, chunks); err != nil {
		return err
	}

	fmt.Printf("Ingested %d chunks from %s.\n", len(chunks), source)

	return nil
}

// loadChunks reads the corpus and splits it into chunks.
func loadChunks(path string, cfg config.Config) ([]chunking.Chunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return chunking.FromSections(
		document.ParseSections(string(data)),
		cfg.ChunkSize,
		cfg.ChunkOverlap,
	), nil
}
