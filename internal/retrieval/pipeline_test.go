package retrieval

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// fakeSearcher serves canned stage results and records the arguments the
// pipeline passed to each stage.
type fakeSearcher struct {
	vector   []Document
	keyword  []Document
	expanded []Document

	searchErr  error
	keywordErr error
	expandErr  error

	topK         int
	keywordQuery string
	keywordTopK  int
	expandKeys   []SectionKey
	expandLimit  int
}

func (f *fakeSearcher) Search(
	ctx context.Context,
	vector []float64,
	topK int,
) ([]Document, error) {
	f.topK = topK

	if f.searchErr != nil {
		return nil, f.searchErr
	}

	return f.vector, nil
}

func (f *fakeSearcher) KeywordSearch(
	ctx context.Context,
	query string,
	topK int,
) ([]Document, error) {
	f.keywordQuery = query
	f.keywordTopK = topK

	if f.keywordErr != nil {
		return nil, f.keywordErr
	}

	return f.keyword, nil
}

func (f *fakeSearcher) SectionChunks(
	ctx context.Context,
	keys []SectionKey,
	limit int,
) ([]Document, error) {
	f.expandKeys = keys
	f.expandLimit = limit

	if f.expandErr != nil {
		return nil, f.expandErr
	}

	return f.expanded, nil
}

func TestHybridRetrieveWiresStagesInOrder(t *testing.T) {
	topChunk := Document{
		ID:         1,
		Source:     "loan-policy.md",
		Section:    "Missed Payments",
		ChunkIndex: 0,
		Similarity: 0.9,
	}

	// Same section as topChunk, so deduplication must drop it.
	lowerChunk := Document{
		ID:         2,
		Source:     "loan-policy.md",
		Section:    "Missed Payments",
		ChunkIndex: 1,
		Similarity: 0.8,
	}

	// Below the similarity floor, so fusion must never see it.
	belowFloor := Document{
		ID:         3,
		Source:     "large-loan-policy.md",
		Section:    "Fees",
		Similarity: 0.2,
	}

	keywordHit := Document{
		ID:           4,
		Source:       "large-loan-policy.md",
		Section:      "Refunds",
		KeywordScore: 0.5,
	}

	searcher := &fakeSearcher{
		vector:   []Document{topChunk, lowerChunk, belowFloor},
		keyword:  []Document{keywordHit},
		expanded: []Document{topChunk, lowerChunk},
	}

	result, err := HybridRetrieve(
		context.Background(),
		searcher,
		"how long is the grace period?",
		[]float64{0.1, 0.2},
		PipelineOptions{
			CandidateK:    4,
			FinalK:        3,
			ExpandLimit:   20,
			MinSimilarity: 0.6,
		},
	)
	if err != nil {
		t.Fatalf("hybrid retrieve: %v", err)
	}

	if searcher.topK != 4 || searcher.keywordTopK != 4 {
		t.Fatalf(
			"expected both searches to use CandidateK=4, got %d and %d",
			searcher.topK,
			searcher.keywordTopK,
		)
	}

	if searcher.keywordQuery != "how long is the grace period?" {
		t.Fatalf("keyword search got question %q", searcher.keywordQuery)
	}

	// The floor is applied to vector results, keeping rank order.
	wantVector := []int64{1, 2}
	if got := ids(result.Vector); !slices.Equal(got, wantVector) {
		t.Fatalf("expected vector documents %v, got %v", wantVector, got)
	}

	wantKeyword := []int64{4}
	if got := ids(result.Keyword); !slices.Equal(got, wantKeyword) {
		t.Fatalf("expected keyword documents %v, got %v", wantKeyword, got)
	}

	// Fusion keeps FinalK documents, then deduplication keeps one per section:
	// document 2 loses to document 1 because they share a section.
	wantFused := []int64{1, 4}
	if got := ids(result.Fused); !slices.Equal(got, wantFused) {
		t.Fatalf("expected fused documents %v, got %v", wantFused, got)
	}

	if result.Fused[0].FusionScore == 0 {
		t.Fatal("fused documents should carry an RRF score")
	}

	// Expansion is driven by the deduplicated sections, not by raw fusion.
	wantKeys := []SectionKey{
		{Source: "loan-policy.md", Section: "Missed Payments"},
		{Source: "large-loan-policy.md", Section: "Refunds"},
	}

	if !slices.Equal(searcher.expandKeys, wantKeys) {
		t.Fatalf("expected expansion keys %v, got %v", wantKeys, searcher.expandKeys)
	}

	if searcher.expandLimit != 20 {
		t.Fatalf("expected ExpandLimit=20, got %d", searcher.expandLimit)
	}

	wantExpanded := []int64{1, 2}
	if got := ids(result.Expanded); !slices.Equal(got, wantExpanded) {
		t.Fatalf("expected expanded documents %v, got %v", wantExpanded, got)
	}
}

func TestHybridRetrieveLimitsFusionToFinalK(t *testing.T) {
	searcher := &fakeSearcher{
		vector: []Document{
			{ID: 1, Source: "a.md", Section: "One", Similarity: 0.9},
			{ID: 2, Source: "a.md", Section: "Two", Similarity: 0.8},
		},
	}

	result, err := HybridRetrieve(
		context.Background(),
		searcher,
		"question",
		[]float64{0.1},
		PipelineOptions{
			CandidateK:    2,
			FinalK:        1,
			ExpandLimit:   5,
			MinSimilarity: 0.5,
		},
	)
	if err != nil {
		t.Fatalf("hybrid retrieve: %v", err)
	}

	wantFused := []int64{1}
	if got := ids(result.Fused); !slices.Equal(got, wantFused) {
		t.Fatalf("expected fused documents %v, got %v", wantFused, got)
	}

	wantKeys := []SectionKey{{Source: "a.md", Section: "One"}}
	if !slices.Equal(searcher.expandKeys, wantKeys) {
		t.Fatalf("expected expansion keys %v, got %v", wantKeys, searcher.expandKeys)
	}
}

func TestHybridRetrieveReturnsStageErrors(t *testing.T) {
	searchErr := errors.New("search failed")
	keywordErr := errors.New("keyword search failed")
	expandErr := errors.New("expand failed")

	tests := []struct {
		name     string
		searcher *fakeSearcher
		want     error
	}{
		{
			name:     "search",
			searcher: &fakeSearcher{searchErr: searchErr},
			want:     searchErr,
		},
		{
			name:     "keyword search",
			searcher: &fakeSearcher{keywordErr: keywordErr},
			want:     keywordErr,
		},
		{
			name:     "section chunks",
			searcher: &fakeSearcher{expandErr: expandErr},
			want:     expandErr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := HybridRetrieve(
				context.Background(),
				test.searcher,
				"question",
				[]float64{0.1},
				PipelineOptions{
					CandidateK:    4,
					FinalK:        2,
					ExpandLimit:   20,
					MinSimilarity: 0.6,
				},
			)

			if !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
		})
	}
}

func ids(documents []Document) []int64 {
	values := make([]int64, 0, len(documents))

	for _, document := range documents {
		values = append(values, document.ID)
	}

	return values
}
