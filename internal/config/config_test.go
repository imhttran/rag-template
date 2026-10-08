package config

import (
	"strings"
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
// environment variable or given the wrong default fails here. A missing
// environment is the case where every value falls back to its default.
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

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

// TestLoad covers the four value classes: valid overrides, invalid values,
// boundaries, and missing (unset or blank) values.
//
// A present but invalid value must surface as an error naming the offending
// setting; it must never be silently replaced by the default. A missing value
// must fall back to the default.
func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
		check   func(*testing.T, Config)
	}{
		{
			name: "missing values fall back to defaults",
			env:  map[string]string{},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.ChunkSize != DefaultChunkSize || c.ChunkOverlap != DefaultChunkOverlap {
					t.Fatalf("chunk = %d/%d, want %d/%d", c.ChunkSize, c.ChunkOverlap, DefaultChunkSize, DefaultChunkOverlap)
				}

				if c.TopK != DefaultTopK || c.FinalK != DefaultFinalK || c.ExpandLimit != DefaultExpandLimit {
					t.Fatalf("k = %d/%d/%d, want %d/%d/%d", c.TopK, c.FinalK, c.ExpandLimit, DefaultTopK, DefaultFinalK, DefaultExpandLimit)
				}

				if c.MinSimilarity != DefaultMinSimilarity {
					t.Fatalf("MIN_SIMILARITY = %v, want %v", c.MinSimilarity, DefaultMinSimilarity)
				}
			},
		},
		{
			name: "blank values count as missing",
			env: map[string]string{
				"CHUNK_SIZE":      "",
				"CHUNK_OVERLAP":   "",
				"MIN_SIMILARITY":  "",
				"REQUEST_TIMEOUT": "",
				"QUERY_REWRITE":   "",
			},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.ChunkSize != DefaultChunkSize || c.ChunkOverlap != DefaultChunkOverlap {
					t.Fatalf("blank chunk = %d/%d, want %d/%d", c.ChunkSize, c.ChunkOverlap, DefaultChunkSize, DefaultChunkOverlap)
				}

				if c.MinSimilarity != DefaultMinSimilarity {
					t.Fatalf("blank MIN_SIMILARITY = %v, want %v", c.MinSimilarity, DefaultMinSimilarity)
				}

				if c.RequestTimeout != DefaultRequestTimeout {
					t.Fatalf("blank REQUEST_TIMEOUT = %v, want %v", c.RequestTimeout, DefaultRequestTimeout)
				}

				if c.QueryRewrite != DefaultQueryRewrite {
					t.Fatalf("blank QUERY_REWRITE = %v, want %v", c.QueryRewrite, DefaultQueryRewrite)
				}
			},
		},
		{
			name: "valid string override",
			env:  map[string]string{"OLLAMA_URL": "http://example:1234"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.OllamaURL != "http://example:1234" {
					t.Fatalf("OLLAMA_URL = %q", c.OllamaURL)
				}
			},
		},
		{
			name: "valid chunk size and overlap",
			env:  map[string]string{"CHUNK_SIZE": "250", "CHUNK_OVERLAP": "50"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.ChunkSize != 250 || c.ChunkOverlap != 50 {
					t.Fatalf("chunk = %d/%d, want 250/50", c.ChunkSize, c.ChunkOverlap)
				}
			},
		},
		{
			name: "boundary smallest chunk size",
			env:  map[string]string{"CHUNK_SIZE": "1", "CHUNK_OVERLAP": "0"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.ChunkSize != 1 || c.ChunkOverlap != 0 {
					t.Fatalf("chunk = %d/%d, want 1/0", c.ChunkSize, c.ChunkOverlap)
				}
			},
		},
		{
			name: "boundary overlap one below size",
			env:  map[string]string{"CHUNK_SIZE": "10", "CHUNK_OVERLAP": "9"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.ChunkOverlap != 9 {
					t.Fatalf("CHUNK_OVERLAP = %d, want 9", c.ChunkOverlap)
				}
			},
		},
		{
			name: "zero overlap is kept",
			env:  map[string]string{"CHUNK_OVERLAP": "0"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.ChunkOverlap != 0 {
					t.Fatalf("CHUNK_OVERLAP = %d, want 0", c.ChunkOverlap)
				}
			},
		},
		{
			name: "valid k and timeout overrides",
			env: map[string]string{
				"TOP_K":           "8",
				"FINAL_K":         "5",
				"EXPAND_LIMIT":    "10",
				"REQUEST_TIMEOUT": "90s",
			},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.TopK != 8 || c.FinalK != 5 || c.ExpandLimit != 10 {
					t.Fatalf("k = %d/%d/%d, want 8/5/10", c.TopK, c.FinalK, c.ExpandLimit)
				}

				if c.RequestTimeout != 90*time.Second {
					t.Fatalf("REQUEST_TIMEOUT = %v, want 90s", c.RequestTimeout)
				}
			},
		},
		{
			name: "boundary similarity one",
			env:  map[string]string{"MIN_SIMILARITY": "1"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.MinSimilarity != 1 {
					t.Fatalf("MIN_SIMILARITY = %v, want 1", c.MinSimilarity)
				}
			},
		},
		{
			name: "boundary similarity zero",
			env:  map[string]string{"MIN_SIMILARITY": "0"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.MinSimilarity != 0 {
					t.Fatalf("MIN_SIMILARITY = %v, want 0", c.MinSimilarity)
				}
			},
		},
		{
			name: "valid bool on",
			env:  map[string]string{"EVAL_LLM_RERANK": "true"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if !c.LLMRerank {
					t.Fatalf("EVAL_LLM_RERANK = false, want true")
				}
			},
		},
		{
			name: "valid bool off",
			env:  map[string]string{"RAG_ANSWERABILITY_GATE": "false"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.RagAnswerabilityGate {
					t.Fatalf("RAG_ANSWERABILITY_GATE = true, want false")
				}
			},
		},
		{
			name:    "invalid chunk size text",
			env:     map[string]string{"CHUNK_SIZE": "nope"},
			wantErr: "CHUNK_SIZE",
		},
		{
			name:    "invalid chunk size zero",
			env:     map[string]string{"CHUNK_SIZE": "0"},
			wantErr: "CHUNK_SIZE",
		},
		{
			name:    "invalid chunk size negative",
			env:     map[string]string{"CHUNK_SIZE": "-5"},
			wantErr: "CHUNK_SIZE",
		},
		{
			name:    "invalid overlap text",
			env:     map[string]string{"CHUNK_OVERLAP": "x"},
			wantErr: "CHUNK_OVERLAP",
		},
		{
			name:    "invalid overlap negative",
			env:     map[string]string{"CHUNK_OVERLAP": "-1"},
			wantErr: "CHUNK_OVERLAP",
		},
		{
			name:    "overlap equals chunk size",
			env:     map[string]string{"CHUNK_SIZE": "10", "CHUNK_OVERLAP": "10"},
			wantErr: "CHUNK_OVERLAP",
		},
		{
			name:    "overlap exceeds chunk size",
			env:     map[string]string{"CHUNK_SIZE": "10", "CHUNK_OVERLAP": "11"},
			wantErr: "CHUNK_OVERLAP",
		},
		{
			name:    "invalid top k zero",
			env:     map[string]string{"TOP_K": "0"},
			wantErr: "TOP_K",
		},
		{
			name:    "invalid final k zero",
			env:     map[string]string{"FINAL_K": "0"},
			wantErr: "FINAL_K",
		},
		{
			name:    "invalid expand limit zero",
			env:     map[string]string{"EXPAND_LIMIT": "0"},
			wantErr: "EXPAND_LIMIT",
		},
		{
			name:    "invalid similarity text",
			env:     map[string]string{"MIN_SIMILARITY": "x"},
			wantErr: "MIN_SIMILARITY",
		},
		{
			name:    "invalid similarity below range",
			env:     map[string]string{"MIN_SIMILARITY": "-0.1"},
			wantErr: "MIN_SIMILARITY",
		},
		{
			name:    "invalid similarity above range",
			env:     map[string]string{"MIN_SIMILARITY": "1.1"},
			wantErr: "MIN_SIMILARITY",
		},
		{
			name:    "invalid zero timeout",
			env:     map[string]string{"REQUEST_TIMEOUT": "0s"},
			wantErr: "REQUEST_TIMEOUT",
		},
		{
			name:    "invalid negative timeout",
			env:     map[string]string{"REQUEST_TIMEOUT": "-1s"},
			wantErr: "REQUEST_TIMEOUT",
		},
		{
			name:    "invalid timeout text",
			env:     map[string]string{"REQUEST_TIMEOUT": "soon"},
			wantErr: "REQUEST_TIMEOUT",
		},
		{
			name:    "invalid bool text",
			env:     map[string]string{"EVAL_LLM_RERANK": "maybe"},
			wantErr: "EVAL_LLM_RERANK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)

			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			cfg, err := Load()

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Load() error = nil, want an error naming %q", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %q, want it to contain %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}

			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}
