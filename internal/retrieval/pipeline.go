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
	Vector     []Document
	Keyword    []Document
	Candidates []Document
	Fused      []Document
	Expanded   []Document
}

// MultiQueryResult exposes the single-query stages plus the rewritten-query
// stages, so callers can inspect each before the fused and expanded results.
type MultiQueryResult struct {
	OriginalVector   []Document
	OriginalKeyword  []Document
	RewrittenVector  []Document
	RewrittenKeyword []Document
	Candidates       []Document
	Fused            []Document
	Expanded         []Document
}

// searchOne runs the vector and keyword search for one query, applying the
// similarity floor to the vector results.
func searchOne(
	ctx context.Context,
	searcher Searcher,
	query string,
	vector []float64,
	options PipelineOptions,
) ([]Document, []Document, error) {
	vectorDocuments, err := searcher.Search(
		ctx,
		vector,
		options.CandidateK,
	)
	if err != nil {
		return nil, nil, err
	}

	// The similarity floor is a vector-search concept: full-text rank is not
	// comparable to cosine similarity, so keyword results are left unfiltered.
	vectorDocuments = KeepSimilar(
		vectorDocuments,
		options.MinSimilarity,
	)

	keywordDocuments, err := searcher.KeywordSearch(
		ctx,
		query,
		options.CandidateK,
	)
	if err != nil {
		return nil, nil, err
	}

	return vectorDocuments, keywordDocuments, nil
}

// fuseAndExpand fuses rankings at CandidateK and FinalK, deduplicates sections,
// and expands the final fused sections.
func fuseAndExpand(
	ctx context.Context,
	searcher Searcher,
	rankings [][]Document,
	options PipelineOptions,
) ([]Document, []Document, []Document, error) {
	fused := FuseRankings(rankings, options.FinalK)
	fused = DeduplicateSections(fused)

	candidates := FuseRankings(rankings, options.CandidateK)
	candidates = DeduplicateSections(candidates)

	expanded, err := searcher.SectionChunks(
		ctx,
		SectionKeys(fused),
		options.ExpandLimit,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	return candidates, fused, expanded, nil
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
	vectorDocuments, keywordDocuments, err := searchOne(
		ctx,
		searcher,
		question,
		vector,
		options,
	)
	if err != nil {
		return PipelineResult{}, err
	}

	candidates, fused, expanded, err := fuseAndExpand(
		ctx,
		searcher,
		[][]Document{vectorDocuments, keywordDocuments},
		options,
	)
	if err != nil {
		return PipelineResult{}, err
	}

	return PipelineResult{
		Vector:     vectorDocuments,
		Keyword:    keywordDocuments,
		Candidates: candidates,
		Fused:      fused,
		Expanded:   expanded,
	}, nil
}

// MultiQueryRetrieve runs the pipeline over the original and the rewritten
// query, fusing all four rankings.
func MultiQueryRetrieve(
	ctx context.Context,
	searcher Searcher,
	originalQuery string,
	rewrittenQuery string,
	originalVector []float64,
	rewrittenVector []float64,
	options PipelineOptions,
) (MultiQueryResult, error) {
	originalVectorDocuments, originalKeywordDocuments, err := searchOne(
		ctx,
		searcher,
		originalQuery,
		originalVector,
		options,
	)
	if err != nil {
		return MultiQueryResult{}, err
	}

	rewrittenVectorDocuments, rewrittenKeywordDocuments, err := searchOne(
		ctx,
		searcher,
		rewrittenQuery,
		rewrittenVector,
		options,
	)
	if err != nil {
		return MultiQueryResult{}, err
	}

	candidates, fused, expanded, err := fuseAndExpand(
		ctx,
		searcher,
		[][]Document{
			originalVectorDocuments,
			originalKeywordDocuments,
			rewrittenVectorDocuments,
			rewrittenKeywordDocuments,
		},
		options,
	)
	if err != nil {
		return MultiQueryResult{}, err
	}

	return MultiQueryResult{
		OriginalVector:   originalVectorDocuments,
		OriginalKeyword:  originalKeywordDocuments,
		RewrittenVector:  rewrittenVectorDocuments,
		RewrittenKeyword: rewrittenKeywordDocuments,
		Candidates:       candidates,
		Fused:            fused,
		Expanded:         expanded,
	}, nil
}
