package document

import (
	"slices"
	"testing"
)

func TestParseSections(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []Section
	}{
		{
			name: "headings and content, preamble ignored",
			text: "# Title\n\n## Payments\n\nDue on the due date.\n\n## Late Fees\n\nA fee may apply.\n",
			want: []Section{
				{Title: "Payments", Content: "Due on the due date."},
				{Title: "Late Fees", Content: "A fee may apply."},
			},
		},
		{
			name: "heading on the first line",
			text: "## Payments\ncontent\n",
			want: []Section{
				{Title: "Payments", Content: "content"},
			},
		},
		{
			name: "no headings",
			text: "# Title\n\njust prose\n",
			want: nil,
		},
		{
			name: "empty text",
			text: "",
			want: nil,
		},
		{
			name: "heading without content",
			text: "## Empty\n## Next\nbody\n",
			want: []Section{
				{Title: "Empty", Content: ""},
				{Title: "Next", Content: "body"},
			},
		},
		{
			name: "deeper heading stays in its section",
			text: "## One\nbody\n### Deeper\nmore\n",
			want: []Section{
				{Title: "One", Content: "body\n### Deeper\nmore"},
			},
		},
		{
			name: "mid-line hashes are not a heading",
			text: "## One\nsee ## two here\n",
			want: []Section{
				{Title: "One", Content: "see ## two here"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ParseSections(test.text)

			if !slices.Equal(got, test.want) {
				t.Fatalf(
					"ParseSections(%q) = %#v, want %#v",
					test.text,
					got,
					test.want,
				)
			}
		})
	}
}
