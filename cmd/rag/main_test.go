package main

import (
	"strings"
	"testing"
)

func TestResolveQuestion(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		args       []string
		stdin      string
		want       string
		wantErr    bool
	}{
		{
			name:       "arguments win over the setting",
			configured: "from the setting",
			args:       []string{"from", "the", "arguments"},
			stdin:      "from stdin\n",
			want:       "from the arguments",
		},
		{
			name:       "blank arguments fall through to the setting",
			configured: "from the setting",
			args:       []string{"  "},
			want:       "from the setting",
		},
		{
			name:       "the setting is used when there are no arguments",
			configured: "from the setting",
			stdin:      "from stdin\n",
			want:       "from the setting",
		},
		{
			name:  "a typed line is used when nothing else is set",
			stdin: "  typed question \n",
			want:  "typed question",
		},
		{
			name:    "a blank typed line is not a question",
			stdin:   "   \n",
			wantErr: true,
		},
		{
			name:    "no input at all",
			stdin:   "",
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveQuestion(
				test.configured,
				test.args,
				strings.NewReader(test.stdin),
				false,
			)

			if test.wantErr {
				if err == nil {
					t.Fatalf(
						"resolveQuestion(...) = %q, want an error",
						got,
					)
				}

				return
			}

			if err != nil {
				t.Fatalf(
					"resolveQuestion(...): unexpected error: %v",
					err,
				)
			}

			if got != test.want {
				t.Fatalf(
					"resolveQuestion(...) = %q, want %q",
					got,
					test.want,
				)
			}
		})
	}
}
