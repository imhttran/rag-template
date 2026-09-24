// Package config holds the runtime settings shared by the commands. Every
// setting can be overridden with an environment variable so the code does not
// have to change between machines.
package config

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"rag-template/internal/ollama"
)

const (
	DefaultOllamaURL   = "http://localhost:11434"
	DefaultEmbedModel  = "nomic-embed-text"
	DefaultChatModel   = "qwen3.8:27b-mlx"
	DefaultDatabaseURL = "postgres://rag:rag@127.0.0.1:5433/rag?sslmode=disable"
	DefaultTopK        = 4

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

	// DefaultRagAnswerabilityGate is whether cmd/rag checks answerability with
	// the chat model before answering. On by default.
	DefaultRagAnswerabilityGate = true

	// DefaultFinalK is how many fused candidates cmd/rag keeps after hybrid
	// retrieval, before the matched sections are expanded.
	DefaultFinalK = 2

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
	TopK                 int
	FinalK               int
	ExpandLimit          int
	MinSimilarity        float64
	LexicalRerank        bool
	LLMRerank            bool
	AnswerabilityGate    bool
	FactJudge            bool
	RagAnswerabilityGate bool
	RequestTimeout       time.Duration
}

// Load reads the settings from the environment, falling back to the defaults.
func Load() Config {
	return Config{
		OllamaURL:   envOrDefault("OLLAMA_URL", DefaultOllamaURL),
		EmbedModel:  envOrDefault("OLLAMA_EMBED_MODEL", DefaultEmbedModel),
		ChatModel:   envOrDefault("OLLAMA_CHAT_MODEL", DefaultChatModel),
		DatabaseURL: envOrDefault("DATABASE_URL", DefaultDatabaseURL),
		// Question has no default: the rag command requires one.
		Question:             envOrDefault("QUESTION", ""),
		TopK:                 envIntOrDefault("TOP_K", DefaultTopK),
		FinalK:               envIntOrDefault("FINAL_K", DefaultFinalK),
		ExpandLimit:          envIntOrDefault("EXPAND_LIMIT", DefaultExpandLimit),
		MinSimilarity:        envFloatOrDefault("MIN_SIMILARITY", DefaultMinSimilarity),
		LexicalRerank:        envBoolOrDefault("EVAL_LEXICAL_RERANK", DefaultLexicalRerank),
		LLMRerank:            envBoolOrDefault("EVAL_LLM_RERANK", DefaultLLMRerank),
		AnswerabilityGate:    envBoolOrDefault("EVAL_ANSWERABILITY_GATE", DefaultAnswerabilityGate),
		FactJudge:            envBoolOrDefault("EVAL_FACT_JUDGE", DefaultFactJudge),
		RagAnswerabilityGate: envBoolOrDefault("RAG_ANSWERABILITY_GATE", DefaultRagAnswerabilityGate),
		RequestTimeout:       envDurationOrDefault("REQUEST_TIMEOUT", DefaultRequestTimeout),
	}
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

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return fallback
}

func envIntOrDefault(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}

	return value
}

func envFloatOrDefault(key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(key)), 64)
	if err != nil {
		return fallback
	}

	return value
}

func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}

	return value
}

func envBoolOrDefault(key string, fallback bool) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(key)))
	if err != nil {
		return fallback
	}

	return value
}
