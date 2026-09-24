package answerability

import "testing"

func TestParseDecision(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     bool
		wantErr  bool
	}{
		{
			name:     "answerable",
			response: "ANSWERABLE",
			want:     true,
		},
		{
			name:     "not answerable",
			response: "NOT_ANSWERABLE",
			want:     false,
		},
		{
			name:     "not-answerable must not read as answerable",
			response: "NOT_ANSWERABLE.",
			want:     false,
		},
		{
			name:     "lowercase",
			response: "answerable",
			want:     true,
		},
		{
			name:     "mixed case",
			response: "Not_Answerable",
			want:     false,
		},
		{
			name:     "surrounding whitespace",
			response: "\n  ANSWERABLE \n",
			want:     true,
		},
		{
			name:     "trailing prose",
			response: "ANSWERABLE because the policy states the period.",
			want:     true,
		},
		{
			name:     "prose only",
			response: "The evidence is sufficient.",
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
			got, err := parseDecision(test.response)

			if test.wantErr {
				if err == nil {
					t.Fatalf(
						"parseDecision(%q) = %v, want error",
						test.response,
						got,
					)
				}

				return
			}

			if err != nil {
				t.Fatalf(
					"parseDecision(%q): unexpected error: %v",
					test.response,
					err,
				)
			}

			if got != test.want {
				t.Fatalf(
					"parseDecision(%q) = %v, want %v",
					test.response,
					got,
					test.want,
				)
			}
		})
	}
}
