// Package loader defines a pluggable document content-extraction interface.
//
// A Loader detects whether it supports a file (by extension) and extracts the
// file's content as []document.Section, which is the boundary type consumed by
// internal/chunking and the ingest command.
//
// Future format loaders (HTML, PDF, OCR) register themselves with a Registry
// via Register and are dispatched without any change to callers. The Markdown
// loader wraps document.ParseSections so existing Markdown semantics are
// preserved byte-for-byte; the plain-text loader maps a file's whole content
// into a single Section.
package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rag-template/internal/document"
)

// Loader extracts the sections of a document. Implementations report whether
// they support a path, then extract that file's content as sections. Detection
// should be cheap -- the shipped loaders match on the file extension -- and
// must not read the whole file unless required. A loader whose format needs a
// stronger check (for example a magic-byte sniff) may do so inside Supports.
type Loader interface {
	// Supports reports whether this loader can handle path.
	Supports(path string) bool

	// Load reads path and returns its sections. A file that parses to no
	// sections returns an empty (nil) slice and a nil error; the caller is
	// responsible for deciding whether that is an error.
	Load(path string) ([]document.Section, error)
}

// ErrNoLoader indicates that no registered loader supports the given path.
type ErrNoLoader struct {
	Path string
}

func (e *ErrNoLoader) Error() string {
	return fmt.Sprintf("no loader for %s", e.Path)
}

// Registry holds a set of loaders and resolves the loader for a path.
type Registry struct {
	loaders []Loader
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// DefaultRegistry is the process-wide registry preloaded with the loaders that
// ship with this package (Markdown and plain text). Callers that need a
// different set can build their own registry; the shipped defaults are not
// changed by doing so.
func DefaultRegistry() *Registry {
	registry := NewRegistry()
	registry.Register(&MarkdownLoader{})
	registry.Register(&TextLoader{})

	return registry
}

// Register adds a loader. Later registrations take precedence over earlier
// ones, so callers can override a built-in loader without editing the
// dispatcher.
func (r *Registry) Register(loader Loader) {
	r.loaders = append(r.loaders, loader)
}

// For returns the loader that supports path, or an *ErrNoLoader naming the
// path when none matches.
func (r *Registry) For(path string) (Loader, error) {
	for index := len(r.loaders) - 1; index >= 0; index-- {
		if r.loaders[index].Supports(path) {
			return r.loaders[index], nil
		}
	}

	return nil, &ErrNoLoader{Path: path}
}

// Load resolves a loader for path and extracts its sections.
func (r *Registry) Load(path string) ([]document.Section, error) {
	loader, err := r.For(path)
	if err != nil {
		return nil, err
	}

	return loader.Load(path)
}

// Dispatch loads path using the DefaultRegistry.
func Dispatch(path string) ([]document.Section, error) {
	return DefaultRegistry().Load(path)
}

// MarkdownLoader extracts "## " sections from Markdown, delegating to
// document.ParseSections so behaviour is identical to the historical
// parser.
type MarkdownLoader struct{}

func (l *MarkdownLoader) Supports(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

func (l *MarkdownLoader) Load(path string) ([]document.Section, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return document.ParseSections(string(data)), nil
}

// TextLoader extracts a plain-text file. A non-empty file maps to a single
// Section titled with the file's base name; an empty (or whitespace-only) file
// yields no sections, preserving the caller's existing "no sections found"
// guard.
type TextLoader struct{}

func (l *TextLoader) Supports(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".txt")
}

func (l *TextLoader) Load(path string) ([]document.Section, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil, nil
	}

	return []document.Section{{
		Title:   filepath.Base(path),
		Content: content,
	}}, nil
}
