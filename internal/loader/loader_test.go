package loader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rag-template/internal/document"
)

func writeTemp(t *testing.T, name, text string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)

	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	return path
}

func TestDispatchUnknownType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.xyz")

	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	_, err := Dispatch(path)
	if err == nil {
		t.Fatalf("Dispatch(%q) = nil error, want a no-loader error", path)
	}

	if !strings.Contains(err.Error(), "no loader") {
		t.Fatalf("Dispatch(%q) error = %q, want it to mention %q", path, err, "no loader")
	}

	if !strings.Contains(err.Error(), path) {
		t.Fatalf("Dispatch(%q) error = %q, want it to name the path", path, err)
	}
}

func TestMarkdownLoaderMatchesParseSections(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{
			name: "headings and content, preamble ignored",
			text: "# Title\n\n## Payments\n\nDue on the due date.\n\n## Late Fees\n\nA fee may apply.\n",
		},
		{
			name: "heading on the first line",
			text: "## Payments\ncontent\n",
		},
		{
			name: "no headings",
			text: "# Title\n\njust prose\n",
		},
		{
			name: "empty text",
			text: "",
		},
		{
			name: "heading without content",
			text: "## Empty\n## Next\nbody\n",
		},
		{
			name: "deeper heading stays in its section",
			text: "## One\nbody\n### Deeper\nmore\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeTemp(t, "doc.md", test.text)

			got, err := Dispatch(path)
			if err != nil {
				t.Fatalf("Dispatch(%q): %v", path, err)
			}

			want := document.ParseSections(test.text)

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Dispatch(%q) = %#v, want %#v", path, got, want)
			}
		})
	}
}

func TestTextLoader(t *testing.T) {
	t.Run("non-empty yields a section", func(t *testing.T) {
		path := writeTemp(t, "notes.txt", "hello world\n")

		got, err := Dispatch(path)
		if err != nil {
			t.Fatalf("Dispatch(%q): %v", path, err)
		}

		if len(got) != 1 {
			t.Fatalf("Dispatch(%q) = %d sections, want 1", path, len(got))
		}

		if got[0].Content != "hello world" {
			t.Fatalf("content = %q, want %q", got[0].Content, "hello world")
		}
	})

	t.Run("empty yields no sections", func(t *testing.T) {
		path := writeTemp(t, "empty.txt", "")

		got, err := Dispatch(path)
		if err != nil {
			t.Fatalf("Dispatch(%q): %v", path, err)
		}

		if len(got) != 0 {
			t.Fatalf("Dispatch(%q) = %d sections, want 0", path, len(got))
		}
	})

	t.Run("whitespace-only yields no sections", func(t *testing.T) {
		path := writeTemp(t, "blank.txt", "  \n\t\n  ")

		got, err := Dispatch(path)
		if err != nil {
			t.Fatalf("Dispatch(%q): %v", path, err)
		}

		// A whitespace-only file collapses to zero sections, so the caller's
		// existing "no sections found" guard applies unchanged.
		if len(got) != 0 {
			t.Fatalf("Dispatch(%q) = %d sections, want 0", path, len(got))
		}
	})

	t.Run("does not handle markdown", func(t *testing.T) {
		loader := &TextLoader{}
		if loader.Supports("doc.md") {
			t.Fatalf("TextLoader.Supports(\"doc.md\") = true, want false")
		}
	})
}

func TestPDFLoaderSupports(t *testing.T) {
	loader := &PDFLoader{}

	trueCases := []string{"x.pdf", "x.PDF", "x.Pdf", "/tmp/dir/a.pdf"}
	for _, path := range trueCases {
		if !loader.Supports(path) {
			t.Errorf("PDFLoader.Supports(%q) = false, want true", path)
		}
	}

	falseCases := []string{"x.md", "x.markdown", "x.txt", "x.doc", "xpdf", "pdf", "x.pdf.txt"}
	for _, path := range falseCases {
		if loader.Supports(path) {
			t.Errorf("PDFLoader.Supports(%q) = true, want false", path)
		}
	}
}

func TestPDFLoaderTextLayer(t *testing.T) {
	path := filepath.Join("testdata", "text-layer.pdf")

	sections, err := Dispatch(path)
	if err != nil {
		t.Fatalf("Dispatch(%q): %v", path, err)
	}

	wantContent := []string{"This is page one.", "This is page two."}

	if len(sections) != len(wantContent) {
		t.Fatalf("Dispatch(%q) = %d sections, want %d", path, len(sections), len(wantContent))
	}

	for index, section := range sections {
		if section.Page != index+1 {
			t.Errorf("section %d Page = %d, want %d", index, section.Page, index+1)
		}

		if want := fmt.Sprintf("Page %d", index+1); section.Title != want {
			t.Errorf("section %d Title = %q, want %q", index, section.Title, want)
		}

		if section.Content != wantContent[index] {
			t.Errorf("section %d Content = %q, want %q", index, section.Content, wantContent[index])
		}
	}
}

func TestPDFLoaderNoTextLayer(t *testing.T) {
	path := filepath.Join("testdata", "no-text-layer.pdf")

	sections, err := Dispatch(path)
	if err == nil {
		t.Fatalf("Dispatch(%q) = %d sections, nil error; want an OCR-required error", path, len(sections))
	}

	if len(sections) != 0 {
		t.Fatalf("Dispatch(%q) returned %d sections on error, want 0", path, len(sections))
	}

	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %q, want it to name the file %q", err, path)
	}

	if !strings.Contains(strings.ToUpper(err.Error()), "OCR") {
		t.Errorf("error = %q, want it to state that OCR is required", err)
	}
}

func TestPDFLoaderMalformed(t *testing.T) {
	path := filepath.Join("testdata", "malformed.pdf")

	var (
		sections []document.Section
		err      error
		panicked any
	)

	func() {
		defer func() {
			panicked = recover()
		}()

		sections, err = Dispatch(path)
	}()

	if panicked != nil {
		t.Fatalf("Dispatch(%q) panicked: %v", path, panicked)
	}

	if err == nil {
		t.Fatalf("Dispatch(%q) = %d sections, nil error; want an error", path, len(sections))
	}

	if len(sections) != 0 {
		t.Fatalf("Dispatch(%q) returned %d sections on error, want 0", path, len(sections))
	}
}

func TestDefaultRegistryPDFRouting(t *testing.T) {
	registry := DefaultRegistry()

	selected, err := registry.For("report.pdf")
	if err != nil {
		t.Fatalf("For(report.pdf): %v", err)
	}

	if _, ok := selected.(*PDFLoader); !ok {
		t.Fatalf("For(report.pdf) = %T, want *PDFLoader", selected)
	}

	markdownCases := []string{"doc.md", "doc.markdown"}
	for _, path := range markdownCases {
		selected, err := registry.For(path)
		if err != nil {
			t.Fatalf("For(%q): %v", path, err)
		}

		if _, ok := selected.(*MarkdownLoader); !ok {
			t.Fatalf("For(%q) = %T, want *MarkdownLoader", path, selected)
		}
	}

	selected, err = registry.For("notes.txt")
	if err != nil {
		t.Fatalf("For(notes.txt): %v", err)
	}

	if _, ok := selected.(*TextLoader); !ok {
		t.Fatalf("For(notes.txt) = %T, want *TextLoader", selected)
	}
}

// TestPDFLoaderMixedTextAndScan asserts a PDF with one readable page and one
// scanned page returns the readable section plus an explicit
// IncompleteExtractionError naming the OCR-required page.
func TestPDFLoaderMixedTextAndScan(t *testing.T) {
	path := filepath.Join("testdata", "mixed-text-scan.pdf")

	sections, err := Dispatch(path)

	var incomplete *IncompleteExtractionError
	if !errors.As(err, &incomplete) {
		t.Fatalf("Dispatch(%q) error = %v, want *IncompleteExtractionError", path, err)
	}

	if len(sections) != 1 {
		t.Fatalf("Dispatch(%q) = %d sections, want 1 (the readable page)", path, len(sections))
	}

	if sections[0].Title != "Page 1" || sections[0].Page != 1 {
		t.Fatalf("section = %+v, want the readable page titled %q with Page 1", sections[0], "Page 1")
	}

	if !reflect.DeepEqual(incomplete.Pages, []int{2}) {
		t.Fatalf("OCR-required pages = %v, want [2]", incomplete.Pages)
	}

	if !strings.Contains(strings.ToUpper(incomplete.Error()), "OCR") {
		t.Fatalf("diagnostic = %q, want it to mention OCR", incomplete.Error())
	}
}

// TestPDFLoaderNullPage asserts a PDF whose page object is null is rejected with
// a controlled error and never panics.
func TestPDFLoaderNullPage(t *testing.T) {
	path := filepath.Join("testdata", "null-page.pdf")

	var (
		sections []document.Section
		err      error
		panicked any
	)

	func() {
		defer func() {
			panicked = recover()
		}()

		sections, err = Dispatch(path)
	}()

	if panicked != nil {
		t.Fatalf("Dispatch(%q) panicked: %v", path, panicked)
	}

	if err == nil {
		t.Fatalf("Dispatch(%q) = %d sections, nil error; want a controlled error", path, len(sections))
	}

	if len(sections) != 0 {
		t.Fatalf("Dispatch(%q) returned %d sections on error, want 0", path, len(sections))
	}
}

// stubLoader is a test-only Loader that claims the given extensions and
// returns one sentinel section, so a test can prove which loader the registry
// selected rather than merely which concrete type is registered.
type stubLoader struct {
	extensions []string
	marker     string
}

func (s *stubLoader) Supports(path string) bool {
	for _, extension := range s.extensions {
		if strings.EqualFold(filepath.Ext(path), extension) {
			return true
		}
	}

	return false
}

func (s *stubLoader) Load(string) ([]document.Section, error) {
	return []document.Section{{Title: "stub", Content: s.marker}}, nil
}

func TestRegistryOverride(t *testing.T) {
	registry := DefaultRegistry()

	override := &stubLoader{extensions: []string{".md"}, marker: "override-wins"}
	registry.Register(override)

	// A later registration takes precedence over the built-in MarkdownLoader,
	// which also supports ".md".
	got, err := registry.Load("any.md")
	if err != nil {
		t.Fatalf("Load(any.md): %v", err)
	}

	want := []document.Section{{Title: "stub", Content: "override-wins"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(any.md) = %#v, want the override loader's output %#v", got, want)
	}

	// An extension the override does not claim still resolves to the built-in.
	selected, err := registry.For("doc.txt")
	if err != nil {
		t.Fatalf("For(doc.txt): %v", err)
	}

	if _, ok := selected.(*TextLoader); !ok {
		t.Fatalf("For(doc.txt) = %T, want *TextLoader", selected)
	}
}
