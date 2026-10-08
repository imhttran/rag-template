package loader

import (
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
