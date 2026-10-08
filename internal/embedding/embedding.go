// Package embedding turns text into vectors using Ollama.
package embedding

import (
	"context"
	"errors"
	"fmt"

	"rag-template/internal/ollama"
)

// Embedder turns text into an embedding vector. Ingestion and other consumers
// depend on this interface rather than a concrete implementation, so tests can
// substitute a stub that performs no HTTP.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
}

// ollamaEmbedder creates embeddings with a specific Ollama model. It is the
// default Embedder implementation.
type ollamaEmbedder struct {
	client *ollama.Client
	model  string
}

// New returns an Embedder that uses model and talks to Ollama.
func New(client *ollama.Client, model string) Embedder {
	return &ollamaEmbedder{client: client, model: model}
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

// Embed returns the embedding vector for text.
func (e *ollamaEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	request := embedRequest{
		Model: e.model,
		Input: text,
	}

	var result embedResponse
	if err := e.client.PostJSON(ctx, "/api/embed", request, &result); err != nil {
		return nil, fmt.Errorf("embed text: %w", err)
	}

	if len(result.Embeddings) == 0 {
		return nil, errors.New("ollama returned no embeddings")
	}

	return result.Embeddings[0], nil
}
