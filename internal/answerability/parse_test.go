package answerability

import "testing"

func TestParseSupportedFacts(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		factCount int
		want      int
		wantErr   bool
	}{
		{
			name:      "all supported",
			response:  "1,2,3",
			factCount: 3,
			want:      3,
		},
		{
			name:      "subset",
			response:  "2",
			factCount: 3,
			want:      1,
		},
		{
			name:      "surrounding spaces",
			response:  " 1, 3 ",
			factCount: 3,
			want:      2,
		},
		{
			name:      "none supported",
			response:  "NONE",
			factCount: 3,
			want:      0,
		},
		{
			name:      "none supported, lowercase",
			response:  "none",
			factCount: 3,
			want:      0,
		},
		{
			name:      "repeats count once",
			response:  "1,1,2",
			factCount: 3,
			want:      2,
		},
		{
			name:      "out of range",
			response:  "1,4",
			factCount: 3,
			wantErr:   true,
		},
		{
			name:      "zero is not a fact number",
			response:  "0",
			factCount: 3,
			wantErr:   true,
		},
		{
			name:      "prose",
			response:  "facts one and three",
			factCount: 3,
			wantErr:   true,
		},
		{
			name:      "empty",
			response:  "",
			factCount: 3,
			wantErr:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseSupportedFacts(test.response, test.factCount)

			if test.wantErr {
				if err == nil {
					t.Fatalf(
						"parseSupportedFacts(%q, %d) = %d, want error",
						test.response,
						test.factCount,
						got,
					)
				}

				return
			}

			if err != nil {
				t.Fatalf(
					"parseSupportedFacts(%q, %d): unexpected error: %v",
					test.response,
					test.factCount,
					err,
				)
			}

			if got != test.want {
				t.Fatalf(
					"parseSupportedFacts(%q, %d) = %d, want %d",
					test.response,
					test.factCount,
					got,
					test.want,
				)
			}
		})
	}
}

func TestParseGroundedness(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     bool
		wantErr  bool
	}{
		{
			name:     "grounded",
			response: "GROUNDED",
			want:     true,
		},
		{
			name:     "not grounded",
			response: "NOT_GROUNDED",
			want:     false,
		},
		{
			name:     "not-grounded must not read as grounded",
			response: "NOT_GROUNDED.",
			want:     false,
		},
		{
			name:     "lowercase",
			response: "grounded",
			want:     true,
		},
		{
			name:     "trailing prose",
			response: "GROUNDED because every claim appears in the evidence.",
			want:     true,
		},
		{
			name:     "prose only",
			response: "Looks fine to me.",
			wantErr:  true,
		},
		{
			name:     "empty",
			response: "",
			wantErr:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseGroundedness(test.response)

			if test.wantErr {
				if err == nil {
					t.Fatalf(
						"parseGroundedness(%q) = %v, want error",
						test.response,
						got,
					)
				}

				return
			}

			if err != nil {
				t.Fatalf(
					"parseGroundedness(%q): unexpected error: %v",
					test.response,
					err,
				)
			}

			if got != test.want {
				t.Fatalf(
					"parseGroundedness(%q) = %v, want %v",
					test.response,
					got,
					test.want,
				)
			}
		})
	}
}
