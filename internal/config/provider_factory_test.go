package config

import (
	"testing"
	"time"

	"rag-template/internal/embedding"
	"rag-template/internal/generation"
)

func factoryConfig() Config {
	return Config{
		OllamaURL:      DefaultOllamaURL,
		EmbedModel:     DefaultEmbedModel,
		ChatModel:      DefaultChatModel,
		EmbedProvider:  DefaultEmbedProvider,
		GenProvider:    DefaultGenProvider,
		RequestTimeout: time.Second,
	}
}

func TestConfigEmbedderReturnsInterface(t *testing.T) {
	value, err := factoryConfig().Embedder()
	if err != nil {
		t.Fatalf("Embedder() returned error: %v", err)
	}

	if _, ok := any(value).(embedding.Embedder); !ok {
		t.Fatalf("Embedder() returned %T, want embedding.Embedder", value)
	}
}

func TestConfigGeneratorReturnsInterface(t *testing.T) {
	value, err := factoryConfig().Generator()
	if err != nil {
		t.Fatalf("Generator() returned error: %v", err)
	}

	if _, ok := any(value).(generation.Generator); !ok {
		t.Fatalf("Generator() returned %T, want generation.Generator", value)
	}
}

func TestConfigEmbedderDefaultsToOllama(t *testing.T) {
	cfg := factoryConfig()
	cfg.EmbedProvider = ""

	if _, err := cfg.Embedder(); err != nil {
		t.Fatalf("Embedder() with empty provider returned error: %v", err)
	}
}

func TestConfigGeneratorDefaultsToOllama(t *testing.T) {
	cfg := factoryConfig()
	cfg.GenProvider = ""

	if _, err := cfg.Generator(); err != nil {
		t.Fatalf("Generator() with empty provider returned error: %v", err)
	}
}

func TestConfigEmbedderUnknownProvider(t *testing.T) {
	cfg := factoryConfig()
	cfg.EmbedProvider = "does-not-exist"

	value, err := cfg.Embedder()
	if err == nil {
		t.Fatal("Embedder() with unknown provider returned nil error")
	}

	if value != nil {
		t.Fatalf("Embedder() with unknown provider returned %T, want nil", value)
	}
}

func TestConfigGeneratorUnknownProvider(t *testing.T) {
	cfg := factoryConfig()
	cfg.GenProvider = "does-not-exist"

	value, err := cfg.Generator()
	if err == nil {
		t.Fatal("Generator() with unknown provider returned nil error")
	}

	if value != nil {
		t.Fatalf("Generator() with unknown provider returned %T, want nil", value)
	}
}
