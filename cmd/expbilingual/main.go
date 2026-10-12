// Command expbilingual is an EVALUATION-ONLY experiment harness for RAG-024 RQ-006.
//
// It runs retrieval arms over a frozen evaluation split against an already-ingested
// disposable database and prints per-case and aggregate metrics. It does NOT modify
// production retrieval. Query generation is pinned (temperature 0, fixed seed, bounded
// output) and cached by case id + configuration so repeated evaluations isolate
// retrieval determinism from generation variability.
//
// Arms:
//
//	A  - baseline (original query)
//	B  - bilingual expansion (fused)
//	C  - translation (fused)
//	D1 - C + original-query rejection guard
//	D2 - C + original-query guard + baseline-first ranking (original top-1 pinned at rank 1)
//	E  - C + cross-language agreement (>=2 of 3 query vector paths clear the floor)
//
// Env: ARM=A|B|C|D1|D2|E|all, REPEATS, DIAG=1, QUERY_CACHE=<path>,
// GEN_TIMEOUT=<seconds>, OLLAMA_CHAT_MODEL, OLLAMA_EMBED_MODEL, EVAL_DATASET, MIN_SIMILARITY.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"rag-template/internal/config"
	"rag-template/internal/embedding"
	"rag-template/internal/ollama"
	"rag-template/internal/retrieval"
)

type expectedDoc struct {
	Source   string   `json:"source"`
	Section  string   `json:"section"`
	Evidence []string `json:"evidence"`
}

type evalCase struct {
	Kind     string        `json:"kind"`
	ID       string        `json:"id"`
	Question string        `json:"question"`
	Expected []expectedDoc `json:"expected"`
}

type options struct {
	candidateK  int
	finalK      int
	expandLimit int
	minSim      float64
}

func (o options) pipeline() retrieval.PipelineOptions {
	return retrieval.PipelineOptions{
		CandidateK:    o.candidateK,
		FinalK:        o.finalK,
		ExpandLimit:   o.expandLimit,
		MinSimilarity: o.minSim,
	}
}

// decoding is the pinned generation configuration (recorded in the header).
var decoding = map[string]any{
	"temperature": 0,
	"seed":        0,
	"num_predict": 64,
}

type cacheEntry struct {
	Question string            `json:"question"`
	Variants map[string]string `json:"variants"`
}

type cacheData struct {
	Model   string                 `json:"model"`
	Entries map[string]*cacheEntry `json:"entries"`
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "expbilingual: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	arm := strings.ToUpper(strings.TrimSpace(envOr("ARM", "all")))
	dataset := envOr("EVAL_DATASET", "evals/retrieval-vi-en-expanded.json")
	embedModel := envOr("OLLAMA_EMBED_MODEL", "nomic-embed-text")
	chatModel := envOr("OLLAMA_CHAT_MODEL", "llama3.2:3b")
	cachePath := envOr("QUERY_CACHE", "docs/rq-006-query-cache.json")
	repeats := envInt("REPEATS", 1)
	diag := envOr("DIAG", "") == "1"
	genTimeout := time.Duration(envInt("GEN_TIMEOUT", 60)) * time.Second
	opts := options{candidateK: 4, finalK: 3, expandLimit: 20, minSim: cfg.MinSimilarity}

	fmt.Printf("HEADER arm=%s dataset=%s embed=%s chat=%s floor=%.2f repeats=%d gen_options=%v\n",
		arm, dataset, embedModel, chatModel, opts.minSim, repeats, decoding)

	raw, err := os.ReadFile(dataset)
	if err != nil {
		return err
	}

	var cases []evalCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		return err
	}

	conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	client := cfg.OllamaClient()
	embedder := embedding.New(client, embedModel)
	searcher := retrieval.New(conn)

	cache := loadCache(cachePath, chatModel)

	arms := []string{"A", "D1", "D2"}
	if arm != "ALL" {
		arms = strings.Split(arm, ",")
	}

	start := time.Now()
	generated := 0

	for _, a := range arms {
		for rep := 1; rep <= repeats; rep++ {
			for _, c := range cases {
				n, err := runCase(ctx, searcher, embedder, client, chatModel, cache, genTimeout, opts, a, rep, c, diag)
				if err != nil {
					return err
				}
				generated += n
			}
		}
	}

	saveCache(cachePath, chatModel, cache)

	fmt.Printf("FOOTER wall=%s generated_queries=%d cache=%s\n", time.Since(start).Round(time.Millisecond), generated, cachePath)
	return nil
}

func runCase(
	ctx context.Context,
	searcher retrieval.Searcher,
	embedder embedding.Embedder,
	client *ollama.Client,
	chatModel string,
	cache *cacheData,
	genTimeout time.Duration,
	opts options,
	arm string,
	rep int,
	c evalCase,
	diag bool,
) (int, error) {
	qvec, err := embedder.Embed(ctx, c.Question)
	if err != nil {
		return 0, fmt.Errorf("embed %s: %w", c.ID, err)
	}

	original, err := retrieval.HybridRetrieve(ctx, searcher, c.Question, qvec, opts.pipeline())
	if err != nil {
		return 0, err
	}

	generated := 0
	orig := [][]retrieval.Document{original.Vector, original.Keyword}
	var rankings [][]retrieval.Document = orig
	vecPathsClear := 0
	if len(original.Vector) > 0 {
		vecPathsClear = 1
	}

	if arm == "B" || arm == "C" || arm == "D1" || arm == "D2" || arm == "E" {
		exp, n, err := generate(ctx, client, chatModel, cache, genTimeout, c, "exp", expansionPrompt(c.Question))
		if err != nil {
			return generated, err
		}
		generated += n
		expVec, err := embedder.Embed(ctx, exp)
		if err != nil {
			return generated, err
		}
		expRes, err := retrieval.HybridRetrieve(ctx, searcher, strings.TrimSpace(exp), expVec, opts.pipeline())
		if err != nil {
			return generated, err
		}

		vi, n, err := generate(ctx, client, chatModel, cache, genTimeout, c, "vi", translationPrompt(c.Question, "Vietnamese"))
		if err != nil {
			return generated, err
		}
		generated += n
		en, n, err := generate(ctx, client, chatModel, cache, genTimeout, c, "en", translationPrompt(c.Question, "English"))
		if err != nil {
			return generated, err
		}
		generated += n
		viVec, err := embedder.Embed(ctx, vi)
		if err != nil {
			return generated, err
		}
		enVec, err := embedder.Embed(ctx, en)
		if err != nil {
			return generated, err
		}
		viRes, err := retrieval.HybridRetrieve(ctx, searcher, strings.TrimSpace(vi), viVec, opts.pipeline())
		if err != nil {
			return generated, err
		}
		enRes, err := retrieval.HybridRetrieve(ctx, searcher, strings.TrimSpace(en), enVec, opts.pipeline())
		if err != nil {
			return generated, err
		}
		if len(viRes.Vector) > 0 {
			vecPathsClear++
		}
		if len(enRes.Vector) > 0 {
			vecPathsClear++
		}

		switch arm {
		case "B":
			rankings = append(append([][]retrieval.Document{}, orig...), expRes.Vector, expRes.Keyword)
		case "C", "D1", "D2":
			rankings = append(append([][]retrieval.Document{}, orig...),
				viRes.Vector, viRes.Keyword, enRes.Vector, enRes.Keyword, expRes.Vector, expRes.Keyword)
		case "E":
			rankings = append(append([][]retrieval.Document{}, orig...),
				viRes.Vector, viRes.Keyword, enRes.Vector, enRes.Keyword, expRes.Vector, expRes.Keyword)
		}
	}

	rejected := len(c.Expected) == 0
	guardOK := true
	switch arm {
	case "D1", "D2":
		guardOK = len(original.Vector) > 0
	case "E":
		guardOK = vecPathsClear >= 2
	}

	fusedAll := retrieval.DeduplicateSections(retrieval.FuseRankings(rankings, 1000))

	var fused []retrieval.Document
	switch arm {
	case "D2":
		if guardOK {
			fused = baselineFirst(orig, fusedAll)
		}
	default:
		fused = fusedAll
	}

	if rejected && !guardOK {
		fused = nil
	}

	if diag {
		fmt.Printf("DIAG arm=%s rep=%d id=%s kind=%s guardOK=%v candidates=%d rejected=%v\n", arm, rep, c.ID, c.Kind, guardOK, len(fused), rejected)
		for i, d := range fused {
			fmt.Printf("DIAG   #%d sim=%.4f %s / %s\n", i+1, d.Similarity, d.Source, d.Section)
		}
	}

	r1 := recallAtK(c.Expected, fused, 1)
	r4 := recallAtK(c.Expected, fused, 4)
	rej := "n/a"
	if rejected {
		if len(fused) == 0 {
			rej = "reject"
		} else {
			rej = "FALSE_POSITIVE"
		}
	}

	// Evidence follows cmd/eval FinalK semantics: expand only the finalK fused sections.
	evidence := 0.0
	if !rejected {
		finalSections := fused
		if len(finalSections) > opts.finalK {
			finalSections = finalSections[:opts.finalK]
		}
		expanded, err := searcher.SectionChunksFiltered(
			ctx, retrieval.SectionKeys(finalSections), opts.expandLimit, retrieval.Filter{},
		)
		if err != nil {
			return generated, err
		}
		evidence = evidenceRecall(c.Expected, expanded)
	}

	fmt.Printf("CASE arm=%s rep=%d id=%s kind=%s r1=%.2f r4=%.2f evidence=%.2f %s\n",
		arm, rep, c.ID, c.Kind, r1, r4, evidence, rej)
	return generated, nil
}

// baselineFirst pins the original query's top fused section at rank 1, then fills
// the remaining ranks (through 4) from the fused translated ranking.
func baselineFirst(orig [][]retrieval.Document, fusedAll []retrieval.Document) []retrieval.Document {
	out := retrieval.DeduplicateSections(retrieval.FuseRankings(orig, 1))

	if len(out) == 0 {
		return fusedAll
	}

	for _, d := range fusedAll {
		if len(out) >= 4 {
			break
		}
		if d.Source == out[0].Source && d.Section == out[0].Section {
			continue
		}
		dup := false
		for _, e := range out {
			if e.Source == d.Source && e.Section == d.Section {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, d)
		}
	}
	return out
}

func generate(
	ctx context.Context,
	client *ollama.Client,
	model string,
	cache *cacheData,
	timeout time.Duration,
	c evalCase,
	variant string,
	prompt string,
) (string, int, error) {
	if entry, ok := cache.Entries[c.ID]; ok && entry.Question == c.Question {
		if v, ok := entry.Variants[variant]; ok && strings.TrimSpace(v) != "" {
			fmt.Printf("GEN id=%s variant=%s source=cache\n", c.ID, variant)
			return v, 0, nil
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request := map[string]any{
		"model":   model,
		"prompt":  prompt,
		"stream":  false,
		"options": decoding,
	}

	var response struct {
		Response string `json:"response"`
	}
	if err := client.PostJSON(callCtx, "/api/generate", request, &response); err != nil {
		return "", 0, fmt.Errorf("generate %s/%s: %w", c.ID, variant, err)
	}

	text := strings.TrimSpace(response.Response)
	fmt.Printf("GEN id=%s variant=%s source=model\n", c.ID, variant)

	entry, ok := cache.Entries[c.ID]
	if !ok || entry.Question != c.Question {
		entry = &cacheEntry{Question: c.Question, Variants: map[string]string{}}
		cache.Entries[c.ID] = entry
	}
	entry.Variants[variant] = text

	return text, 1, nil
}

func loadCache(path, model string) *cacheData {
	cache := &cacheData{Model: model, Entries: map[string]*cacheEntry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return cache
	}
	var loaded cacheData
	if err := json.Unmarshal(data, &loaded); err != nil || loaded.Entries == nil {
		return cache
	}
	if loaded.Model != "" && loaded.Model != model {
		fmt.Printf("CACHE model mismatch (cached=%s requested=%s); starting fresh\n", loaded.Model, model)
		return cache
	}
	return &loaded
}

func saveCache(path, model string, cache *cacheData) {
	cache.Model = model
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

func recallAtK(expected []expectedDoc, docs []retrieval.Document, k int) float64 {
	if len(expected) == 0 {
		return 0
	}
	if k > len(docs) {
		k = len(docs)
	}
	top := docs[:k]
	found := 0
	for _, e := range expected {
		for _, d := range top {
			if d.Source == e.Source && d.Section == e.Section {
				found++
				break
			}
		}
	}
	return float64(found) / float64(len(expected))
}

func evidenceRecall(expected []expectedDoc, docs []retrieval.Document) float64 {
	total, found := 0, 0
	for _, e := range expected {
		for _, ev := range e.Evidence {
			total++
			for _, d := range docs {
				if d.Source != e.Source || d.Section != e.Section {
					continue
				}
				if strings.Contains(strings.ToLower(d.Content), strings.ToLower(ev)) {
					found++
					break
				}
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(found) / float64(total)
}

func expansionPrompt(question string) string {
	return "Rewrite the user's question into a short bilingual search query that contains key terms " +
		"in BOTH English and Vietnamese, so it retrieves matching passages in either language. " +
		"Return only the query, no quotes or explanation.\n\nQUESTION:\n" + question + "\n\nQUERY:"
}

func translationPrompt(question, language string) string {
	return "Translate the following question into " + language + ". " +
		"Return only the translation, with no quotes, labels, or explanation.\n\nQUESTION:\n" + question + "\n\nTRANSLATION:"
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			return fallback
		}
		n = n*10 + int(r-'0')
	}
	if n < 1 {
		return fallback
	}
	return n
}
