package provider

import (
	"testing"
	"time"

	"rag-template/internal/embedding"
	"rag-template/internal/generation"
)

func testClient() Client {
	return Client{
		OllamaURL:      "http://localhost:11434",
		EmbedModel:     "nomic-embed-text",
		ChatModel:      "qwen3.8:27b-mlx",
		RequestTimeout: time.Second,
	}
}

func TestResolveNameDefaults(t *testing.T) {
	if got := ResolveName(""); got != Default {
		t.Fatalf("ResolveName(empty) = %q, want %q", got, Default)
	}

	if got := ResolveName(Default); got != Default {
		t.Fatalf("ResolveName(%q) = %q, want %q", Default, got, Default)
	}
}

func TestEmbedderByName(t *testing.T) {
	value, err := Embedder(Default, testClient())
	if err != nil {
		t.Fatalf("Embedder(%q) returned error: %v", Default, err)
	}

	if _, ok := any(value).(embedding.Embedder); !ok {
		t.Fatalf("Embedder returned %T, want embedding.Embedder", value)
	}
}

func TestGeneratorByName(t *testing.T) {
	value, err := Generator(Default, testClient())
	if err != nil {
		t.Fatalf("Generator(%q) returned error: %v", Default, err)
	}

	if _, ok := any(value).(generation.Generator); !ok {
		t.Fatalf("Generator returned %T, want generation.Generator", value)
	}
}

func TestEmbedderDefaultsToOllama(t *testing.T) {
	if _, err := Embedder("", testClient()); err != nil {
		t.Fatalf("Embedder with empty name returned error: %v", err)
	}
}

func TestGeneratorDefaultsToOllama(t *testing.T) {
	if _, err := Generator("", testClient()); err != nil {
		t.Fatalf("Generator with empty name returned error: %v", err)
	}
}

func TestEmbedderUnknownName(t *testing.T) {
	value, err := Embedder("does-not-exist", testClient())
	if err == nil {
		t.Fatal("Embedder with unknown name returned nil error")
	}

	if value != nil {
		t.Fatalf("Embedder with unknown name returned %T, want nil", value)
	}
}

func TestGeneratorUnknownName(t *testing.T) {
	value, err := Generator("does-not-exist", testClient())
	if err == nil {
		t.Fatal("Generator with unknown name returned nil error")
	}

	if value != nil {
		t.Fatalf("Generator with unknown name returned %T, want nil", value)
	}
}
