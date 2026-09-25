package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"

	"rag-template/internal/answerability"
	"rag-template/internal/config"
	"rag-template/internal/embedding"
	"rag-template/internal/generation"
	"rag-template/internal/rag"
	"rag-template/internal/reranking"
	"rag-template/internal/retrieval"
)

type ExpectedDocument struct {
	Source   string   `json:"source"`
	Section  string   `json:"section"`
	Evidence []string `json:"evidence,omitempty"`
}

type EvalCase struct {
	Question      string             `json:"question"`
	Expected      []ExpectedDocument `json:"expected"`
	ExpectedFacts []string           `json:"expected_facts,omitempty"`
}

// stats accumulates the per-case metrics for the final summary.
type stats struct {
	recallTotals          map[int]float64
	precisionTotals       map[int]float64
	hybridRecallTotals    map[int]float64
	hybridPrecisionTotals map[int]float64
	rerankRecall          float64
	rerankPrecision       float64
	llmRecall             float64
	llmPrecision          float64
	answerable            int
	unanswerable          int
	correctRejections     int

	evidenceBefore float64
	evidenceAfter  float64
	evidenceCases  int

	gateCorrectAccepts int
	gateFalseRejects   int
	gateCorrectRejects int
	gateFalseAccepts   int

	factsSupported  int
	factsTotal      int
	groundedAnswers int
	answersJudged   int

	validCitations    int
	totalCitations    int
	entailedCitations int
	judgedCitations   int
}

type citedClaim struct {
	Claim   string
	Source  string
	Section string
}

// evaluator scores cases against a retriever.
type evaluator struct {
	embedder          *embedding.Embedder
	retriever         *retrieval.Retriever
	generator         *generation.Generator
	judge             *answerability.Judge
	lexicalRerank     bool
	llmRerank         bool
	queryRewrite      bool
	rewriteOnly       bool
	answerabilityGate bool
	factJudge         bool
	minSimilarity     float64
	expandLimit       int
	candidateK        int
	finalK            int
	ks                []int
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	cases, err := loadCases()
	if err != nil {
		return err
	}

	cfg := config.Load()

	conn, err := cfg.Connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	client := cfg.OllamaClient()

	eval := evaluator{
		embedder:          embedding.New(client, cfg.EmbedModel),
		retriever:         retrieval.New(conn),
		lexicalRerank:     cfg.LexicalRerank,
		llmRerank:         cfg.LLMRerank,
		queryRewrite:      cfg.QueryRewrite,
		rewriteOnly:       cfg.RewriteOnly,
		answerabilityGate: cfg.AnswerabilityGate,
		factJudge:         cfg.FactJudge,
		minSimilarity:     cfg.MinSimilarity,
		expandLimit:       cfg.ExpandLimit,
		candidateK:        cfg.TopK,
		finalK:            cfg.FinalK,
		ks:                []int{1, 2, cfg.TopK},
	}

	if cfg.AnswerabilityGate ||
		cfg.FactJudge ||
		cfg.LLMRerank ||
		cfg.QueryRewrite {

		eval.generator = generation.New(client, cfg.ChatModel)

		if cfg.AnswerabilityGate || cfg.FactJudge {
			eval.judge = answerability.New(eval.generator)
		}
	}

	stats := stats{
		recallTotals:          make(map[int]float64),
		precisionTotals:       make(map[int]float64),
		hybridRecallTotals:    make(map[int]float64),
		hybridPrecisionTotals: make(map[int]float64),
	}

	for _, evalCase := range cases {
		if err := eval.evaluate(ctx, evalCase, &stats); err != nil {
			return err
		}
	}

	printOverall(eval, stats)

	return nil
}

// loadCases reads and parses the evaluation cases.
func loadCases() ([]EvalCase, error) {
	data, err := os.ReadFile("evals/retrieval.json")
	if err != nil {
		return nil, err
	}

	var cases []EvalCase

	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, err
	}

	fmt.Printf("Loaded %d evaluation cases\n", len(cases))

	return cases, nil
}

// evaluate runs every measurement for one case and folds the results into stats.
func (e evaluator) evaluate(
	ctx context.Context,
	evalCase EvalCase,
	stats *stats,
) error {
	fmt.Println()
	fmt.Printf("Question: %s\n", evalCase.Question)

	var rewrittenQuery string
	var rewrittenVector []float64

	if e.queryRewrite {
		rewritten, err := rag.Rewrite(
			ctx,
			e.generator,
			evalCase.Question,
		)
		if err != nil {
			return err
		}

		rewrittenQuery = rewritten

		fmt.Printf("Retrieval query: %s\n", rewrittenQuery)
	}

	vector, err := e.embedder.Embed(
		ctx,
		evalCase.Question,
	)
	if err != nil {
		return err
	}

	if e.queryRewrite {
		rewrittenVector, err = e.embedder.Embed(
			ctx,
			rewrittenQuery,
		)
		if err != nil {
			return err
		}
	}

	answerable := len(evalCase.Expected) > 0

	if answerable {
		stats.answerable++
	} else {
		stats.unanswerable++
	}

	if _, err := e.baselines(ctx, vector, evalCase, answerable, stats); err != nil {
		return err
	}

	var (
		candidates []retrieval.Document
		expanded   []retrieval.Document
	)

	switch {
	case !e.queryRewrite:
		candidates, expanded, err = e.reportBaselines(
			ctx,
			evalCase,
			answerable,
			stats,
			queryVector{query: evalCase.Question, vector: vector},
		)
	case e.rewriteOnly:
		candidates, expanded, err = e.reportBaselines(
			ctx,
			evalCase,
			answerable,
			stats,
			queryVector{query: rewrittenQuery, vector: rewrittenVector},
		)
	default:
		candidates, expanded, err = e.reportBaselines(
			ctx,
			evalCase,
			answerable,
			stats,
			queryVector{query: evalCase.Question, vector: vector},
			queryVector{query: rewrittenQuery, vector: rewrittenVector},
		)
	}

	if err != nil {
		return err
	}

	if e.answerabilityGate {
		if err := e.checkAnswerability(
			ctx,
			evalCase,
			expanded,
			answerable,
			stats,
		); err != nil {
			return err
		}
	}

	if !answerable {
		return nil
	}

	if !e.lexicalRerank && !e.llmRerank {
		return nil
	}

	return e.rerank(ctx, evalCase, candidates, stats)
}

// queryVector pairs a retrieval query with its embedding.
type queryVector struct {
	query  string
	vector []float64
}

// pipelineOptions builds the retrieval options every baseline uses.
func (e evaluator) pipelineOptions() retrieval.PipelineOptions {
	return retrieval.PipelineOptions{
		CandidateK:    e.candidateK,
		FinalK:        e.finalK,
		ExpandLimit:   e.expandLimit,
		MinSimilarity: e.minSimilarity,
	}
}

// reportBaselines runs the retrieval pipeline for one query (single-query
// retrieval) or two (the original plus the rewritten query), reports its stages,
// and returns the rerank candidates.
func (e evaluator) reportBaselines(
	ctx context.Context,
	evalCase EvalCase,
	answerable bool,
	stats *stats,
	queries ...queryVector,
) ([]retrieval.Document, []retrieval.Document, error) {
	var (
		rankings   [][]retrieval.Document
		fused      []retrieval.Document
		expanded   []retrieval.Document
		candidates []retrieval.Document
	)

	switch len(queries) {
	case 1:
		result, err := retrieval.HybridRetrieve(
			ctx,
			e.retriever,
			queries[0].query,
			queries[0].vector,
			e.pipelineOptions(),
		)
		if err != nil {
			return nil, nil, err
		}

		rankings = [][]retrieval.Document{result.Vector, result.Keyword}
		fused, expanded, candidates = result.Fused, result.Expanded, result.Candidates

	case 2:
		result, err := retrieval.MultiQueryRetrieve(
			ctx,
			e.retriever,
			queries[0].query,
			queries[1].query,
			queries[0].vector,
			queries[1].vector,
			e.pipelineOptions(),
		)
		if err != nil {
			return nil, nil, err
		}

		rankings = [][]retrieval.Document{
			result.OriginalVector,
			result.OriginalKeyword,
			result.RewrittenVector,
			result.RewrittenKeyword,
		}
		fused, expanded, candidates = result.Fused, result.Expanded, result.Candidates

	default:
		return nil, nil, fmt.Errorf(
			"retrieval needs one or two queries, got %d",
			len(queries),
		)
	}

	e.reportFusedBaselines(rankings, evalCase, answerable, stats)

	if err := e.reportEvidenceAndJudge(
		ctx,
		evalCase,
		fused,
		expanded,
		answerable,
		stats,
	); err != nil {
		return nil, nil, err
	}

	return candidates, expanded, nil
}

func hasExpectedEvidence(expected []ExpectedDocument) bool {
	return slices.ContainsFunc(expected, func(doc ExpectedDocument) bool {
		return len(doc.Evidence) > 0
	})
}

// reportFusedBaselines reports recall and precision at every k for rankings,
// fused with RRF, and folds the results into stats.
func (e evaluator) reportFusedBaselines(
	rankings [][]retrieval.Document,
	evalCase EvalCase,
	answerable bool,
	stats *stats,
) {
	for _, k := range e.ks {
		documents := retrieval.FuseRankings(rankings, k)

		if !answerable {
			fmt.Printf("Hybrid K=%d", k)

			if len(documents) == 0 {
				fmt.Println("  Correct rejection")
			} else {
				fmt.Println("  FALSE POSITIVE")
				printRetrieved(documents)
			}

			continue
		}

		recall := recallAtK(evalCase.Expected, documents)
		precision := precisionAtK(evalCase.Expected, documents)

		stats.hybridRecallTotals[k] += recall
		stats.hybridPrecisionTotals[k] += precision

		fmt.Printf(
			"Hybrid K=%d  Recall@%d=%.2f  Precision@%d=%.2f\n",
			k,
			k,
			recall,
			k,
			precision,
		)
	}
}

// reportEvidenceAndJudge reports evidence recall before and after expansion and,
// when the fact judge is on, the generated-answer metrics for one case.
func (e evaluator) reportEvidenceAndJudge(
	ctx context.Context,
	evalCase EvalCase,
	fused []retrieval.Document,
	expanded []retrieval.Document,
	answerable bool,
	stats *stats,
) error {
	if !answerable {
		return nil
	}

	beforeExpansion := evidenceRecall(evalCase.Expected, fused)
	afterExpansion := evidenceRecall(evalCase.Expected, expanded)

	if hasExpectedEvidence(evalCase.Expected) {
		stats.evidenceBefore += beforeExpansion
		stats.evidenceAfter += afterExpansion
		stats.evidenceCases++

		fmt.Printf(
			"Evidence Recall: before expansion=%.2f  after expansion=%.2f\n",
			beforeExpansion,
			afterExpansion,
		)
	}

	if e.factJudge && len(evalCase.ExpectedFacts) > 0 {
		answer, err := rag.Answer(
			ctx,
			e.generator,
			evalCase.Question,
			expanded,
		)
		if err != nil {
			return err
		}

		fmt.Println("Generated answer:")
		fmt.Printf("  %s\n", answer)

		supported, err := e.judge.SupportedAnswerFacts(
			ctx,
			evalCase.Question,
			answer,
			evalCase.ExpectedFacts,
		)
		if err != nil {
			return err
		}

		stats.factsSupported += supported
		stats.factsTotal += len(evalCase.ExpectedFacts)

		fmt.Printf(
			"Generated Fact Recall: %d/%d (%.2f)\n",
			supported,
			len(evalCase.ExpectedFacts),
			float64(supported)/float64(len(evalCase.ExpectedFacts)),
		)

		grounded, err := e.judge.IsGrounded(
			ctx,
			evalCase.Question,
			answer,
			expanded,
		)
		if err != nil {
			return err
		}

		stats.answersJudged++

		if grounded {
			stats.groundedAnswers++
			fmt.Println("Grounded: YES")
		} else {
			fmt.Println("Grounded: NO")
		}

		valid, total := citationValidity(answer, expanded)

		stats.validCitations += valid
		stats.totalCitations += total

		if total > 0 {
			fmt.Printf(
				"Citation Validity: %d/%d (%.2f)\n",
				valid,
				total,
				float64(valid)/float64(total),
			)
		}

		entailed, judged, err := e.checkCitationEntailment(
			ctx,
			answer,
			expanded,
		)
		if err != nil {
			return err
		}

		stats.entailedCitations += entailed
		stats.judgedCitations += judged

		if judged > 0 {
			fmt.Printf(
				"Citation Entailment: %d/%d (%.2f)\n",
				entailed,
				judged,
				float64(entailed)/float64(judged),
			)
		}
	}

	return nil
}

// checkAnswerability records whether the judge agrees with the case's expected
// answerability. It judges the expanded pipeline result, the same documents
// cmd/rag hands to the answerability gate and the model.
func (e evaluator) checkAnswerability(
	ctx context.Context,
	evalCase EvalCase,
	expanded []retrieval.Document,
	expectedAnswerable bool,
	stats *stats,
) error {
	predictedAnswerable, err := e.judge.IsAnswerable(
		ctx,
		evalCase.Question,
		expanded,
	)
	if err != nil {
		return err
	}

	switch {
	case expectedAnswerable && predictedAnswerable:
		stats.gateCorrectAccepts++
		fmt.Println("Answerability: ANSWERABLE ✓")

	case expectedAnswerable && !predictedAnswerable:
		stats.gateFalseRejects++
		fmt.Println("Answerability: NOT_ANSWERABLE ✗ false rejection")

	case !expectedAnswerable && predictedAnswerable:
		stats.gateFalseAccepts++
		fmt.Println("Answerability: ANSWERABLE ✗ false acceptance")

	default:
		stats.gateCorrectRejects++
		fmt.Println("Answerability: NOT_ANSWERABLE ✓")
	}

	return nil
}

// baselines reports plain vector search at every k in e.ks and returns the
// filtered candidateK documents for the rerankers (e.ks always includes
// candidateK). The answerability judge uses the expanded pipeline result
// instead, matching what cmd/rag sends to the model.
func (e evaluator) baselines(
	ctx context.Context,
	vector []float64,
	evalCase EvalCase,
	answerable bool,
	stats *stats,
) ([]retrieval.Document, error) {
	var candidates []retrieval.Document

	for _, k := range e.ks {
		documents, err := e.retriever.Search(ctx, vector, k)
		if err != nil {
			return nil, err
		}

		// Apply the same similarity filter to both
		// answerable and unanswerable questions.
		documents = retrieval.KeepSimilar(documents, e.minSimilarity)

		if k == e.candidateK {
			candidates = documents
		}

		if !answerable {
			e.reportRejection(k, documents, stats)

			continue
		}

		e.reportBaseline(k, evalCase, documents, stats)
	}

	return candidates, nil
}

// reportBaseline scores one k's documents against the case and prints them.
func (e evaluator) reportBaseline(
	k int,
	evalCase EvalCase,
	documents []retrieval.Document,
	stats *stats,
) {
	recall := recallAtK(evalCase.Expected, documents)
	precision := precisionAtK(evalCase.Expected, documents)

	stats.recallTotals[k] += recall
	stats.precisionTotals[k] += precision

	fmt.Printf(
		"K=%d  Recall@%d=%.2f  Precision@%d=%.2f\n",
		k,
		k,
		recall,
		k,
		precision,
	)

	// Show the ranking whenever retrieval misses expected evidence,
	// and always at candidateK so the candidate set can be inspected.
	if recall < 1.0 || k == e.candidateK {
		if recall < 1.0 {
			fmt.Println("  Retrieval miss:")
		} else {
			fmt.Println("  Ranking:")
		}

		printRetrieved(documents)
	}
}

// rerank reports the reranked result for an answerable case. diverse is the
// deduplicated candidate set.
func (e evaluator) rerank(
	ctx context.Context,
	evalCase EvalCase,
	diverse []retrieval.Document,
	stats *stats,
) error {
	if e.lexicalRerank {
		recall, precision := e.report(
			"Rerank",
			"Reranked",
			evalCase.Expected,
			topK(reranking.Rerank(evalCase.Question, diverse), e.finalK),
		)

		stats.rerankRecall += recall
		stats.rerankPrecision += precision
	}

	if !e.llmRerank {
		return nil
	}

	llmReranked, err := reranking.RerankLLM(
		ctx,
		e.generator,
		evalCase.Question,
		diverse,
	)
	if err != nil {
		return err
	}

	llmRecall, llmPrecision := e.report(
		"LLM Rerank",
		"LLM Reranked",
		evalCase.Expected,
		topK(llmReranked, e.finalK),
	)

	stats.llmRecall += llmRecall
	stats.llmPrecision += llmPrecision

	return nil
}

// printOverall reports the averages across every case.
func printOverall(e evaluator, stats stats) {
	fmt.Println()
	fmt.Println("Overall:")

	fmt.Println("Vector retrieval:")

	for _, k := range e.ks {
		fmt.Printf(
			"K=%d  Avg Recall@%d=%.2f  Avg Precision@%d=%.2f\n",
			k,
			k,
			average(stats.recallTotals[k], stats.answerable),
			k,
			average(stats.precisionTotals[k], stats.answerable),
		)
	}

	fmt.Println()
	fmt.Println("Hybrid retrieval:")

	for _, k := range e.ks {
		fmt.Printf(
			"K=%d  Avg Recall@%d=%.2f  Avg Precision@%d=%.2f\n",
			k,
			k,
			average(stats.hybridRecallTotals[k], stats.answerable),
			k,
			average(stats.hybridPrecisionTotals[k], stats.answerable),
		)
	}

	if stats.evidenceCases > 0 {
		fmt.Printf(
			"Evidence Recall: before expansion=%.2f  after expansion=%.2f\n",
			average(stats.evidenceBefore, stats.evidenceCases),
			average(stats.evidenceAfter, stats.evidenceCases),
		)
	}

	if e.lexicalRerank {
		fmt.Printf(
			"Rerank %d→%d  Avg Recall=%.2f  Avg Precision=%.2f\n",
			e.candidateK,
			e.finalK,
			average(stats.rerankRecall, stats.answerable),
			average(stats.rerankPrecision, stats.answerable),
		)
	}

	if e.llmRerank {
		fmt.Printf(
			"LLM Rerank %d→%d  Avg Recall=%.2f  Avg Precision=%.2f\n",
			e.candidateK,
			e.finalK,
			average(stats.llmRecall, stats.answerable),
			average(stats.llmPrecision, stats.answerable),
		)
	}

	if e.answerabilityGate {
		fmt.Printf(
			"Answerability gate: correct accepts=%d/%d, false rejects=%d\n",
			stats.gateCorrectAccepts,
			stats.answerable,
			stats.gateFalseRejects,
		)

		fmt.Printf(
			"Answerability gate: correct rejects=%d/%d, false accepts=%d\n",
			stats.gateCorrectRejects,
			stats.unanswerable,
			stats.gateFalseAccepts,
		)
	}

	if e.factJudge && stats.factsTotal > 0 {
		fmt.Printf(
			"Generated Fact Recall: %d/%d (%.2f)\n",
			stats.factsSupported,
			stats.factsTotal,
			float64(stats.factsSupported)/float64(stats.factsTotal),
		)
	}

	if stats.unanswerable > 0 {
		rejectionAccuracy :=
			float64(stats.correctRejections) / float64(stats.unanswerable)

		fmt.Printf(
			"Similarity-only rejection=%d/%d (%.2f), False-positive rate=%.2f\n",
			stats.correctRejections,
			stats.unanswerable,
			rejectionAccuracy,
			1.0-rejectionAccuracy,
		)
	}

	if stats.answersJudged > 0 {
		fmt.Printf(
			"Groundedness: %d/%d (%.2f)\n",
			stats.groundedAnswers,
			stats.answersJudged,
			float64(stats.groundedAnswers)/float64(stats.answersJudged),
		)
	}

	if stats.totalCitations > 0 {
		fmt.Printf(
			"Citation Validity: %d/%d (%.2f)\n",
			stats.validCitations,
			stats.totalCitations,
			float64(stats.validCitations)/float64(stats.totalCitations),
		)
	}

	if stats.judgedCitations > 0 {
		fmt.Printf(
			"Citation Entailment: %d/%d (%.2f)\n",
			stats.entailedCitations,
			stats.judgedCitations,
			float64(stats.entailedCitations)/float64(stats.judgedCitations),
		)
	}
}

// average returns sum/count, or 0 when count is 0 so the report never shows NaN.
func average(sum float64, count int) float64 {
	if count == 0 {
		return 0
	}

	return sum / float64(count)
}

// reportRejection records and prints the result for an unanswerable case.
func (e evaluator) reportRejection(
	k int,
	documents []retrieval.Document,
	stats *stats,
) {
	if len(documents) == 0 {
		fmt.Printf("K=%d  Correct rejection\n", k)

		if k == e.candidateK {
			stats.correctRejections++
		}

		return
	}

	fmt.Printf("K=%d  FALSE POSITIVE\n", k)
	printRetrieved(documents)
}

// topK truncates documents to at most k entries.
func topK(documents []retrieval.Document, k int) []retrieval.Document {
	return documents[:min(len(documents), k)]
}

// report scores documents against the expected sections, prints the metrics
// line and the ranking, and returns the recall and precision for accumulation.
func (e evaluator) report(
	metrics string,
	heading string,
	expected []ExpectedDocument,
	documents []retrieval.Document,
) (float64, float64) {
	recall := recallAtK(expected, documents)
	precision := precisionAtK(expected, documents)

	fmt.Printf(
		"%s %d→%d  Recall=%.2f  Precision=%.2f\n",
		metrics,
		e.candidateK,
		e.finalK,
		recall,
		precision,
	)

	fmt.Printf("  %s:\n", heading)
	printRetrieved(documents)

	return recall, precision
}

func recallAtK(
	expected []ExpectedDocument,
	actual []retrieval.Document,
) float64 {
	if len(expected) == 0 {
		return 0
	}

	found := 0

	for _, expectedDoc := range expected {
		for _, actualDoc := range actual {
			if expectedDoc.Source == actualDoc.Source &&
				expectedDoc.Section == actualDoc.Section {
				found++
				break
			}
		}
	}

	return float64(found) / float64(len(expected))
}

func precisionAtK(
	expected []ExpectedDocument,
	actual []retrieval.Document,
) float64 {
	// Evaluation is section-based. Multiple chunks from the same source/section
	// count as one retrieved section.
	retrieved := retrieval.DeduplicateSections(actual)
	if len(retrieved) == 0 {
		return 0
	}

	relevant := 0

	for _, actualDoc := range retrieved {
		for _, expectedDoc := range expected {
			if expectedDoc.Source == actualDoc.Source &&
				expectedDoc.Section == actualDoc.Section {
				relevant++
				break
			}
		}
	}

	return float64(relevant) / float64(len(retrieved))
}

func printRetrieved(documents []retrieval.Document) {
	for index, doc := range documents {
		fmt.Printf(
			"    #%d  %.4f  %s / %s / chunk %d\n",
			index+1,
			doc.Similarity,
			doc.Source,
			doc.Section,
			doc.ChunkIndex,
		)
	}
}

// citationValidity counts the [source - section] citations in answer and how
// many of them name a retrieved document.
func citationValidity(
	answer string,
	documents []retrieval.Document,
) (valid int, total int) {
	remaining := answer

	for {
		_, rest, found := strings.Cut(remaining, "[")
		if !found {
			break
		}

		citation, rest, found := strings.Cut(rest, "]")
		citation = normalizeCitation(citation)
		if !found {
			break
		}

		remaining = rest

		// Only [source - section] counts as a citation.
		source, section, found := strings.Cut(citation, " - ")
		if !found {
			continue
		}

		total++

		source = strings.TrimSpace(source)
		section = strings.TrimSpace(section)

		for _, doc := range documents {
			if normalizeSource(doc.Source) == normalizeSource(source) &&
				doc.Section == section {
				valid++

				break
			}
		}
	}

	return valid, total
}

func evidenceRecall(
	expected []ExpectedDocument,
	actual []retrieval.Document,
) float64 {
	total := 0
	found := 0

	for _, expectedDoc := range expected {
		for _, evidence := range expectedDoc.Evidence {
			total++

			for _, actualDoc := range actual {
				if actualDoc.Source != expectedDoc.Source ||
					actualDoc.Section != expectedDoc.Section {
					continue
				}

				if strings.Contains(
					strings.ToLower(actualDoc.Content),
					strings.ToLower(evidence),
				) {
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

func extractCitedClaims(answer string) []citedClaim {
	var claims []citedClaim

	for line := range strings.SplitSeq(answer, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		remaining := line

		for {
			start := strings.Index(remaining, "[")
			if start == -1 {
				break
			}

			end := strings.Index(remaining[start:], "]")
			if end == -1 {
				break
			}

			end += start

			citation := remaining[start+1 : end]
			citation = normalizeCitation(citation)

			source, section, found := strings.Cut(citation, " - ")
			if !found {
				remaining = remaining[end+1:]
				continue
			}

			claim := strings.TrimSpace(remaining[:start])
			claim = strings.TrimSpace(strings.TrimPrefix(claim, "-"))
			claim = strings.TrimSpace(strings.TrimSuffix(claim, "."))

			if claim != "" {
				claims = append(claims, citedClaim{
					Claim:   claim,
					Source:  strings.TrimSpace(source),
					Section: strings.TrimSpace(section),
				})
			}

			// Everything after this citation may contain another
			// claim/citation pair.
			remaining = strings.TrimSpace(remaining[end+1:])
			remaining = strings.TrimLeft(remaining, ". ")
		}
	}

	return claims
}

// checkCitationEntailment returns how many cited claims were judged and how many
// the judge found entailed by the cited section.
func (e evaluator) checkCitationEntailment(
	ctx context.Context,
	answer string,
	documents []retrieval.Document,
) (int, int, error) {
	claims := extractCitedClaims(answer)

	var judged, entailed int

	for _, claim := range claims {
		var evidence strings.Builder

		for _, doc := range documents {
			if normalizeSource(doc.Source) == normalizeSource(claim.Source) &&
				doc.Section == claim.Section {
				evidence.WriteString(doc.Content)
				evidence.WriteString("\n")
			}
		}

		// Invalid citations are already measured by citationValidity().
		if evidence.Len() == 0 {
			continue
		}

		isEntailed, err := e.judge.IsEntailed(
			ctx,
			claim.Claim,
			evidence.String(),
		)
		if err != nil {
			return 0, 0, err
		}

		judged++

		if isEntailed {
			entailed++
		}
	}

	return entailed, judged, nil
}

func normalizeCitation(citation string) string {
	citation = strings.ReplaceAll(citation, "–", "-")
	citation = strings.ReplaceAll(citation, "—", "-")

	return citation
}

// normalizeSource makes a citation's source comparable to a document's source.
// Models tend to drop the ".md" suffix and vary capitalization; neither
// changes which document a citation points at.
func normalizeSource(source string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(source)), ".md")
}
