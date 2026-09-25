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

	"github.com/jackc/pgx/v5"

	"rag-template/internal/answerability"
	"rag-template/internal/config"
	"rag-template/internal/embedding"
	"rag-template/internal/generation"
	"rag-template/internal/rag"
	"rag-template/internal/reranking"
	"rag-template/internal/retrieval"
)

func main() {
	cfg := config.Load()

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
	fmt.Println("Question:")
	fmt.Println(question)

	ctx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
	defer cancel()

	client := cfg.OllamaClient()
	embedder := embedding.New(client, cfg.EmbedModel)
	generator := generation.New(client, cfg.ChatModel)

	originalEmbedding, err := embedQuestion(
		ctx,
		embedder,
		question,
	)
	if err != nil {
		return err
	}

	retrievalQuery := question

	var rewrittenEmbedding []float64

	if cfg.QueryRewrite {
		retrievalQuery, err = rag.Rewrite(
			ctx,
			generator,
			question,
		)
		if err != nil {
			return err
		}

		fmt.Println()
		fmt.Println("Retrieval query:")
		fmt.Println(retrievalQuery)

		rewrittenEmbedding, err = embedQuestion(
			ctx,
			embedder,
			retrievalQuery,
		)
		if err != nil {
			return err
		}
	}

	conn, err := cfg.Connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

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

	if !answerable {
		fmt.Println()
		fmt.Println("Answer:")
		fmt.Println("I do not have enough information.")

		return nil
	}

	// Print exactly what rag.Answer sends, so the debug output and the request
	// cannot drift apart.
	fmt.Println()
	fmt.Println("Context being sent to the LLM:")
	fmt.Println(rag.FormatContext(documents))

	answer, err := rag.Answer(
		ctx,
		generator,
		question,
		documents,
	)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Answer:")
	fmt.Println(answer)

	return nil
}

// isAnswerable reports whether documents can answer question: false when nothing
// passed the similarity filter, otherwise the judge decides when gate is set.
func isAnswerable(
	ctx context.Context,
	gate bool,
	generator *generation.Generator,
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
	embedder *embedding.Embedder,
	question string,
) ([]float64, error) {
	queryEmbedding, err := embedder.Embed(ctx, question)
	if err != nil {
		return nil, err
	}

	fmt.Println()
	fmt.Printf(
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
	generator *generation.Generator,
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
	fmt.Println()
	fmt.Println(label)
	printRetrieved(documents, minSimilarity)
}

func printKeywordStage(label string, documents []retrieval.Document) {
	fmt.Println()
	fmt.Println(label)
	printKeywordRetrieved(documents)
}

func printKeywordRetrieved(documents []retrieval.Document) {
	for _, doc := range documents {
		fmt.Printf(
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
		fmt.Printf(
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
			fmt.Printf("FILTERED  %.4f  %s\n", doc.Similarity, doc.Content)

			continue
		}

		fmt.Printf(
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
		fmt.Printf(
			"[%s - %s - chunk %d] %s\n",
			doc.Source,
			doc.Section,
			doc.ChunkIndex,
			doc.Content,
		)
	}
}

func printFusedAndExpanded(fused []retrieval.Document, expanded []retrieval.Document) {
	fmt.Println()
	fmt.Println("Fused retrieval:")
	printHybridRetrieved(fused)

	fmt.Println()
	fmt.Println("Expanded context:")
	printExpandedDocuments(expanded)
}

// rerankAndExpand optionally reranks the candidates with the model, then expands
// the surviving sections. With reranking off it returns expanded unchanged.
func rerankAndExpand(
	ctx context.Context,
	retriever *retrieval.Retriever,
	generator *generation.Generator,
	question string,
	cfg config.Config,
	candidates []retrieval.Document,
	expanded []retrieval.Document,
) ([]retrieval.Document, error) {
	if !cfg.RagLLMRerank {
		return expanded, nil
	}

	reranked, err := reranking.RerankLLM(
		ctx,
		generator,
		question,
		candidates,
	)
	if err != nil {
		return nil, err
	}

	reranked = reranked[:min(len(reranked), cfg.FinalK)]

	rerankedExpanded, err := retriever.SectionChunks(
		ctx,
		retrieval.SectionKeys(reranked),
		cfg.ExpandLimit,
	)
	if err != nil {
		return nil, err
	}

	fmt.Println()
	fmt.Println("Semantic reranking:")
	printHybridRetrieved(reranked)

	fmt.Println()
	fmt.Println("Reranked expanded context:")
	printExpandedDocuments(rerankedExpanded)

	return rerankedExpanded, nil
}
