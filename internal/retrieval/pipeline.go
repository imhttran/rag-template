package retrieval

import "context"

// Searcher is the retrieval work the hybrid pipeline needs. *Retriever
// implements it; tests supply a fake to exercise the pipeline without a
// database.
type Searcher interface {
	Search(
		ctx context.Context,
		vector []float64,
		topK int,
	) ([]Document, error)

	KeywordSearch(
		ctx context.Context,
		query string,
		topK int,
	) ([]Document, error)

	SectionChunks(
		ctx context.Context,
		keys []SectionKey,
		limit int,
	) ([]Document, error)
}

// PipelineOptions controls the production hybrid retrieval pipeline.
type PipelineOptions struct {
	CandidateK    int
	FinalK        int
	ExpandLimit   int
	MinSimilarity float64
}

// PipelineResult exposes the major retrieval stages so callers can inspect
// or evaluate them without reimplementing the pipeline.
type PipelineResult struct {
	Vector   []Document
	Keyword  []Document
	Fused    []Document
	Expanded []Document
}

// MultiQueryResult exposes the single-query stages plus the rewritten-query
// stages, so callers can inspect each before the fused and expanded results.
type MultiQueryResult struct {
	OriginalVector   []Document
	OriginalKeyword  []Document
	RewrittenVector  []Document
	RewrittenKeyword []Document
	Fused            []Document
	Expanded         []Document
}

// HybridRetrieve runs the production retrieval pipeline:
//
//	vector search
//	  + keyword search
//	        ↓
//	      RRF fusion
//	        ↓
//	  section deduplication
//	        ↓
//	  section expansion
func HybridRetrieve(
	ctx context.Context,
	searcher Searcher,
	question string,
	vector []float64,
	options PipelineOptions,
) (PipelineResult, error) {
	vectorDocuments, err := searcher.Search(
		ctx,
		vector,
		options.CandidateK,
	)
	if err != nil {
		return PipelineResult{}, err
	}

	// The similarity floor is a vector-search concept: full-text rank is not
	// comparable to cosine similarity, so keyword results are left unfiltered.
	vectorDocuments = KeepSimilar(
		vectorDocuments,
		options.MinSimilarity,
	)

	keywordDocuments, err := searcher.KeywordSearch(
		ctx,
		question,
		options.CandidateK,
	)
	if err != nil {
		return PipelineResult{}, err
	}

	fused := Fuse(
		vectorDocuments,
		keywordDocuments,
		options.FinalK,
	)

	fused = DeduplicateSections(fused)

	expanded, err := searcher.SectionChunks(
		ctx,
		SectionKeys(fused),
		options.ExpandLimit,
	)
	if err != nil {
		return PipelineResult{}, err
	}

	return PipelineResult{
		Vector:   vectorDocuments,
		Keyword:  keywordDocuments,
		Fused:    fused,
		Expanded: expanded,
	}, nil
}

func MultiQueryRetrieve(
	ctx context.Context,
	searcher Searcher,
	originalQuery string,
	rewrittenQuery string,
	originalVector []float64,
	rewrittenVector []float64,
	options PipelineOptions,
) (MultiQueryResult, error) {
	originalVectorDocuments, err := searcher.Search(
		ctx,
		originalVector,
		options.CandidateK,
	)
	if err != nil {
		return MultiQueryResult{}, err
	}

	originalVectorDocuments = KeepSimilar(
		originalVectorDocuments,
		options.MinSimilarity,
	)

	originalKeywordDocuments, err := searcher.KeywordSearch(
		ctx,
		originalQuery,
		options.CandidateK,
	)
	if err != nil {
		return MultiQueryResult{}, err
	}

	rewrittenVectorDocuments, err := searcher.Search(
		ctx,
		rewrittenVector,
		options.CandidateK,
	)
	if err != nil {
		return MultiQueryResult{}, err
	}

	rewrittenVectorDocuments = KeepSimilar(
		rewrittenVectorDocuments,
		options.MinSimilarity,
	)

	rewrittenKeywordDocuments, err := searcher.KeywordSearch(
		ctx,
		rewrittenQuery,
		options.CandidateK,
	)
	if err != nil {
		return MultiQueryResult{}, err
	}

	fused := FuseRankings(
		[][]Document{
			originalVectorDocuments,
			originalKeywordDocuments,
			rewrittenVectorDocuments,
			rewrittenKeywordDocuments,
		},
		options.FinalK,
	)

	fused = DeduplicateSections(fused)

	expanded, err := searcher.SectionChunks(
		ctx,
		SectionKeys(fused),
		options.ExpandLimit,
	)
	if err != nil {
		return MultiQueryResult{}, err
	}

	return MultiQueryResult{
		OriginalVector:   originalVectorDocuments,
		OriginalKeyword:  originalKeywordDocuments,
		RewrittenVector:  rewrittenVectorDocuments,
		RewrittenKeyword: rewrittenKeywordDocuments,
		Fused:            fused,
		Expanded:         expanded,
	}, nil
}
