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

func TestHasEmbedderAndGenerator(t *testing.T) {
	if !HasEmbedder(Default) {
		t.Fatalf("HasEmbedder(%q) = false, want true", Default)
	}

	if !HasEmbedder("") {
		t.Fatal("HasEmbedder(empty) = false, want true (resolves to Default)")
	}

	if HasEmbedder("does-not-exist") {
		t.Fatal("HasEmbedder(unknown) = true, want false")
	}

	if !HasGenerator(Default) {
		t.Fatalf("HasGenerator(%q) = false, want true", Default)
	}

	if !HasGenerator("") {
		t.Fatal("HasGenerator(empty) = false, want true (resolves to Default)")
	}

	if HasGenerator("does-not-exist") {
		t.Fatal("HasGenerator(unknown) = true, want false")
	}
}

// TestStubProviderSelection proves a registered non-default provider name is
// selected through Embedder/Generator and returns the deterministic stub.
func TestStubProviderSelection(t *testing.T) {
	cleanup := registerStub()
	defer cleanup()

	embedder, err := Embedder(stubProviderName, testClient())
	if err != nil {
		t.Fatalf("Embedder(%q) returned error: %v", stubProviderName, err)
	}

	stubEmbed, ok := any(embedder).(stubEmbedder)
	if !ok {
		t.Fatalf("Embedder(%q) returned %T, want stubEmbedder", stubProviderName, embedder)
	}

	got, err := stubEmbed.Embed(t.Context(), "anything")
	if err != nil {
		t.Fatalf("stub embedder returned error: %v", err)
	}

	if len(got) != len(stubEmbedVector) {
		t.Fatalf("stub embedder returned %d values, want %d", len(got), len(stubEmbedVector))
	}

	for index := range stubEmbedVector {
		if got[index] != stubEmbedVector[index] {
			t.Fatalf("stub embedder[%d] = %v, want %v", index, got[index], stubEmbedVector[index])
		}
	}

	generator, err := Generator(stubProviderName, testClient())
	if err != nil {
		t.Fatalf("Generator(%q) returned error: %v", stubProviderName, err)
	}

	stubGen, ok := any(generator).(stubGenerator)
	if !ok {
		t.Fatalf("Generator(%q) returned %T, want stubGenerator", stubProviderName, generator)
	}

	answer, err := stubGen.Generate(t.Context(), "anything")
	if err != nil {
		t.Fatalf("stub generator returned error: %v", err)
	}

	if answer != "stub answer" {
		t.Fatalf("stub generator returned %q, want %q", answer, "stub answer")
	}
}

// TestStubProviderDeterministic proves repeated stub calls return identical
// results, so the stub tests are deterministic and network-free.
func TestStubProviderDeterministic(t *testing.T) {
	cleanup := registerStub()
	defer cleanup()

	embedder, err := Embedder(stubProviderName, testClient())
	if err != nil {
		t.Fatalf("Embedder(%q) returned error: %v", stubProviderName, err)
	}

	first, err := embedder.Embed(t.Context(), "q")
	if err != nil {
		t.Fatalf("first stub embed returned error: %v", err)
	}

	second, err := embedder.Embed(t.Context(), "q")
	if err != nil {
		t.Fatalf("second stub embed returned error: %v", err)
	}

	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("stub embed not deterministic at %d: %v vs %v", index, first[index], second[index])
		}
	}
}

// TestStubRegistrationIsolated proves cleanup removes the stub so the registry
// is restored and an unregistered name errors again.
func TestStubRegistrationIsolated(t *testing.T) {
	cleanup := registerStub()

	if !HasEmbedder(stubProviderName) {
		t.Fatalf("HasEmbedder(%q) = false after registration, want true", stubProviderName)
	}

	cleanup()

	if HasEmbedder(stubProviderName) {
		t.Fatalf("HasEmbedder(%q) = true after cleanup, want false", stubProviderName)
	}

	if HasGenerator(stubProviderName) {
		t.Fatalf("HasGenerator(%q) = true after cleanup, want false", stubProviderName)
	}

	if _, err := Embedder(stubProviderName, testClient()); err == nil {
		t.Fatal("Embedder after cleanup returned nil error, want unregistered error")
	}
}
