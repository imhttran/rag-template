// Package citations parses the [source - section] citations a model writes in
// an answer, validates each against the retrieved documents, and repairs an
// answer so that no invalid citation is rendered as if valid.
//
// The parser here is the logic proven in cmd/eval: it is lexical over the answer
// text and the retrieved documents and names no model.
package citations

import (
	"strings"

	"rag-template/internal/retrieval"
)

// Citation is a parsed [source - section] pair.
type Citation struct {
	Source  string
	Section string
}

// NormalizeCitation normalizes the dashes a model may emit (-, en dash, em dash)
// so a citation can be split on a single separator.
func NormalizeCitation(citation string) string {
	citation = strings.ReplaceAll(citation, "\u2013", "-")
	citation = strings.ReplaceAll(citation, "\u2014", "-")

	return citation
}

// NormalizeSource makes a citation's source comparable to a document's source.
// Models tend to drop the ".md" suffix and vary capitalization; neither
// changes which document a citation points at.
func NormalizeSource(source string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(source)), ".md")
}

// Parse extracts every [source - section] citation from answer, in order. A
// bracket that is unclosed, or that has no " - " separator, is not a citation
// and is skipped, matching the eval parser.
func Parse(answer string) []Citation {
	var citations []Citation

	remaining := answer

	for {
		_, rest, found := strings.Cut(remaining, "[")
		if !found {
			break
		}

		citation, rest, found := strings.Cut(rest, "]")
		citation = NormalizeCitation(citation)
		if !found {
			break
		}

		remaining = rest

		// Only [source - section] counts as a citation.
		source, section, found := strings.Cut(citation, " - ")
		if !found {
			continue
		}

		citations = append(citations, Citation{
			Source:  strings.TrimSpace(source),
			Section: strings.TrimSpace(section),
		})
	}

	return citations
}

// Valid reports whether citation resolves to a retrieved document's source and
// section.
func Valid(citation Citation, documents []retrieval.Document) bool {
	for _, doc := range documents {
		if NormalizeSource(doc.Source) == NormalizeSource(citation.Source) &&
			doc.Section == citation.Section {
			return true
		}
	}

	return false
}

// Validity counts the citations in answer and how many of them name a retrieved
// document. It reproduces the exact valid/total counts of the cmd/eval parser.
func Validity(answer string, documents []retrieval.Document) (valid int, total int) {
	for _, citation := range Parse(answer) {
		total++

		if Valid(citation, documents) {
			valid++
		}
	}

	return valid, total
}

// Strip removes every citation from answer that does not resolve to a retrieved
// document, leaving valid citations and all surrounding prose intact. It
// returns the stripped answer and how many citations were removed.
//
// When an invalid citation sits between two word characters (for example
// "foo[a.md - X]bar"), removing it would join the surrounding words into
// "foobar". Strip inserts a single space in that case so prose stays readable
// and words are not fused. It never removes anything but the invalid citation
// and that separating space.
func Strip(answer string, documents []retrieval.Document) (string, int) {
	var (
		builder  strings.Builder
		stripped int
	)

	remaining := answer

	for {
		open := strings.Index(remaining, "[")
		if open == -1 {
			builder.WriteString(remaining)

			break
		}

		closeIndex := strings.Index(remaining[open:], "]")
		if closeIndex == -1 {
			builder.WriteString(remaining)

			break
		}

		closeIndex += open

		citation := NormalizeCitation(remaining[open+1 : closeIndex])

		source, section, found := strings.Cut(citation, " - ")
		if !found {
			// Not a citation: keep it verbatim.
			builder.WriteString(remaining[:closeIndex+1])
			remaining = remaining[closeIndex+1:]

			continue
		}

		parsed := Citation{
			Source:  strings.TrimSpace(source),
			Section: strings.TrimSpace(section),
		}

		if Valid(parsed, documents) {
			builder.WriteString(remaining[:closeIndex+1])
		} else {
			// Drop the invalid citation but keep the surrounding text. If the
			// removal would fuse two word characters across the gap (no
			// whitespace on either side), insert one space so words do not
			// merge.
			prefix := remaining[:open]
			suffix := remaining[closeIndex+1:]

			builder.WriteString(prefix)

			if needsSeparator(prefix, suffix) {
				builder.WriteString(" ")
			}

			stripped++
		}

		remaining = remaining[closeIndex+1:]
	}

	return builder.String(), stripped
}

// needsSeparator reports whether removing text between prefix and suffix would
// join the last character of prefix to the first character of suffix, turning
// two words into one ("foo" + "bar" -> "foobar").
func needsSeparator(prefix, suffix string) bool {
	if prefix == "" || suffix == "" {
		return false
	}

	last := prefix[len(prefix)-1]
	first := suffix[0]

	return isWordByte(last) && isWordByte(first)
}

// isWordByte reports whether b can be part of a word: a letter, digit, or
// underscore. Whitespace and punctuation are not word bytes, so a citation that
// already abuts punctuation or space needs no injected separator.
func isWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '_':
		return true
	default:
		return false
	}
}
