package document

import "strings"

// Section is one "## " section of a document. Page is the 1-based source page
// the section was extracted from (PDF loaders emit one section per page); it is
// 0 when the source has no page information (Markdown, plain text).
type Section struct {
	Title   string
	Content string
	Page    int
}

// ParseSections splits markdown text into "## " sections. Content before the
// first heading is ignored. Markdown carries no page information, so every
// section's Page is 0; paged formats populate Page through SetPages.
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

// SetPages assigns page numbers to sections in order, one page per section, so
// a paged loader (for example a PDF extractor that emits one section per page)
// can attach provenance. The first section is page 1. When pages has fewer
// entries than sections, the remaining sections keep Page 0 (unset). Sections
// with an already-set Page are left unchanged.
func SetPages(sections []Section, pages []int) {
	for index := range sections {
		if index >= len(pages) {
			return
		}

		if sections[index].Page == 0 {
			sections[index].Page = pages[index]
		}
	}
}
