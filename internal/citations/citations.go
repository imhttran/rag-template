// Package citations parses the [source - section] citations a model writes in
// an answer, validates each against the retrieved documents, and repairs an
// answer so that no invalid citation is rendered as if valid.
//
// The parser here is the logic proven in cmd/eval: it is lexical over the answer
// text and the retrieved documents and names no model. It accepts an optional
// page segment ([source - section - p.N]); a page citation is valid only when a
// retrieved document matches the source and section and carries that page.
//
// The trailing segment of a bracket is treated as a page only when it matches
// "p.<positive int>". A section that itself contains " - " (for example
// "Late Payments - Grace Period") is therefore preserved whole rather than
// being mistaken for a section/page split.
package citations

import (
	"strconv"
	"strings"

	"rag-template/internal/retrieval"
)

// Citation is a parsed [source - section] pair with an optional page. Page is 0
// when the citation makes no page claim (the legacy form).
type Citation struct {
	Source  string
	Section string
	Page    int
}

// Span is a parsed citation together with the byte range of its brackets within
// the parsed text. Start is the index of the opening '[' and End is the index
// just past the closing ']'. Callers that need the surrounding claim text (for
// example cmd/eval) can slice the original text with the span instead of
// re-implementing a bracket scanner.
type Span struct {
	Citation Citation
	Start    int
	End      int
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

// Parse extracts every [source - section] or [source - section - p.N] citation
// from answer, in order. A bracket that is unclosed, that has no " - "
// separator, or whose trailing segment is neither part of the section nor
// "p<digits>" is not a citation and is skipped.
func Parse(answer string) []Citation {
	spans := ParseSpans(answer)

	citations := make([]Citation, 0, len(spans))
	for _, span := range spans {
		citations = append(citations, span.Citation)
	}

	return citations
}

// ParseSpans extracts every citation from answer, in order, together with the
// byte range of its brackets. It is the single bracket scanner in the
// repository: Parse and every consumer (cmd/eval) build on it, so citation
// parsing cannot drift between callers.
func ParseSpans(answer string) []Span {
	var spans []Span

	remaining := answer
	offset := 0

	for {
		open := strings.Index(remaining, "[")
		if open == -1 {
			break
		}

		start := offset + open

		closeRel := strings.Index(remaining[open:], "]")
		if closeRel == -1 {
			break
		}

		closeIndex := open + closeRel

		citation := NormalizeCitation(remaining[open+1 : closeIndex])

		if parsed, ok := parseCitation(citation); ok {
			spans = append(spans, Span{
				Citation: parsed,
				Start:    start,
				End:      offset + closeIndex + 1,
			})
		}

		offset += closeIndex + 1
		remaining = remaining[closeIndex+1:]
	}

	return spans
}

// parseCitation parses the inside of a citation bracket. It accepts
// "source - section" and "source - section - p.N". A trailing segment is a page
// only when it matches "p.<positive int>"; otherwise it stays part of the
// section, so a section title containing " - " is preserved whole. A bracket
// with no " - " separator is not a citation.
func parseCitation(citation string) (Citation, bool) {
	source, rest, found := strings.Cut(citation, " - ")
	if !found {
		return Citation{}, false
	}

	parsed := Citation{
		Source: strings.TrimSpace(source),
	}

	// A page is claimed only when the trailing " - " segment is "p.N". When it
	// is not (or there is no trailing segment), keep the whole remainder as the
	// section so a section title containing " - " is not split.
	if section, pageSegment, hasSegment := strings.Cut(rest, " - "); hasSegment {
		if page, ok := parsePageSegment(pageSegment); ok {
			parsed.Section = strings.TrimSpace(section)
			parsed.Page = page

			return parsed, true
		}
	}

	parsed.Section = strings.TrimSpace(rest)

	return parsed, true
}

// parsePageSegment parses a page segment of the form "p.N" where N is a
// positive integer, returning the page number.
func parsePageSegment(segment string) (int, bool) {
	trimmed := strings.TrimSpace(segment)

	digits, found := strings.CutPrefix(trimmed, "p.")
	if !found {
		return 0, false
	}

	page, err := strconv.Atoi(strings.TrimSpace(digits))
	if err != nil || page <= 0 {
		return 0, false
	}

	return page, true
}

// Valid reports whether citation resolves to a retrieved document's source and
// section, and — when the citation claims a page — whether that document
// carries the same page.
func Valid(citation Citation, documents []retrieval.Document) bool {
	for _, doc := range documents {
		if NormalizeSource(doc.Source) != NormalizeSource(citation.Source) ||
			doc.Section != citation.Section {
			continue
		}

		if citation.Page == 0 {
			return true
		}

		if doc.Page == citation.Page {
			return true
		}
	}

	return false
}

// Validity counts the citations in answer and how many of them name a retrieved
// document. It reproduces the exact valid/total counts of the cmd/eval parser
// for the legacy form and extends them to the page form.
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
	spans := ParseSpans(answer)

	if len(spans) == 0 {
		return answer, 0
	}

	var (
		builder  strings.Builder
		stripped int
		cursor   int
	)

	for _, span := range spans {
		if Valid(span.Citation, documents) {
			continue
		}

		prefix := answer[cursor:span.Start]
		suffix := answer[span.End:]

		builder.WriteString(prefix)

		if needsSeparator(prefix, suffix) {
			builder.WriteString(" ")
		}

		cursor = span.End
		stripped++
	}

	builder.WriteString(answer[cursor:])

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
