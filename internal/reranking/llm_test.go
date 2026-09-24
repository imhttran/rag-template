package reranking

import (
	"slices"
	"testing"
)

func TestParseRanking(t *testing.T) {
	tests := []struct {
		name          string
		response      string
		documentCount int
		want          []int
		wantErr       bool
	}{
		{
			name:          "plain JSON array",
			response:      "[2,0,1]",
			documentCount: 3,
			want:          []int{2, 0, 1},
		},
		{
			name:          "fenced JSON array",
			response:      "```json\n[2,0,1]\n```",
			documentCount: 3,
			want:          []int{2, 0, 1},
		},
		{
			name:          "bare fenced array",
			response:      "```\n[1,0]\n```",
			documentCount: 2,
			want:          []int{1, 0},
		},
		{
			name:          "surrounding whitespace",
			response:      "\n  [0]  \n",
			documentCount: 1,
			want:          []int{0},
		},
		{
			name:          "empty ranking",
			response:      "[]",
			documentCount: 0,
			want:          []int{},
		},
		{
			name:          "too few IDs",
			response:      "[0,1]",
			documentCount: 3,
			wantErr:       true,
		},
		{
			name:          "too many IDs",
			response:      "[0,1,2,3]",
			documentCount: 3,
			wantErr:       true,
		},
		{
			name:          "duplicate ID",
			response:      "[0,0,1]",
			documentCount: 3,
			wantErr:       true,
		},
		{
			name:          "out of range ID",
			response:      "[0,1,3]",
			documentCount: 3,
			wantErr:       true,
		},
		{
			name:          "negative ID",
			response:      "[-1,0,1]",
			documentCount: 3,
			wantErr:       true,
		},
		{
			name:          "not JSON",
			response:      "The ranking is [0,1,2]",
			documentCount: 3,
			wantErr:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseRanking(
				test.response,
				test.documentCount,
			)

			if test.wantErr {
				if err == nil {
					t.Fatalf(
						"parseRanking(%q, %d) = %v, want error",
						test.response,
						test.documentCount,
						got,
					)
				}

				return
			}

			if err != nil {
				t.Fatalf(
					"parseRanking(%q, %d): unexpected error: %v",
					test.response,
					test.documentCount,
					err,
				)
			}

			if !slices.Equal(got, test.want) {
				t.Fatalf(
					"parseRanking(%q, %d) = %v, want %v",
					test.response,
					test.documentCount,
					got,
					test.want,
				)
			}
		})
	}
}
