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

	queryEmbedding, err := embedQuestion(ctx, embedder, question)
	if err != nil {
		return err
	}

	conn, err := cfg.Connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	documents, err := retrieveDocuments(
		ctx,
		conn,
		question,
		queryEmbedding,
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
// kept or filtered out, and returns the documents that passed the similarity
// threshold.
func retrieveDocuments(
	ctx context.Context,
	conn *pgx.Conn,
	question string,
	vector []float64,
	cfg config.Config,
) ([]retrieval.Document, error) {
	retriever := retrieval.New(conn)

	result, err := retrieval.HybridRetrieve(
		ctx,
		retriever,
		question,
		vector,
		retrieval.PipelineOptions{
			CandidateK:    cfg.TopK,
			FinalK:        cfg.FinalK,
			ExpandLimit:   cfg.ExpandLimit,
			MinSimilarity: cfg.MinSimilarity,
		},
	)
	if err != nil {
		return nil, err
	}

	printRetrievalStages(
		result.Vector,
		result.Fused,
		result.Expanded,
		cfg.MinSimilarity,
	)

	return result.Expanded, nil
}

// printRetrievalStages reports each stage of hybrid retrieval.
func printRetrievalStages(
	vectorDocuments []retrieval.Document,
	fused []retrieval.Document,
	expanded []retrieval.Document,
	minSimilarity float64,
) {
	fmt.Println()
	fmt.Println("Vector retrieval:")
	printRetrieved(vectorDocuments, minSimilarity)

	fmt.Println()
	fmt.Println("Hybrid retrieval:")
	printHybridRetrieved(fused)

	fmt.Println()
	fmt.Println("Expanded context:")
	printExpandedDocuments(expanded)
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
