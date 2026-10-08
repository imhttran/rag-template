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
		"EMBED_DIM",
		"OLLAMA_CHAT_MODEL",
		"DATABASE_URL",
		"QUESTION",
		"EMBED_PROVIDER",
		"GEN_PROVIDER",
		"CORPUS_LANGUAGE",
		"CHUNK_SIZE",
		"CHUNK_OVERLAP",
		"TOP_K",
		"FINAL_K",
		"EXPAND_LIMIT",
		"CONTEXT_BUDGET",
		"EMBED_WORKERS",
		"EMBED_RETRIES",
		"MIN_SIMILARITY",
		"EVAL_LEXICAL_RERANK",
		"EVAL_LLM_RERANK",
		"RAG_LLM_RERANK",
		"EVAL_ANSWERABILITY_GATE",
		"EVAL_FACT_JUDGE",
		"EVAL_REWRITE_ONLY",
		"RAG_ANSWERABILITY_GATE",
		"RAG_CITATION_VALIDATION",
		"REQUEST_TIMEOUT",
		"QUERY_REWRITE",
		"OBSERVABILITY_FORMAT",
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
		OllamaURL:             DefaultOllamaURL,
		EmbedModel:            DefaultEmbedModel,
		EmbedDim:              DefaultEmbedDim,
		ChatModel:             DefaultChatModel,
		DatabaseURL:           DefaultDatabaseURL,
		EmbedProvider:         DefaultEmbedProvider,
		GenProvider:           DefaultGenProvider,
		Language:              DefaultLanguage,
		ChunkSize:             DefaultChunkSize,
		ChunkOverlap:          DefaultChunkOverlap,
		TopK:                  DefaultTopK,
		FinalK:                DefaultFinalK,
		ExpandLimit:           DefaultExpandLimit,
		ContextBudget:         DefaultContextBudget,
		EmbedWorkers:          DefaultEmbedWorkers,
		EmbedRetries:          DefaultEmbedRetries,
		MinSimilarity:         DefaultMinSimilarity,
		LexicalRerank:         DefaultLexicalRerank,
		LLMRerank:             DefaultLLMRerank,
		AnswerabilityGate:     DefaultAnswerabilityGate,
		FactJudge:             DefaultFactJudge,
		RewriteOnly:           DefaultRewriteOnly,
		RagAnswerabilityGate:  DefaultRagAnswerabilityGate,
		RagCitationValidation: DefaultRagCitationValidation,
		RequestTimeout:        DefaultRequestTimeout,
		QueryRewrite:          DefaultQueryRewrite,
		ObservabilityFormat:   DefaultObservabilityFormat,
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

// TestEmbedDimDefaultsTo768 pins the default dimension that the stored
// vector(768) column expects: an unset EMBED_DIM must not drift from 768.
func TestEmbedDimDefaultsTo768(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.EmbedDim != 768 {
		t.Fatalf("EmbedDim = %d, want 768", cfg.EmbedDim)
	}
}

// TestContextBudgetDefaultsToDisabled pins the default budget: an unset or
// blank CONTEXT_BUDGET must keep the current chunk-count behaviour (0/disabled)
// and must not drift to a non-zero default.
func TestContextBudgetDefaultsToDisabled(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if DefaultContextBudget != 0 {
		t.Fatalf("DefaultContextBudget = %d, want 0", DefaultContextBudget)
	}

	if cfg.ContextBudget != DefaultContextBudget {
		t.Fatalf("ContextBudget = %d, want %d", cfg.ContextBudget, DefaultContextBudget)
	}
}

// TestObservabilityFormatDefaultsToHuman pins the observability default: an
// unset or blank OBSERVABILITY_FORMAT must keep the current human-readable
// output and must not drift to JSON, which is opt-in.
func TestObservabilityFormatDefaultsToHuman(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if DefaultObservabilityFormat != OutputHuman {
		t.Fatalf("DefaultObservabilityFormat = %q, want %q", DefaultObservabilityFormat, OutputHuman)
	}

	if cfg.ObservabilityFormat != OutputHuman {
		t.Fatalf("ObservabilityFormat = %q, want %q", cfg.ObservabilityFormat, OutputHuman)
	}

	if cfg.ObservabilityJSON() {
		t.Fatalf("ObservabilityJSON() = true for the default, want false")
	}
}

// TestCorpusLanguage pins the language default and override: an unset or blank
// CORPUS_LANGUAGE keeps the baseline 'english' configuration (empty), and an
// explicit value is loaded verbatim.
func TestCorpusLanguage(t *testing.T) {
	clearEnv(t)

	if DefaultLanguage != "" {
		t.Fatalf("DefaultLanguage = %q, want empty (baseline english)", DefaultLanguage)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Language != DefaultLanguage {
		t.Fatalf("Language = %q, want %q", cfg.Language, DefaultLanguage)
	}

	t.Setenv("CORPUS_LANGUAGE", "de")

	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Language != "de" {
		t.Fatalf("CORPUS_LANGUAGE = %q, want %q", cfg.Language, "de")
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

				if c.EmbedDim != DefaultEmbedDim {
					t.Fatalf("EMBED_DIM = %d, want %d", c.EmbedDim, DefaultEmbedDim)
				}

				if c.ChunkSize != DefaultChunkSize || c.ChunkOverlap != DefaultChunkOverlap {
					t.Fatalf("chunk = %d/%d, want %d/%d", c.ChunkSize, c.ChunkOverlap, DefaultChunkSize, DefaultChunkOverlap)
				}

				if c.TopK != DefaultTopK || c.FinalK != DefaultFinalK || c.ExpandLimit != DefaultExpandLimit {
					t.Fatalf("k = %d/%d/%d, want %d/%d/%d", c.TopK, c.FinalK, c.ExpandLimit, DefaultTopK, DefaultFinalK, DefaultExpandLimit)
				}

				if c.ContextBudget != DefaultContextBudget {
					t.Fatalf("CONTEXT_BUDGET = %d, want %d", c.ContextBudget, DefaultContextBudget)
				}

				if c.EmbedWorkers != DefaultEmbedWorkers || c.EmbedRetries != DefaultEmbedRetries {
					t.Fatalf("embed = %d/%d, want %d/%d", c.EmbedWorkers, c.EmbedRetries, DefaultEmbedWorkers, DefaultEmbedRetries)
				}

				if c.MinSimilarity != DefaultMinSimilarity {
					t.Fatalf("MIN_SIMILARITY = %v, want %v", c.MinSimilarity, DefaultMinSimilarity)
				}

				if c.EmbedProvider != DefaultEmbedProvider || c.GenProvider != DefaultGenProvider {
					t.Fatalf("providers = %q/%q, want %q/%q", c.EmbedProvider, c.GenProvider, DefaultEmbedProvider, DefaultGenProvider)
				}

				if c.ObservabilityFormat != DefaultObservabilityFormat {
					t.Fatalf("OBSERVABILITY_FORMAT = %q, want %q", c.ObservabilityFormat, DefaultObservabilityFormat)
				}
			},
		},
		{
			name: "valid context budget override",
			env:  map[string]string{"CONTEXT_BUDGET": "4096"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.ContextBudget != 4096 {
					t.Fatalf("CONTEXT_BUDGET = %d, want 4096", c.ContextBudget)
				}
			},
		},
		{
			name: "zero context budget is kept (disabled)",
			env:  map[string]string{"CONTEXT_BUDGET": "0"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.ContextBudget != 0 {
					t.Fatalf("CONTEXT_BUDGET = %d, want 0", c.ContextBudget)
				}
			},
		},
		{
			name: "blank context budget counts as missing",
			env:  map[string]string{"CONTEXT_BUDGET": ""},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.ContextBudget != DefaultContextBudget {
					t.Fatalf("blank CONTEXT_BUDGET = %d, want %d", c.ContextBudget, DefaultContextBudget)
				}
			},
		},
		{
			name: "valid observability format human",
			env:  map[string]string{"OBSERVABILITY_FORMAT": "human"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.ObservabilityFormat != OutputHuman {
					t.Fatalf("OBSERVABILITY_FORMAT = %q, want %q", c.ObservabilityFormat, OutputHuman)
				}

				if c.ObservabilityJSON() {
					t.Fatalf("ObservabilityJSON() = true, want false")
				}
			},
		},
		{
			name: "valid observability format json",
			env:  map[string]string{"OBSERVABILITY_FORMAT": "json"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.ObservabilityFormat != OutputJSON {
					t.Fatalf("OBSERVABILITY_FORMAT = %q, want %q", c.ObservabilityFormat, OutputJSON)
				}

				if !c.ObservabilityJSON() {
					t.Fatalf("ObservabilityJSON() = false, want true")
				}
			},
		},
		{
			name: "observability format is case-insensitive",
			env:  map[string]string{"OBSERVABILITY_FORMAT": "JSON"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if !c.ObservabilityJSON() {
					t.Fatalf("ObservabilityJSON() = false for %q, want true", c.ObservabilityFormat)
				}
			},
		},
		{
			name: "blank observability format counts as missing",
			env:  map[string]string{"OBSERVABILITY_FORMAT": ""},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.ObservabilityFormat != DefaultObservabilityFormat {
					t.Fatalf("blank OBSERVABILITY_FORMAT = %q, want %q", c.ObservabilityFormat, DefaultObservabilityFormat)
				}
			},
		},
		{
			name:    "invalid observability format",
			env:     map[string]string{"OBSERVABILITY_FORMAT": "xml"},
			wantErr: "OBSERVABILITY_FORMAT",
		},
		{
			name: "valid embedding dimension override",
			env:  map[string]string{"EMBED_DIM": "1024"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.EmbedDim != 1024 {
					t.Fatalf("EMBED_DIM = %d, want 1024", c.EmbedDim)
				}
			},
		},
		{
			name: "blank embedding dimension counts as missing",
			env:  map[string]string{"EMBED_DIM": ""},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.EmbedDim != DefaultEmbedDim {
					t.Fatalf("blank EMBED_DIM = %d, want %d", c.EmbedDim, DefaultEmbedDim)
				}
			},
		},
		{
			name: "blank values count as missing",
			env: map[string]string{
				"CHUNK_SIZE":      "",
				"CHUNK_OVERLAP":   "",
				"EMBED_WORKERS":   "",
				"EMBED_RETRIES":   "",
				"MIN_SIMILARITY":  "",
				"REQUEST_TIMEOUT": "",
				"QUERY_REWRITE":   "",
				"EMBED_PROVIDER":  "",
				"GEN_PROVIDER":    "",
			},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.ChunkSize != DefaultChunkSize || c.ChunkOverlap != DefaultChunkOverlap {
					t.Fatalf("blank chunk = %d/%d, want %d/%d", c.ChunkSize, c.ChunkOverlap, DefaultChunkSize, DefaultChunkOverlap)
				}

				if c.EmbedWorkers != DefaultEmbedWorkers || c.EmbedRetries != DefaultEmbedRetries {
					t.Fatalf("blank embed = %d/%d, want %d/%d", c.EmbedWorkers, c.EmbedRetries, DefaultEmbedWorkers, DefaultEmbedRetries)
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

				if c.EmbedProvider != DefaultEmbedProvider || c.GenProvider != DefaultGenProvider {
					t.Fatalf("blank providers = %q/%q, want %q/%q", c.EmbedProvider, c.GenProvider, DefaultEmbedProvider, DefaultGenProvider)
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
			name: "valid provider overrides",
			env:  map[string]string{"EMBED_PROVIDER": "ollama", "GEN_PROVIDER": "ollama"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.EmbedProvider != "ollama" || c.GenProvider != "ollama" {
					t.Fatalf("providers = %q/%q, want ollama/ollama", c.EmbedProvider, c.GenProvider)
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
			name: "valid embed workers and retries",
			env:  map[string]string{"EMBED_WORKERS": "8", "EMBED_RETRIES": "5"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.EmbedWorkers != 8 || c.EmbedRetries != 5 {
					t.Fatalf("embed = %d/%d, want 8/5", c.EmbedWorkers, c.EmbedRetries)
				}
			},
		},
		{
			name: "boundary one embed worker",
			env:  map[string]string{"EMBED_WORKERS": "1"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.EmbedWorkers != 1 {
					t.Fatalf("EMBED_WORKERS = %d, want 1", c.EmbedWorkers)
				}
			},
		},
		{
			name: "zero embed retries is kept",
			env:  map[string]string{"EMBED_RETRIES": "0"},
			check: func(t *testing.T, c Config) {
				t.Helper()

				if c.EmbedRetries != 0 {
					t.Fatalf("EMBED_RETRIES = %d, want 0", c.EmbedRetries)
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
			name:    "invalid context budget text",
			env:     map[string]string{"CONTEXT_BUDGET": "lots"},
			wantErr: "CONTEXT_BUDGET",
		},
		{
			name:    "invalid context budget negative",
			env:     map[string]string{"CONTEXT_BUDGET": "-1"},
			wantErr: "CONTEXT_BUDGET",
		},
		{
			name:    "invalid embedding dimension text",
			env:     map[string]string{"EMBED_DIM": "abc"},
			wantErr: "EMBED_DIM",
		},
		{
			name:    "invalid embedding dimension zero",
			env:     map[string]string{"EMBED_DIM": "0"},
			wantErr: "EMBED_DIM",
		},
		{
			name:    "invalid embedding dimension negative",
			env:     map[string]string{"EMBED_DIM": "-1"},
			wantErr: "EMBED_DIM",
		},
		{
			name:    "invalid embed workers text",
			env:     map[string]string{"EMBED_WORKERS": "many"},
			wantErr: "EMBED_WORKERS",
		},
		{
			name:    "invalid embed workers zero",
			env:     map[string]string{"EMBED_WORKERS": "0"},
			wantErr: "EMBED_WORKERS",
		},
		{
			name:    "invalid embed workers negative",
			env:     map[string]string{"EMBED_WORKERS": "-1"},
			wantErr: "EMBED_WORKERS",
		},
		{
			name:    "invalid embed retries text",
			env:     map[string]string{"EMBED_RETRIES": "lots"},
			wantErr: "EMBED_RETRIES",
		},
		{
			name:    "invalid embed retries negative",
			env:     map[string]string{"EMBED_RETRIES": "-1"},
			wantErr: "EMBED_RETRIES",
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
