package provider

import (
	"context"

	"rag-template/internal/embedding"
	"rag-template/internal/generation"
)

// stubProviderName is the provider name the stub is registered under in tests.
// It is deliberately not the default so selection can be proven to pick a
// non-default provider.
const stubProviderName = "stub"

// stubEmbedder is a deterministic, network-free embedding.Embedder. It returns
// the same fixed vector for every input and never performs I/O or depends on
// time or randomness.
type stubEmbedder struct {
	vector []float64
}

func (s stubEmbedder) Embed(_ context.Context, _ string) ([]float64, error) {
	out := make([]float64, len(s.vector))
	copy(out, s.vector)

	return out, nil
}

// stubGenerator is a deterministic, network-free generation.Generator. It
// returns the same fixed answer for every request and never calls the network.
type stubGenerator struct {
	answer string
}

func (s stubGenerator) Generate(_ context.Context, _ string) (string, error) {
	return s.answer, nil
}

// stubEmbedVector is the fixed vector every stub embedding returns.
var stubEmbedVector = []float64{0.25, 0.5, 0.75}

// registerStub registers the deterministic stub embedder and generator under
// stubProviderName and returns a cleanup function that restores the registry to
// its prior state, so registrations never leak between tests. It performs no
// network access and is safe to call with -race.
func registerStub() func() {
	undoEmbed := registerEmbedderForTest(
		stubProviderName,
		func(Client) (embedding.Embedder, error) {
			return stubEmbedder{vector: stubEmbedVector}, nil
		},
	)

	undoGen := registerGeneratorForTest(
		stubProviderName,
		func(Client) (generation.Generator, error) {
			return stubGenerator{answer: "stub answer"}, nil
		},
	)

	return func() {
		undoGen()
		undoEmbed()
	}
}
