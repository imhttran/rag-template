// Package config holds the runtime settings shared by the commands. Every
// setting can be overridden with an environment variable so the code does not
// have to change between machines.
//
// A setting that is absent (unset or blank) falls back to its default. A setting
// that is present but invalid is an error, never a silent fallback: a typo in
// CHUNK_SIZE should stop the command with a message naming the variable instead
// of quietly using the default.
package config

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"rag-template/internal/embedding"
	"rag-template/internal/generation"
	"rag-template/internal/ollama"
	"rag-template/internal/provider"
)

const (
	DefaultOllamaURL   = "http://localhost:11434"
	DefaultEmbedModel  = "nomic-embed-text"
	DefaultChatModel   = "qwen3.8:27b-mlx"
	DefaultDatabaseURL = "postgres://rag:rag@127.0.0.1:5433/rag?sslmode=disable"
	DefaultTopK        = 4

	// DefaultEmbedProvider and DefaultGenProvider name the provider registry
	// entries the factory methods resolve when EMBED_PROVIDER / GEN_PROVIDER
	// are unset.
	DefaultEmbedProvider = provider.Default
	DefaultGenProvider   = provider.Default

	// DefaultChunkSize and DefaultChunkOverlap are the word counts chunking
	// splits a section into, used by cmd/ingest. 50/20 measured best on the
	// example corpus (see scripts/sweep.sh).
	DefaultChunkSize    = 50
	DefaultChunkOverlap = 20

	// DefaultEmbedWorkers bounds how many embedding calls cmd/ingest runs
	// concurrently. The default is deliberately modest: Ollama serves requests
	// from a single model instance, so a larger fan-out mostly queues server
	// side while adding memory pressure locally.
	DefaultEmbedWorkers = 4

	// DefaultEmbedRetries is how many times a transient embedding failure is
	// retried before the ingest fails. The default is small because a failure
	// that survives a few backoffs is more likely persistent than transient.
	DefaultEmbedRetries = 3

	// DefaultMinSimilarity is the lowest cosine similarity a retrieved
	// document may have to be used as context.
	DefaultMinSimilarity = 0.6

	// DefaultLLMRerank is whether cmd/eval also reranks with the chat model.
	// Off by default: it is slower and needs OLLAMA_CHAT_MODEL.
	DefaultLLMRerank = false

	// DefaultLexicalRerank is whether cmd/eval reranks candidates lexically.
	// Off by default: the K baselines are reported on their own.
	DefaultLexicalRerank = false

	// DefaultAnswerabilityGate is whether cmd/eval asks the chat model whether
	// the retrieved evidence can answer each question. Off by default: it adds
	// one chat-model call per case.
	DefaultAnswerabilityGate = false

	// DefaultFactJudge is whether cmd/eval asks the chat model which expected
	// facts the retrieved evidence supports. Off by default: it adds one
	// chat-model call per case that lists facts.
	DefaultFactJudge = false

	// DefaultRewriteOnly is whether cmd/eval retrieves with the rewritten query
	// alone, instead of the original plus the rewritten query. Off by default:
	// it is an experiment comparing replacing the original query against
	// keeping it alongside the rewrite.
	DefaultRewriteOnly = false

	// DefaultQueryRewrite is whether the question is rewritten into a search
	// query with the chat model before retrieval. On by default: it improves
	// recall on the example corpus, at the cost of one chat-model call per query.
	DefaultQueryRewrite = true

	// DefaultRagAnswerabilityGate is whether cmd/rag checks answerability with
	// the chat model before answering. On by default.
	DefaultRagAnswerabilityGate = true

	// DefaultFinalK is how many fused candidates cmd/rag keeps after hybrid
	// retrieval, before the matched sections are expanded.
	DefaultFinalK = 3

	// DefaultExpandLimit caps how many section chunks cmd/rag sends as context.
	DefaultExpandLimit = 20

	// DefaultRequestTimeout bounds every network call the commands make.
	DefaultRequestTimeout = 5 * time.Minute
)

// Config holds the runtime settings.
type Config struct {
	OllamaURL            string
	EmbedModel           string
	ChatModel            string
	DatabaseURL          string
	Question             string
	EmbedProvider        string
	GenProvider          string
	ChunkSize            int
	ChunkOverlap         int
	TopK                 int
	FinalK               int
	ExpandLimit          int
	EmbedWorkers         int
	EmbedRetries         int
	MinSimilarity        float64
	LexicalRerank        bool
	LLMRerank            bool
	RagLLMRerank         bool
	AnswerabilityGate    bool
	FactJudge            bool
	RewriteOnly          bool
	RagAnswerabilityGate bool
	RequestTimeout       time.Duration
	QueryRewrite         bool
}

// Load reads the settings from the environment, falling back to the defaults
// when a setting is absent.
//
// It returns an error naming the offending variable when a present setting
// cannot be parsed or is out of range, and when CHUNK_OVERLAP is not smaller than
// CHUNK_SIZE. Invalid values are never silently replaced with defaults.
func Load() (Config, error) {
	cfg := Config{
		OllamaURL:     envOrDefault("OLLAMA_URL", DefaultOllamaURL),
		EmbedModel:    envOrDefault("OLLAMA_EMBED_MODEL", DefaultEmbedModel),
		ChatModel:     envOrDefault("OLLAMA_CHAT_MODEL", DefaultChatModel),
		DatabaseURL:   envOrDefault("DATABASE_URL", DefaultDatabaseURL),
		EmbedProvider: envOrDefault("EMBED_PROVIDER", DefaultEmbedProvider),
		GenProvider:   envOrDefault("GEN_PROVIDER", DefaultGenProvider),
		// Question has no default: the rag command requires one.
		Question: envOrDefault("QUESTION", ""),
	}

	var err error

	if cfg.ChunkSize, err = envPositiveInt("CHUNK_SIZE", DefaultChunkSize); err != nil {
		return Config{}, err
	}

	if cfg.ChunkOverlap, err = envNonNegativeInt("CHUNK_OVERLAP", DefaultChunkOverlap); err != nil {
		return Config{}, err
	}

	if cfg.TopK, err = envPositiveInt("TOP_K", DefaultTopK); err != nil {
		return Config{}, err
	}

	if cfg.FinalK, err = envPositiveInt("FINAL_K", DefaultFinalK); err != nil {
		return Config{}, err
	}

	if cfg.ExpandLimit, err = envPositiveInt("EXPAND_LIMIT", DefaultExpandLimit); err != nil {
		return Config{}, err
	}

	if cfg.EmbedWorkers, err = envPositiveInt("EMBED_WORKERS", DefaultEmbedWorkers); err != nil {
		return Config{}, err
	}

	if cfg.EmbedRetries, err = envNonNegativeInt("EMBED_RETRIES", DefaultEmbedRetries); err != nil {
		return Config{}, err
	}

	if cfg.MinSimilarity, err = envUnitFloat("MIN_SIMILARITY", DefaultMinSimilarity); err != nil {
		return Config{}, err
	}

	if cfg.LexicalRerank, err = envBool("EVAL_LEXICAL_RERANK", DefaultLexicalRerank); err != nil {
		return Config{}, err
	}

	if cfg.LLMRerank, err = envBool("EVAL_LLM_RERANK", DefaultLLMRerank); err != nil {
		return Config{}, err
	}

	if cfg.RagLLMRerank, err = envBool("RAG_LLM_RERANK", false); err != nil {
		return Config{}, err
	}

	if cfg.AnswerabilityGate, err = envBool("EVAL_ANSWERABILITY_GATE", DefaultAnswerabilityGate); err != nil {
		return Config{}, err
	}

	if cfg.FactJudge, err = envBool("EVAL_FACT_JUDGE", DefaultFactJudge); err != nil {
		return Config{}, err
	}

	if cfg.RewriteOnly, err = envBool("EVAL_REWRITE_ONLY", DefaultRewriteOnly); err != nil {
		return Config{}, err
	}

	if cfg.RagAnswerabilityGate, err = envBool("RAG_ANSWERABILITY_GATE", DefaultRagAnswerabilityGate); err != nil {
		return Config{}, err
	}

	if cfg.RequestTimeout, err = envPositiveDuration("REQUEST_TIMEOUT", DefaultRequestTimeout); err != nil {
		return Config{}, err
	}

	if cfg.QueryRewrite, err = envBool("QUERY_REWRITE", DefaultQueryRewrite); err != nil {
		return Config{}, err
	}

	// Chunk overlap must leave the window advancing: a step of chunkSize-overlap
	// has to stay positive, so overlap < chunkSize.
	if cfg.ChunkOverlap >= cfg.ChunkSize {
		return Config{}, fmt.Errorf(
			"CHUNK_OVERLAP (%d) must be smaller than CHUNK_SIZE (%d)",
			cfg.ChunkOverlap,
			cfg.ChunkSize,
		)
	}

	return cfg, nil
}

// chunkerConfigVersion identifies the serialization format of ChunkerConfig.
// Bump it when the chunking algorithm changes in a way that invalidates stored
// chunkings, so previously ingested documents are re-ingested.
const chunkerConfigVersion = "v1"

// ChunkerConfig returns a stable, canonical representation of the chunker
// settings derived from ChunkSize and ChunkOverlap. Two Configs with the same
// size and overlap produce the same string; changing either produces a
// different one. It is recorded as ingestion provenance and compared on
// re-ingest to decide whether a document must be chunked again.
func (c Config) ChunkerConfig() string {
	return fmt.Sprintf("chunker=%s;size=%d;overlap=%d", chunkerConfigVersion, c.ChunkSize, c.ChunkOverlap)
}

// providerClient builds the provider settings the registry needs to construct a
// vendor client.
func (c Config) providerClient() provider.Client {
	return provider.Client{
		OllamaURL:      c.OllamaURL,
		EmbedModel:     c.EmbedModel,
		ChatModel:      c.ChatModel,
		RequestTimeout: c.RequestTimeout,
	}
}

// Embedder resolves the configured EMBED_PROVIDER through the provider registry
// and returns an embedding.Embedder. An empty provider falls back to the ollama
// default; an unregistered provider returns a non-nil error.
func (c Config) Embedder() (embedding.Embedder, error) {
	return provider.Embedder(c.EmbedProvider, c.providerClient())
}

// Generator resolves the configured GEN_PROVIDER through the provider registry
// and returns a generation.Generator. An empty provider falls back to the ollama
// default; an unregistered provider returns a non-nil error.
func (c Config) Generator() (generation.Generator, error) {
	return provider.Generator(c.GenProvider, c.providerClient())
}

// OllamaClient returns a client for the configured Ollama server.
func (c Config) OllamaClient() *ollama.Client {
	return ollama.New(c.OllamaURL, &http.Client{Timeout: c.RequestTimeout})
}

// Connect opens a connection to the configured database.
func (c Config) Connect(ctx context.Context) (*pgx.Conn, error) {
	conn, err := pgx.Connect(ctx, c.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	return conn, nil
}

// envValue returns the trimmed value of key and whether it is present. An unset
// or blank variable counts as absent, so an empty .env entry uses the default.
func envValue(key string) (string, bool) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", false
	}

	return value, true
}

func envOrDefault(key, fallback string) string {
	if value, ok := envValue(key); ok {
		return value
	}

	return fallback
}

// envPositiveInt reads an integer setting that must be greater than zero.
func envPositiveInt(key string, fallback int) (int, error) {
	raw, ok := envValue(key)
	if !ok {
		return fallback, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", key, raw)
	}

	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0, got %d", key, value)
	}

	return value, nil
}

// envNonNegativeInt is envPositiveInt but accepts zero, for settings where zero
// is a valid value rather than an absent one (chunk overlap).
func envNonNegativeInt(key string, fallback int) (int, error) {
	raw, ok := envValue(key)
	if !ok {
		return fallback, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", key, raw)
	}

	if value < 0 {
		return 0, fmt.Errorf("%s must be 0 or greater, got %d", key, value)
	}

	return value, nil
}

// envUnitFloat reads a similarity-style setting that must lie within [0, 1].
func envUnitFloat(key string, fallback float64) (float64, error) {
	raw, ok := envValue(key)
	if !ok {
		return fallback, nil
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number, got %q", key, raw)
	}

	if math.IsNaN(value) || value < 0 || value > 1 {
		return 0, fmt.Errorf("%s must be between 0 and 1, got %q", key, raw)
	}

	return value, nil
}

// envPositiveDuration reads a duration setting that must be greater than zero.
func envPositiveDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw, ok := envValue(key)
	if !ok {
		return fallback, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration such as 30s or 5m, got %q", key, raw)
	}

	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0, got %q", key, raw)
	}

	return value, nil
}

// envBool reads a boolean setting.
func envBool(key string, fallback bool) (bool, error) {
	raw, ok := envValue(key)
	if !ok {
		return fallback, nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean such as true or false, got %q", key, raw)
	}

	return value, nil
}
