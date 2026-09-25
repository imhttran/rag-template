package config

import (
	"testing"
	"time"
)

// clearEnv unsets every setting Load reads, so the developer's exported
// environment (a sourced .env) cannot leak into the expectations.
func clearEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"OLLAMA_URL",
		"OLLAMA_EMBED_MODEL",
		"OLLAMA_CHAT_MODEL",
		"DATABASE_URL",
		"QUESTION",
		"CHUNK_SIZE",
		"CHUNK_OVERLAP",
		"TOP_K",
		"FINAL_K",
		"EXPAND_LIMIT",
		"MIN_SIMILARITY",
		"EVAL_LEXICAL_RERANK",
		"EVAL_LLM_RERANK",
		"RAG_LLM_RERANK",
		"EVAL_ANSWERABILITY_GATE",
		"EVAL_FACT_JUDGE",
		"EVAL_REWRITE_ONLY",
		"RAG_ANSWERABILITY_GATE",
		"REQUEST_TIMEOUT",
		"QUERY_REWRITE",
	} {
		t.Setenv(key, "")
	}
}

// The whole struct is compared at once, so a setting wired to the wrong
// environment variable or given the wrong default fails here.
func TestLoadDefaults(t *testing.T) {
	clearEnv(t)

	want := Config{
		OllamaURL:            DefaultOllamaURL,
		EmbedModel:           DefaultEmbedModel,
		ChatModel:            DefaultChatModel,
		DatabaseURL:          DefaultDatabaseURL,
		ChunkSize:            DefaultChunkSize,
		ChunkOverlap:         DefaultChunkOverlap,
		TopK:                 DefaultTopK,
		FinalK:               DefaultFinalK,
		ExpandLimit:          DefaultExpandLimit,
		MinSimilarity:        DefaultMinSimilarity,
		LexicalRerank:        DefaultLexicalRerank,
		LLMRerank:            DefaultLLMRerank,
		AnswerabilityGate:    DefaultAnswerabilityGate,
		FactJudge:            DefaultFactJudge,
		RewriteOnly:          DefaultRewriteOnly,
		RagAnswerabilityGate: DefaultRagAnswerabilityGate,
		RequestTimeout:       DefaultRequestTimeout,
		QueryRewrite:         DefaultQueryRewrite,
	}

	if got := Load(); got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		check func(Config) bool
	}{
		{"string", "OLLAMA_URL", "http://example:1234", func(c Config) bool {
			return c.OllamaURL == "http://example:1234"
		}},
		{"int", "CHUNK_SIZE", "250", func(c Config) bool {
			return c.ChunkSize == 250
		}},
		{"float", "MIN_SIMILARITY", "0.75", func(c Config) bool {
			return c.MinSimilarity == 0.75
		}},
		{"duration", "REQUEST_TIMEOUT", "90s", func(c Config) bool {
			return c.RequestTimeout == 90*time.Second
		}},
		{"bool on", "EVAL_LLM_RERANK", "true", func(c Config) bool {
			return c.LLMRerank
		}},
		{"bool off", "RAG_ANSWERABILITY_GATE", "false", func(c Config) bool {
			return !c.RagAnswerabilityGate
		}},
		{"unparseable int", "CHUNK_SIZE", "nope", func(c Config) bool {
			return c.ChunkSize == DefaultChunkSize
		}},
		{"zero chunk size", "CHUNK_SIZE", "0", func(c Config) bool {
			return c.ChunkSize == DefaultChunkSize
		}},
		{"zero overlap is kept", "CHUNK_OVERLAP", "0", func(c Config) bool {
			return c.ChunkOverlap == 0
		}},
		{"negative overlap", "CHUNK_OVERLAP", "-1", func(c Config) bool {
			return c.ChunkOverlap == DefaultChunkOverlap
		}},
		{"unparseable float", "MIN_SIMILARITY", "x", func(c Config) bool {
			return c.MinSimilarity == DefaultMinSimilarity
		}},
		{"non-positive duration", "REQUEST_TIMEOUT", "0s", func(c Config) bool {
			return c.RequestTimeout == DefaultRequestTimeout
		}},
		{"unparseable bool", "EVAL_LLM_RERANK", "maybe", func(c Config) bool {
			return !c.LLMRerank
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(tt.key, tt.value)

			if got := Load(); !tt.check(got) {
				t.Fatalf("Load() with %s=%q = %+v", tt.key, tt.value, got)
			}
		})
	}
}
