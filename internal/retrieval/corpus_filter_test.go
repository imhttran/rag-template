package retrieval

import "testing"

// The corpus dimension is the isolation unit (RAG-SOP-002). These are the
// negative/allowlist unit tests for the filter fragment: a corpus predicate is
// emitted first, it binds as a parameter (never concatenated), and an empty
// corpus never widens the query.
func TestFilterFragmentCorpus(t *testing.T) {
	cases := []struct {
		name     string
		filter   Filter
		start    int
		wantSQL  string
		wantArgs int
	}{
		{"corpus only", Filter{Corpus: "kb"}, 2, "corpus = $2", 1},
		{"corpus precedes source", Filter{Corpus: "kb", SourcePrefix: "docs/"}, 2, "corpus = $2 AND source LIKE $3", 2},
		{"zero filter is unchanged", Filter{}, 2, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := filterFragment(tc.filter, tc.start)
			if sql != tc.wantSQL {
				t.Fatalf("sql = %q, want %q", sql, tc.wantSQL)
			}
			if len(args) != tc.wantArgs {
				t.Fatalf("args = %v, want %d bound values", args, tc.wantArgs)
			}
		})
	}
}

func TestFilterCorpusIsNotEmpty(t *testing.T) {
	if (Filter{Corpus: "kb"}).IsZero() {
		t.Fatal("a corpus filter must not be reported as zero (it would be dropped)")
	}
}
