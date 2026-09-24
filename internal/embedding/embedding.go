// Package embedding turns text into vectors using Ollama.
package embedding

import (
	"context"
	"errors"
	"fmt"

	"rag-template/internal/ollama"
)

// Embedder creates embeddings with a specific Ollama model.
type Embedder struct {
	client *ollama.Client
	model  string
}

// New returns an Embedder that uses model.
func New(client *ollama.Client, model string) *Embedder {
	return &Embedder{client: client, model: model}
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

// Embed returns the embedding vector for text.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float64, error) {
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
