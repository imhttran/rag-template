package retrieval

import (
	"fmt"
	"strings"
	"time"
)

// Filter is an optional metadata filter applied to retrieval queries. It
// narrows the candidate set by source prefix, language, and ingested-at date
// range. The zero value (every field unset) produces an empty SQL fragment and
// no bound arguments, so queries built with a zero Filter are byte-identical to
// the unfiltered implementation.
//
// Document type is deliberately NOT a filter dimension: the documents table has
// no doc-type column, so there is nothing to filter on. It is out of scope.
//
// The Language field targets the stored documents.language column (added by
// migration 003_add_language.sql). A NULL or empty stored language is treated as
// unset by ingestion; the filter matches the literal column value, so callers
// who want to include unset rows should not set Language.
type Filter struct {
	// SourcePrefix matches documents whose source begins with this prefix.
	SourcePrefix string

	// Language matches documents whose stored language column equals this value
	// exactly. Empty means no language restriction.
	Language string

	// IngestedFrom matches documents ingested at or after this instant.
	IngestedFrom *time.Time

	// IngestedTo matches documents ingested at or before this instant.
	IngestedTo *time.Time
}

// IsZero reports whether the filter carries no criteria.
func (f Filter) IsZero() bool {
	return f.SourcePrefix == "" &&
		f.Language == "" &&
		f.IngestedFrom == nil &&
		f.IngestedTo == nil
}

// filterFragment builds the SQL predicate fragment for the filter along with
// the values to bind. startIndex is the index of the first placeholder to emit
// (i.e. the next free $N), so the fragment composes with any parameters that
// precede it in the query.
//
// Only predicate structure (clause text and placeholder numbering) is dynamic.
// Every filter value is returned in args as a bound parameter; no value is ever
// concatenated into the returned SQL text.
//
// A zero-value filter returns ("", nil), so callers append nothing and bind
// nothing, preserving the unfiltered behavior exactly.
func filterFragment(f Filter, startIndex int) (string, []any) {
	if f.IsZero() {
		return "", nil
	}

	clauses := make([]string, 0, 4)
	args := make([]any, 0, 4)
	next := startIndex

	if f.SourcePrefix != "" {
		clauses = append(clauses, fmt.Sprintf("source LIKE $%d", next))
		args = append(args, escapeLikePrefix(f.SourcePrefix)+"%")
		next++
	}

	if f.Language != "" {
		clauses = append(clauses, fmt.Sprintf("language = $%d", next))
		args = append(args, f.Language)
		next++
	}

	if f.IngestedFrom != nil {
		clauses = append(clauses, fmt.Sprintf("ingested_at >= $%d", next))
		args = append(args, *f.IngestedFrom)
		next++
	}

	if f.IngestedTo != nil {
		clauses = append(clauses, fmt.Sprintf("ingested_at <= $%d", next))
		args = append(args, *f.IngestedTo)
	}

	return strings.Join(clauses, " AND "), args
}

// escapeLikePrefix escapes the LIKE metacharacters (%, _, and the escape
// character itself) in a literal prefix so the value is matched literally.
func escapeLikePrefix(prefix string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"%", "\\%",
		"_", "\\_",
	)

	return replacer.Replace(prefix)
}
