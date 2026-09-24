// Package generation answers prompts with a local Ollama model.
package generation

import (
	"context"
	"fmt"

	"rag-template/internal/ollama"
)

// Generator produces text with a specific Ollama model.
type Generator struct {
	client *ollama.Client
	model  string
}

// New returns a Generator.
func New(client *ollama.Client, model string) *Generator {
	return &Generator{client: client, model: model}
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
func (g *Generator) Generate(ctx context.Context, prompt string) (string, error) {
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
