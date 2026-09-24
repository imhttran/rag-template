package document

import "strings"

type Section struct {
	Title   string
	Content string
}

// ParseSections splits markdown text into "## " sections. Content before the
// first heading is ignored.
func ParseSections(text string) []Section {
	// Prefixing a newline lets the same "\n## " split catch a heading on the
	// first line too.
	parts := strings.Split("\n"+text, "\n## ")

	var sections []Section

	for _, part := range parts[1:] {
		title, content, _ := strings.Cut(part, "\n")

		sections = append(sections, Section{
			Title:   strings.TrimSpace(title),
			Content: strings.TrimSpace(content),
		})
	}

	return sections
}
