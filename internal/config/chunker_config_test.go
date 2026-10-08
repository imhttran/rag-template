package config

import "testing"

// ChunkerConfig must be a deterministic function of the chunker settings: the
// same size and overlap always serialize to the same string, and changing
// either value produces a different one. Stored chunker configs are compared
// against this string on re-ingest, so a regression here would either skip a
// document that needs re-chunking or re-chunk one that does not.
func TestChunkerConfigStableAndSensitive(t *testing.T) {
	t.Run("identical settings produce identical identity", func(t *testing.T) {
		first := Config{ChunkSize: 50, ChunkOverlap: 20}
		second := Config{ChunkSize: 50, ChunkOverlap: 20}

		if first.ChunkerConfig() != second.ChunkerConfig() {
			t.Fatalf(
				"ChunkerConfig() = %q and %q, want them equal",
				first.ChunkerConfig(),
				second.ChunkerConfig(),
			)
		}
	})

	t.Run("a size change changes the identity", func(t *testing.T) {
		base := Config{ChunkSize: 50, ChunkOverlap: 20}
		changed := Config{ChunkSize: 51, ChunkOverlap: 20}

		if base.ChunkerConfig() == changed.ChunkerConfig() {
			t.Fatalf(
				"ChunkerConfig() did not change when CHUNK_SIZE did: %q",
				base.ChunkerConfig(),
			)
		}
	})

	t.Run("an overlap change changes the identity", func(t *testing.T) {
		base := Config{ChunkSize: 50, ChunkOverlap: 20}
		changed := Config{ChunkSize: 50, ChunkOverlap: 19}

		if base.ChunkerConfig() == changed.ChunkerConfig() {
			t.Fatalf(
				"ChunkerConfig() did not change when CHUNK_OVERLAP did: %q",
				base.ChunkerConfig(),
			)
		}
	})

	t.Run("the identity is deterministic across calls", func(t *testing.T) {
		cfg := Config{ChunkSize: 50, ChunkOverlap: 20}

		first := cfg.ChunkerConfig()
		second := cfg.ChunkerConfig()

		if first != second {
			t.Fatalf(
				"ChunkerConfig() is not stable across calls: %q and %q",
				first,
				second,
			)
		}
	})
}
