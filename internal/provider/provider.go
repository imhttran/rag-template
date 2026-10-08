// Package provider maps provider names (EMBED_PROVIDER / GEN_PROVIDER) to
// constructors returning embedding.Embedder and generation.Generator.
//
// It is the only package other than internal/ollama allowed to know about a
// vendor client: the rest of the codebase resolves implementations through the
// registry by name and never imports a vendor directly.
package provider

import (
	"fmt"
	"net/http"
	"time"

	"rag-template/internal/embedding"
	"rag-template/internal/generation"
	"rag-template/internal/ollama"
)

// Default is the provider name used when none is configured.
const Default = "ollama"

// Client carries the settings the registered constructors need to build a
// vendor client.
type Client struct {
	OllamaURL      string
	EmbedModel     string
	ChatModel      string
	RequestTimeout time.Duration
}

// EmbedderConstructor builds an embedding.Embedder for a provider.
type EmbedderConstructor func(Client) (embedding.Embedder, error)

// GeneratorConstructor builds a generation.Generator for a provider.
type GeneratorConstructor func(Client) (generation.Generator, error)

var embedders = map[string]EmbedderConstructor{
	Default: func(c Client) (embedding.Embedder, error) {
		return embedding.New(ollama.New(c.OllamaURL, &http.Client{Timeout: c.RequestTimeout}), c.EmbedModel), nil
	},
}

var generators = map[string]GeneratorConstructor{
	Default: func(c Client) (generation.Generator, error) {
		return generation.New(ollama.New(c.OllamaURL, &http.Client{Timeout: c.RequestTimeout}), c.ChatModel), nil
	},
}

// ResolveName returns name, or Default when name is empty.
func ResolveName(name string) string {
	if name == "" {
		return Default
	}

	return name
}

// Embedder returns an embedding.Embedder for the named provider. An empty name
// resolves to Default. An unregistered name is an error, never a nil success.
func Embedder(name string, client Client) (embedding.Embedder, error) {
	resolved := ResolveName(name)

	constructor, ok := embedders[resolved]
	if !ok {
		return nil, fmt.Errorf("unknown embed provider %q", resolved)
	}

	return constructor(client)
}

// Generator returns a generation.Generator for the named provider. An empty
// name resolves to Default. An unregistered name is an error, never a nil
// success.
func Generator(name string, client Client) (generation.Generator, error) {
	resolved := ResolveName(name)

	constructor, ok := generators[resolved]
	if !ok {
		return nil, fmt.Errorf("unknown generate provider %q", resolved)
	}

	return constructor(client)
}
