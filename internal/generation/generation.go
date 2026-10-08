// Package generation answers prompts with a local Ollama model.
package generation

import (
	"context"
	"fmt"

	"rag-template/internal/ollama"
)

// Generator produces text from a prompt.
type Generator interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// OllamaGenerator produces text with a specific Ollama model.
type OllamaGenerator struct {
	client *ollama.Client
	model  string
}

// New returns a Generator backed by Ollama.
func New(client *ollama.Client, model string) *OllamaGenerator {
	return &OllamaGenerator{client: client, model: model}
}

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type generateResponse struct {
	Response string `json:"response"`
}

// Generate returns the model's answer to prompt.
func (g *OllamaGenerator) Generate(ctx context.Context, prompt string) (string, error) {
	request := generateRequest{
		Model:  g.model,
		Prompt: prompt,
		Stream: false,
	}

	var result generateResponse
	if err := g.client.PostJSON(ctx, "/api/generate", request, &result); err != nil {
		return "", fmt.Errorf("generate answer: %w", err)
	}

	return result.Response, nil
}

// Compile-time check that OllamaGenerator satisfies Generator.
var _ Generator = (*OllamaGenerator)(nil)
