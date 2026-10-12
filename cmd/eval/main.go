package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	"rag-template/internal/answerability"
	"rag-template/internal/citations"
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

// defaultDatasetPath is the dataset scored when EVAL_DATASET is unset or empty.
// It is the pre-existing default and must not change.
const defaultDatasetPath = "evals/retrieval.json"

// prefilterEnv is the opt-in, evaluation-only environment switch that prints
// per-case pre-KeepSimilar candidate diagnostics. It defaults OFF: when it is
// unset or not truthy every code path below behaves exactly as before and the
// default cmd/eval output is byte-identical. It changes no production default,
// no MIN_SIMILARITY handling, and no retrieval/embedding/ingestion code; it only
// prints extra diagnostic lines.
const prefilterEnv = "EVAL_PREFILTER_DUMP"

// prefilterDumpEnabled reports whether the opt-in pre-filter candidate dump is
// on. Only the exact values "1", "true" (any case) enable it; anything else,
// including unset, keeps the default output unchanged.
func prefilterDumpEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(prefilterEnv))) {
	case "1", "true":
		return true
	default:
		return false
	}
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

	// Rerank latency and outcome tracking. off has no rerank call, so it adds
	// no latency; lexical and LLM measure wall time around their rerank calls,
	// and fallback counts how often the hardened reranker degraded to the
	// fused order instead of returning a model ordering.
	llmLatencyTotal     time.Duration
	llmLatencyCases     int
	llmFallbacks        int
	lexicalLatencyTotal time.Duration
	lexicalLatencyCases int
}

type citedClaim struct {
	Claim   string
	Source  string
	Section string
}

// evaluator scores cases against a retriever.
type evaluator struct {
	embedder          embedding.Embedder
	retriever         *retrieval.Retriever
	generator         generation.Generator
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
	rerankTimeout     time.Duration
	ks                []int

	// prefilterDump is the opt-in, evaluation-only switch that enables the
	// pre-KeepSimilar candidate diagnostic lines. It is false by default so
	// default output is byte-identical.
	prefilterDump bool

	// language is cfg.Language forwarded into retrieval.PipelineOptions so
	// keyword retrieval uses the configured CORPUS_LANGUAGE full-text search
	// configuration. Empty keeps the baseline 'english' config, matching
	// cmd/rag's behavior.
	language string
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

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	conn, err := cfg.Connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	// Resolve the embedder through the provider registry so EMBED_PROVIDER
	// selects the implementation. An empty provider keeps the ollama default; an
	// unregistered provider is an error, never a silent fallback.
	embedder, err := cfg.Embedder()
	if err != nil {
		return err
	}

	eval := evaluator{
		embedder:          embedder,
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
		rerankTimeout:     cfg.RerankTimeout,
		ks:                retrievalKs(cfg.TopK),
		prefilterDump:     prefilterDumpEnabled(),
		language:          cfg.Language,
	}

	if cfg.AnswerabilityGate ||
		cfg.FactJudge ||
		cfg.LLMRerank ||
		cfg.QueryRewrite {

		// Resolve the generator through the provider registry, preserving the
		// existing condition under which it is built. An unregistered
		// GEN_PROVIDER is an error, never a silent fallback.
		generator, err := cfg.Generator()
		if err != nil {
			return err
		}

		eval.generator = generator

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

// retrievalKs is the set of K values the report scores, deduplicated so a TOP_K
// of 1 or 2 does not count the same K twice.
func retrievalKs(topK int) []int {
	ks := []int{1, 2, topK}

	slices.Sort(ks)

	return slices.Compact(ks)
}

// datasetPath selects the evaluation dataset path from EVAL_DATASET. An unset or
// empty variable keeps the pre-existing default (evals/retrieval.json), so the
// bundled run is unchanged; any other value selects that file so a bilingual or
// candidate dataset can be scored without editing code.
func datasetPath() string {
	if path := strings.TrimSpace(os.Getenv("EVAL_DATASET")); path != "" {
		return path
	}

	return defaultDatasetPath
}

// loadCases reads and parses the evaluation cases selected by EVAL_DATASET.
// Both the read and the parse failure name the selected path so a mistaken
// EVAL_DATASET is actionable in the log.
func loadCases() ([]EvalCase, error) {
	path := datasetPath()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read evaluation dataset %s: %w", path, err)
	}

	var cases []EvalCase

	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("parse evaluation dataset %s: %w", path, err)
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

// pipelineOptions builds the retrieval options every baseline uses. Language is
// forwarded from cfg.Language so keyword retrieval uses the configured
// CORPUS_LANGUAGE full-text search configuration, matching cmd/rag. Empty keeps
// the baseline 'english' config so default runs are unchanged.
func (e evaluator) pipelineOptions() retrieval.PipelineOptions {
	return retrieval.PipelineOptions{
		CandidateK:    e.candidateK,
		FinalK:        e.finalK,
		ExpandLimit:   e.expandLimit,
		MinSimilarity: e.minSimilarity,
		Language:      e.language,
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

		valid, total := citations.Validity(answer, expanded)

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
		fmt.Println("Answerability: ANSWERABLE \u2713")

	case expectedAnswerable && !predictedAnswerable:
		stats.gateFalseRejects++
		fmt.Println("Answerability: NOT_ANSWERABLE \u2717 false rejection")

	case !expectedAnswerable && predictedAnswerable:
		stats.gateFalseAccepts++
		fmt.Println("Answerability: ANSWERABLE \u2717 false acceptance")

	default:
		stats.gateCorrectRejects++
		fmt.Println("Answerability: NOT_ANSWERABLE \u2713")
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

		// The opt-in dump reports the raw, pre-filter candidate slice before
		// KeepSimilar is applied, so candidate loss attributable to the floor
		// can be measured. It is off by default and prints nothing then, so
		// the default output is unchanged.
		if e.prefilterDump {
			e.reportPrefilter(k, evalCase, documents)
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

// reportPrefilter prints the opt-in, evaluation-only pre-KeepSimilar candidate
// diagnostic for one K BEFORE the similarity floor is applied. It reports the
// pre-filter candidate count, how many are >= the floor and how many are below,
// whether each expected source/section is present pre-filter (and the first
// rank where it appears), and which candidate(s) the floor would drop. It emits
// nothing unless the operator turned the dump on, so the default run stays
// byte-identical.
func (e evaluator) reportPrefilter(
	k int,
	evalCase EvalCase,
	documents []retrieval.Document,
) {
	kept := 0

	for _, doc := range documents {
		if doc.Similarity >= e.minSimilarity {
			kept++
		}
	}

	dropped := len(documents) - kept

	fmt.Printf(
		"Pre-filter K=%d  candidates=%d  sim>=%.2f=%d  sim<%.2f=%d\n",
		k,
		len(documents),
		e.minSimilarity,
		kept,
		e.minSimilarity,
		dropped,
	)

	if len(evalCase.Expected) == 0 {
		fmt.Println("  expected: (unanswerable)")
	} else {
		for _, expected := range evalCase.Expected {
			rank := 0

			for index, doc := range documents {
				if doc.Source == expected.Source && doc.Section == expected.Section {
					rank = index + 1

					break
				}
			}

			if rank == 0 {
				fmt.Printf(
					"  expected present pre-filter: NO  %s / %s\n",
					expected.Source,
					expected.Section,
				)
			} else {
				fmt.Printf(
					"  expected present pre-filter: YES  %s / %s (rank %d)\n",
					expected.Source,
					expected.Section,
					rank,
				)
			}
		}
	}

	for _, doc := range documents {
		if doc.Similarity >= e.minSimilarity {
			continue
		}

		fmt.Printf(
			"  dropped by floor: %.4f  %s / %s / chunk %d\n",
			doc.Similarity,
			doc.Source,
			doc.Section,
			doc.ChunkIndex,
		)
	}
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
//
// Each strategy's wall time is measured around its rerank call so sweep rows can
// compare off / lexical / LLM (and any future cross-encoder) on latency as well
// as recall, precision, and evidence recall. A hardened reranker that degrades
// to the fused order is recorded as a fallback rather than aborting evaluation.
func (e evaluator) rerank(
	ctx context.Context,
	evalCase EvalCase,
	diverse []retrieval.Document,
	stats *stats,
) error {
	if e.lexicalRerank {
		start := time.Now()

		reranked := reranking.Rerank(evalCase.Question, diverse)

		stats.lexicalLatencyTotal += time.Since(start)
		stats.lexicalLatencyCases++

		recall, precision := e.report(
			"Rerank",
			"Reranked",
			evalCase.Expected,
			topK(reranked, e.finalK),
		)

		stats.rerankRecall += recall
		stats.rerankPrecision += precision
	}

	if !e.llmRerank {
		return nil
	}

	start := time.Now()

	result, err := reranking.RerankLLMWithTimeout(
		ctx,
		e.generator,
		evalCase.Question,
		diverse,
		e.rerankTimeout,
	)

	stats.llmLatencyTotal += time.Since(start)
	stats.llmLatencyCases++

	if err != nil {
		// The hardened reranker degrades internally; a returned error is only
		// possible for programming errors and should not abort the whole run.
		fmt.Printf("LLM Rerank: fell back to fused order (%v)\n", err)

		stats.llmFallbacks++

		return nil
	}

	// The hardened reranker reports its own outcome, so a model that happens to
	// return the fused order is not misclassified as a fallback. The disabled
	// case (no candidates to rank) is not a reranker failure and is not counted
	// as a fallback either.
	switch {
	case !result.FellBack:
		fmt.Println("LLM Rerank: used model ordering")
	case result.Reason == reranking.FallbackDisabled:
		fmt.Println("LLM Rerank: no candidates to rank")
	default:
		stats.llmFallbacks++
		fmt.Printf("LLM Rerank: used fused order (fallback: %s)\n", result.Reason)
	}

	llmRecall, llmPrecision := e.report(
		"LLM Rerank",
		"LLM Reranked",
		evalCase.Expected,
		topK(result.Documents, e.finalK),
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
			"Avg Evidence Recall: before expansion=%.2f  after expansion=%.2f\n",
			average(stats.evidenceBefore, stats.evidenceCases),
			average(stats.evidenceAfter, stats.evidenceCases),
		)
	}

	// Latency comparison row, always printed so the off baseline is reported
	// even on an off-only run. Off makes no rerank call, so it contributes zero
	// rerank latency; lexical/LLM report their measured wall time.
	printLatencyComparison(e, stats)

	if e.lexicalRerank {
		fmt.Printf(
			"Rerank %d\u2192%d  Avg Recall=%.2f  Avg Precision=%.2f\n",
			e.candidateK,
			e.finalK,
			average(stats.rerankRecall, stats.answerable),
			average(stats.rerankPrecision, stats.answerable),
		)
	}

	if e.llmRerank {
		fmt.Printf(
			"LLM Rerank %d\u2192%d  Avg Recall=%.2f  Avg Precision=%.2f\n",
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

// printLatencyComparison reports the per-strategy rerank latency row so sweep
// output compares off / lexical / LLM (and any future cross-encoder) on latency
// alongside recall, precision, and evidence recall. The off strategy makes no
// rerank call, so its contribution is reported as zero with a clear marker.
// It is always printed so an off-only run still reports the baseline.
func printLatencyComparison(e evaluator, stats stats) {
	fmt.Println("Rerank latency:")

	fmt.Println("  off      avg=0s  (baseline, no rerank call)")

	if e.lexicalRerank {
		fmt.Printf(
			"  lexical  avg=%s  calls=%d\n",
			averageDuration(stats.lexicalLatencyTotal, stats.lexicalLatencyCases),
			stats.lexicalLatencyCases,
		)
	} else {
		fmt.Println("  lexical  unavailable (EVAL_LEXICAL_RERANK=false)")
	}

	if e.llmRerank {
		fmt.Printf(
			"  llm      avg=%s  calls=%d  fallbacks=%d\n",
			averageDuration(stats.llmLatencyTotal, stats.llmLatencyCases),
			stats.llmLatencyCases,
			stats.llmFallbacks,
		)
	} else {
		fmt.Println("  llm      unavailable (EVAL_LLM_RERANK=false)")
	}

	fmt.Println(
		"  cross-encoder  unavailable (no cross-encoder provider registered)",
	)
}

// averageDuration returns sum/count as a rounded duration, or 0 when count is 0.
func averageDuration(sum time.Duration, count int) time.Duration {
	if count == 0 {
		return 0
	}

	return sum / time.Duration(count)
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
		"%s %d\u2192%d  Recall=%.2f  Precision=%.2f\n",
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

// extractCitedClaims builds one citedClaim per citation in answer, delegating
// bracket scanning and source/section/page parsing to the shared parser in
// internal/citations (so cmd/rag, cmd/eval, and every other consumer share one
// parser). For each citation span it pairs the claim text preceding the bracket
// with the parsed source/section. The claim text is the segment between the
// previous citation bracket's end and this bracket's start; within that segment
// the text after the last "- " separator is used and the result is trimmed of a
// trailing ".", so a bullet list line pairs each bullet with its own citation.
// A bracket with no " - " separator is not a citation and yields no claim, and
// citations with empty claim text are skipped, preserving the previous output
// semantics.
func extractCitedClaims(answer string) []citedClaim {
	var claims []citedClaim

	for line := range strings.SplitSeq(answer, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		cursor := 0

		for _, span := range citations.ParseSpans(line) {
			claim := claimText(line[cursor:span.Start])

			cursor = span.End

			if claim == "" {
				continue
			}

			claims = append(claims, citedClaim{
				Claim:   claim,
				Source:  span.Citation.Source,
				Section: span.Citation.Section,
			})
		}
	}

	return claims
}

// claimText reduces a citation's preceding text to the claim it states. It trims
// surrounding whitespace, then keeps the text after the last "- " separator (so
// a bullet marker or a joining "and - " does not become part of the claim) and
// finally trims a trailing ".". The result is empty when the segment carries no
// claim text.
func claimText(segment string) string {
	segment = strings.TrimSpace(segment)

	if index := strings.LastIndex(segment, "- "); index != -1 {
		segment = segment[index+len("- "):]
	}

	segment = strings.TrimSpace(segment)
	segment = strings.TrimSpace(strings.TrimSuffix(segment, "."))

	return segment
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
			if citations.NormalizeSource(doc.Source) == citations.NormalizeSource(claim.Source) &&
				doc.Section == claim.Section {
				evidence.WriteString(doc.Content)
				evidence.WriteString("\n")
			}
		}

		// Invalid citations are already measured by citations.Validity().
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
