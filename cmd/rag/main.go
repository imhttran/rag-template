// Command rag answers a question from a local document store using
// Retrieval-Augmented Generation.
//
// It embeds the question with Ollama, retrieves the closest chunks from
// PostgreSQL (pgvector), and asks a local model to answer using only those
// chunks as context.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"rag-template/internal/answerability"
	"rag-template/internal/config"
	"rag-template/internal/contextbudget"
	"rag-template/internal/embedding"
	"rag-template/internal/generation"
	"rag-template/internal/ingestion"
	"rag-template/internal/observability"
	"rag-template/internal/rag"
	"rag-template/internal/reranking"
	"rag-template/internal/retrieval"
)

// out is where the human-readable stage output is written. It defaults to
// stdout so the default workflow is byte-for-byte unchanged. When JSON
// observability is selected, run points it at io.Discard so the structured
// record on stdout stays machine-parseable.
var out io.Writer = os.Stdout

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	question, err := resolveQuestion(
		cfg.Question,
		os.Args[1:],
		os.Stdin,
		isTerminal(os.Stdin),
	)
	if err != nil {
		log.Fatal(err)
	}

	// A single error path keeps deferred cleanup (closing the connection,
	// cancelling the context) from being skipped by log.Fatal mid-flow.
	if err := run(context.Background(), cfg, question); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, cfg config.Config, question string) error {
	// In JSON mode the human-readable stream is suppressed so stdout carries
	// only the structured record; the default (human) mode is unchanged.
	if cfg.ObservabilityJSON() {
		out = io.Discard
	} else {
		out = os.Stdout
	}

	// Fail fast on an oversized question before any embedding or retrieval work
	// runs. With MAX_QUESTION_BYTES unset (0) the check is skipped and behavior
	// is unchanged.
	limits := rag.InputLimits{
		MaxQuestionBytes: cfg.MaxQuestionBytes,
		MaxInputBytes:    cfg.MaxInputBytes,
	}

	if err := limits.CheckQuestion(question); err != nil {
		return err
	}

	println("Question:")
	println(question)

	// One reporter per invocation. Its run ID correlates every record emitted
	// for this run. The human reporter adds no lines to the stream above, so the
	// default output is unchanged; the JSON reporter emits one structured record
	// at the end.
	reporter := observability.New(cfg.ObservabilityJSON(), os.Stdout)
	defer reporter.Finish()

	ctx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
	defer cancel()

	client := cfg.OllamaClient()
	embedder := embedding.New(client, cfg.EmbedModel)
	generator := generation.New(client, cfg.ChatModel)

	conn, err := cfg.Connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	// Fail fast when EMBED_DIM does not match the stored documents.embedding
	// column, before the question is embedded or anything is retrieved. On the
	// default 768 path this is a silent no-op.
	if err := ingestion.CheckEmbeddingDim(ctx, conn, cfg.EmbedDim); err != nil {
		return err
	}

	embedStart := time.Now()

	originalEmbedding, err := embedQuestion(
		ctx,
		embedder,
		question,
	)
	if err != nil {
		return err
	}

	reporter.Stage(observability.StageEmbed, time.Since(embedStart))

	retrievalQuery := question

	var rewrittenEmbedding []float64

	if cfg.QueryRewrite {
		rewriteStart := time.Now()

		retrievalQuery, err = rag.Rewrite(
			ctx,
			generator,
			question,
		)
		if err != nil {
			return err
		}

		reporter.Stage(observability.StageRewrite, time.Since(rewriteStart))

		println()
		println("Retrieval query:")
		println(retrievalQuery)

		rewrittenEmbedding, err = embedQuestion(
			ctx,
			embedder,
			retrievalQuery,
		)
		if err != nil {
			return err
		}
	}

	retrieveStart := time.Now()

	documents, err := retrieveDocuments(
		ctx,
		conn,
		generator,
		question,
		retrievalQuery,
		originalEmbedding,
		rewrittenEmbedding,
		cfg,
	)
	if err != nil {
		return err
	}

	reporter.Stage(observability.StageRetrieve, time.Since(retrieveStart))

	// Counts describe the retrieval output: KEPT documents cleared the similarity
	// floor printRetrieved applies; FILTERED are below it (including chunks
	// recovered only by section expansion, which carry no similarity score).
	// Usage.Chunks below reports the final, post-budget context size instead.
	reporter.Counts(countRetrieved(documents, cfg.MinSimilarity))

	// Apply the deterministic context budget to the expanded context. A budget
	// of 0 (the default) disables the builder and leaves the documents
	// unchanged, preserving the current chunk-count behaviour. The budgeted set
	// is the one the answerability gate judges, the one printed, and the one the
	// model answers from, so those views cannot drift apart.
	budgetStart := time.Now()

	documents = contextbudget.Build(documents, cfg.ContextBudget)

	reporter.Stage(observability.StageBudget, time.Since(budgetStart))

	// Token/context usage uses the RAG-010 provider-neutral byte estimate over
	// the same documents that are printed and sent to rag.Answer.
	reporter.Usage(observability.Usage{
		ContextBytes: contextbudget.EstimateAll(documents),
		Chunks:       len(documents),
	})

	// Fail fast on oversized retrieved context before any model call that would
	// use it. With MAX_INPUT_BYTES unset (0) the check is skipped and behavior is
	// unchanged.
	if err := limits.CheckContext(documents); err != nil {
		return err
	}

	answerabilityStart := time.Now()

	answerable, err := isAnswerable(
		ctx,
		cfg.RagAnswerabilityGate,
		generator,
		question,
		documents,
	)
	if err != nil {
		return err
	}

	reporter.Stage(observability.StageAnswerability, time.Since(answerabilityStart))

	if !answerable {
		println()
		println("Answer:")
		println("I do not have enough information.")

		return nil
	}

	// Print exactly what rag.Answer sends, so the debug output and the request
	// cannot drift apart.
	println()
	println("Context being sent to the LLM:")
	println(rag.FormatContext(documents))

	answerStart := time.Now()

	answer, citationOutcome, err := rag.AnswerValidated(
		ctx,
		generator,
		question,
		documents,
		cfg.RagCitationValidation,
	)
	if err != nil {
		return err
	}

	reporter.Stage(observability.StageAnswer, time.Since(answerStart))

	// Report the citation validation/repair outcome only when validation ran, so
	// the default (validation disabled) output is unchanged. The report goes
	// through the same human stream as the other stage lines; nothing new is
	// emitted when the setting is off.
	if citationOutcome.Enabled {
		println()
		printf(
			"Citation validation: valid=%d/%d repaired=%t stripped=%d\n",
			citationOutcome.Valid,
			citationOutcome.Total,
			citationOutcome.Repaired,
			citationOutcome.Stripped,
		)
	}

	println()
	println("Answer:")
	println(answer)

	return nil
}

// println writes a line to the configured human output writer, matching the
// behavior of fmt.Println on stdout in the default mode.
func println(args ...any) {
	fmt.Fprintln(out, args...)
}

// printf writes formatted output to the configured human output writer.
func printf(format string, args ...any) {
	fmt.Fprintf(out, format, args...)
}

// countRetrieved tallies the final context by whether each document cleared the
// similarity floor printRetrieved applies (doc.Similarity < minSimilarity is
// filtered). Retrieved is the number of documents at or above the floor;
// Filtered is the number below it, including chunks recovered only by section
// expansion (whose similarity score is not set). It is a retrieval-output tally,
// computed before the optional budget/rerank, so it does not describe the final
// model context (Usage.Chunks does).
func countRetrieved(documents []retrieval.Document, minSimilarity float64) observability.Counts {
	counts := observability.Counts{}

	for _, doc := range documents {
		if doc.Similarity < minSimilarity {
			counts.Filtered++

			continue
		}

		counts.Retrieved++
	}

	return counts
}

// isAnswerable reports whether documents can answer question: false when nothing
// passed the similarity filter, otherwise the judge decides when gate is set.
func isAnswerable(
	ctx context.Context,
	gate bool,
	generator generation.Generator,
	question string,
	documents []retrieval.Document,
) (bool, error) {
	if len(documents) == 0 {
		return false, nil
	}

	if !gate {
		return true, nil
	}

	return answerability.New(generator).IsAnswerable(
		ctx,
		question,
		documents,
	)
}

// isTerminal reports whether f is attached to a terminal.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}

// resolveQuestion picks the question to ask: a command-line argument, the
// QUESTION setting, or a line typed on standard input. The prompt is only
// printed when stdin is a terminal.
func resolveQuestion(
	configured string,
	args []string,
	stdin io.Reader,
	interactive bool,
) (string, error) {
	if question := strings.TrimSpace(strings.Join(args, " ")); question != "" {
		return question, nil
	}

	if question := strings.TrimSpace(configured); question != "" {
		return question, nil
	}

	if interactive {
		fmt.Fprint(os.Stderr, "Question: ")
	}

	scanner := bufio.NewScanner(stdin)
	if scanner.Scan() {
		if question := strings.TrimSpace(scanner.Text()); question != "" {
			return question, nil
		}
	}

	return "", errors.New("no question given")
}

// embedQuestion embeds question and reports the resulting vector size.
func embedQuestion(
	ctx context.Context,
	embedder embedding.Embedder,
	question string,
) ([]float64, error) {
	queryEmbedding, err := embedder.Embed(ctx, question)
	if err != nil {
		return nil, err
	}

	println()
	printf(
		"Ollama created a %d-dimensional query vector\n",
		len(queryEmbedding),
	)

	return queryEmbedding, nil
}

// retrieveDocuments searches for the closest documents, reports which were
// kept or filtered out, and returns the context the model will answer from.
func retrieveDocuments(
	ctx context.Context,
	conn *pgx.Conn,
	generator generation.Generator,
	originalQuery string,
	rewrittenQuery string,
	originalVector []float64,
	rewrittenVector []float64,
	cfg config.Config,
) ([]retrieval.Document, error) {
	retriever := retrieval.New(conn)

	options := retrieval.PipelineOptions{
		CandidateK:    cfg.TopK,
		FinalK:        cfg.FinalK,
		ExpandLimit:   cfg.ExpandLimit,
		MinSimilarity: cfg.MinSimilarity,
		// Language selects the full-text search configuration for keyword
		// retrieval. Empty (the default) keeps the baseline 'english' config.
		Language: cfg.Language,
	}

	if cfg.QueryRewrite {
		result, err := retrieval.MultiQueryRetrieve(
			ctx,
			retriever,
			originalQuery,
			rewrittenQuery,
			originalVector,
			rewrittenVector,
			options,
		)
		if err != nil {
			return nil, err
		}

		printMultiQueryStages(result, cfg.MinSimilarity)

		return rerankAndExpand(
			ctx,
			retriever,
			generator,
			originalQuery,
			cfg,
			result.Candidates,
			result.Expanded,
		)
	}

	result, err := retrieval.HybridRetrieve(
		ctx,
		retriever,
		originalQuery,
		originalVector,
		options,
	)
	if err != nil {
		return nil, err
	}

	printSingleQueryStages(result, cfg.MinSimilarity)

	return rerankAndExpand(
		ctx,
		retriever,
		generator,
		originalQuery,
		cfg,
		result.Candidates,
		result.Expanded,
	)
}

// printMultiQueryStages reports each stage of multi-query retrieval.
func printMultiQueryStages(
	result retrieval.MultiQueryResult,
	minSimilarity float64,
) {
	printVectorStage("Original vector retrieval:", result.OriginalVector, minSimilarity)
	printKeywordStage("Original keyword retrieval:", result.OriginalKeyword)
	printVectorStage("Rewritten vector retrieval:", result.RewrittenVector, minSimilarity)
	printKeywordStage("Rewritten keyword retrieval:", result.RewrittenKeyword)

	printFusedAndExpanded(result.Fused, result.Expanded)
}

// printSingleQueryStages reports each stage of single-query retrieval.
func printSingleQueryStages(
	result retrieval.PipelineResult,
	minSimilarity float64,
) {
	printVectorStage("Vector retrieval:", result.Vector, minSimilarity)
	printKeywordStage("Keyword retrieval:", result.Keyword)

	printFusedAndExpanded(result.Fused, result.Expanded)
}

func printVectorStage(
	label string,
	documents []retrieval.Document,
	minSimilarity float64,
) {
	println()
	println(label)
	printRetrieved(documents, minSimilarity)
}

func printKeywordStage(label string, documents []retrieval.Document) {
	println()
	println(label)
	printKeywordRetrieved(documents)
}

func printKeywordRetrieved(documents []retrieval.Document) {
	for _, doc := range documents {
		printf(
			"%.4f  [%s - %s - chunk %d] %s\n",
			doc.KeywordScore,
			doc.Source,
			doc.Section,
			doc.ChunkIndex,
			doc.Content,
		)
	}
}

func printHybridRetrieved(documents []retrieval.Document) {
	for _, doc := range documents {
		printf(
			"RRF %.6f  vector %.4f  keyword %.4f  [%s - %s - chunk %d] %s\n",
			doc.FusionScore,
			doc.Similarity,
			doc.KeywordScore,
			doc.Source,
			doc.Section,
			doc.ChunkIndex,
			doc.Content,
		)
	}
}

// printRetrieved reports which documents were kept or filtered out.
func printRetrieved(documents []retrieval.Document, minSimilarity float64) {
	for _, doc := range documents {
		if doc.Similarity < minSimilarity {
			printf("FILTERED  %.4f  %s\n", doc.Similarity, doc.Content)

			continue
		}

		printf(
			"KEPT      %.4f  [%s - %s - chunk %d] %s\n",
			doc.Similarity,
			doc.Source,
			doc.Section,
			doc.ChunkIndex,
			doc.Content,
		)
	}
}

func printExpandedDocuments(documents []retrieval.Document) {
	for _, doc := range documents {
		printf(
			"[%s - %s - chunk %d] %s\n",
			doc.Source,
			doc.Section,
			doc.ChunkIndex,
			doc.Content,
		)
	}
}

func printFusedAndExpanded(fused []retrieval.Document, expanded []retrieval.Document) {
	println()
	println("Fused retrieval:")
	printHybridRetrieved(fused)

	println()
	println("Expanded context:")
	printExpandedDocuments(expanded)
}

// rerankAndExpand optionally reranks the candidates with the model, then expands
// the surviving sections. With reranking off it returns expanded unchanged.
//
// The reranker is hardened: a parse/validation failure, a generator error, or a
// latency-guard expiry degrades to the fused order (the candidates) instead of
// surfacing an error, so the request still answers. The reranker never returns
// an error for those paths, and this function degrades to the fused order even
// if one is ever returned, so no reranker failure can reach the user.
func rerankAndExpand(
	ctx context.Context,
	retriever *retrieval.Retriever,
	generator generation.Generator,
	question string,
	cfg config.Config,
	candidates []retrieval.Document,
	expanded []retrieval.Document,
) ([]retrieval.Document, error) {
	if !cfg.RagLLMRerank {
		return expanded, nil
	}

	result, err := reranking.RerankLLMWithTimeout(
		ctx,
		generator,
		question,
		candidates,
		cfg.RerankTimeout,
	)
	if err != nil {
		// Degrade to the fused order rather than surfacing an error: reranker
		// failures must never stop the request from answering.
		log.Printf(
			"reranking: falling back to fused order: %v",
			err,
		)

		return expanded, nil
	}

	if result.FellBack {
		println()
		printf(
			"Semantic reranking unavailable (%s); using the fused order.\n",
			result.Reason,
		)

		return expanded, nil
	}

	reranked := result.Documents[:min(len(result.Documents), cfg.FinalK)]

	rerankedExpanded, err := retriever.SectionChunks(
		ctx,
		retrieval.SectionKeys(reranked),
		cfg.ExpandLimit,
	)
	if err != nil {
		return nil, err
	}

	println()
	println("Semantic reranking:")
	printHybridRetrieved(reranked)

	println()
	println("Reranked expanded context:")
	printExpandedDocuments(rerankedExpanded)

	return rerankedExpanded, nil
}
