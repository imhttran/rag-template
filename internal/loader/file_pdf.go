package loader

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"

	"rag-template/internal/document"
)

// PDFLoader extracts the text layer of a PDF, one Section per page. It does not
// perform OCR: a page whose text layer is empty is reported through an explicit
// error (or, for a partially scanned document, an *IncompleteExtractionError
// naming the affected pages) so an ingest never silently treats missing text as
// empty content.
//
// Source identity (Title) is left unset because the caller derives it from the
// path; each Section instead carries a "Page N" title and its 1-based page
// number in Section.Page (RAG-017).
type PDFLoader struct{}

// Supports reports whether path names a PDF. Matching is case-insensitive and
// restricted to ".pdf" so Markdown and plain-text dispatch is unaffected.
func (l *PDFLoader) Supports(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".pdf")
}

// IncompleteExtractionError reports that some pages of a PDF had no extractable
// text layer while others did. The extracted sections are still returned (so the
// readable pages are preserved), and the error names the pages that require OCR.
type IncompleteExtractionError struct {
	// Path is the PDF whose extraction was incomplete.
	Path string

	// Pages are the 1-based page numbers with no extractable text layer.
	Pages []int
}

func (e *IncompleteExtractionError) Error() string {
	return fmt.Sprintf(
		"pdf %s: no text layer on page(s) %s: OCR is required for those pages",
		e.Path,
		formatPages(e.Pages),
	)
}

// formatPages renders page numbers as a comma-separated list.
func formatPages(pages []int) string {
	parts := make([]string, len(pages))

	for index, page := range pages {
		parts[index] = strconv.Itoa(page)
	}

	return strings.Join(parts, ", ")
}

// Load extracts one Section per page, in page order, titled "Page N" with Page
// set to N and Content set to that page's text layer.
//
// Error behaviour:
//   - an unreadable or malformed/truncated PDF, or a panic from the parser,
//     returns a controlled error (never a panic);
//   - a page whose object is null is rejected with a controlled error;
//   - a PDF whose pages all lack a text layer returns a single OCR-required
//     error and no sections;
//   - a mixed PDF returns the pages that had text together with an
//     *IncompleteExtractionError naming the pages that require OCR.
func (l *PDFLoader) Load(path string) (sections []document.Section, err error) {
	// The library can panic on some malformed/truncated inputs; convert any
	// such panic into an error so callers never observe a panic.
	defer func() {
		if recovered := recover(); recovered != nil {
			sections = nil
			err = fmt.Errorf("parse pdf %s: %v", path, recovered)
		}
	}()

	file, reader, openErr := pdf.Open(path)
	if openErr != nil {
		return nil, fmt.Errorf("open pdf %s: %w", path, openErr)
	}

	defer func() {
		_ = file.Close()
	}()

	pageCount := reader.NumPage()
	if pageCount <= 0 {
		return nil, fmt.Errorf("parse pdf %s: document has no pages", path)
	}

	sections = make([]document.Section, 0, pageCount)
	missing := make([]int, 0, pageCount)

	for pageNumber := 1; pageNumber <= pageCount; pageNumber++ {
		page := reader.Page(pageNumber)

		if page.V.IsNull() {
			return nil, fmt.Errorf(
				"parse pdf %s: page %d is a null object",
				path,
				pageNumber,
			)
		}

		content, textErr := page.GetPlainText(nil)
		if textErr != nil {
			return nil, fmt.Errorf(
				"extract text from %s page %d: %w",
				path,
				pageNumber,
				textErr,
			)
		}

		text := strings.TrimSpace(content)
		if text == "" {
			missing = append(missing, pageNumber)

			continue
		}

		sections = append(sections, document.Section{
			Title:   fmt.Sprintf("Page %d", pageNumber),
			Content: text,
			Page:    pageNumber,
		})
	}

	switch {
	case len(missing) == pageCount:
		return nil, fmt.Errorf(
			"pdf %s has no extractable text layer: OCR is required",
			path,
		)
	case len(missing) > 0:
		return sections, &IncompleteExtractionError{Path: path, Pages: missing}
	default:
		return sections, nil
	}
}
