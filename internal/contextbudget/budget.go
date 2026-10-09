// Package contextbudget selects a subset of ranked retrieval documents whose
// estimated size fits within a configured budget.
//
// The builder is pure and deterministic: it performs no I/O, imports no
// database or network packages, and returns the same selection for identical
// input, with a stable ordering. It never returns a selection whose estimated
// size exceeds the budget, and it spends the budget fairly across sections so
// one long section cannot starve the others.
package contextbudget

import "rag-template/internal/retrieval"

// Estimate returns the deterministic estimated size of a single document chunk.
//
// No repository-wide size or token estimator exists, so the estimate is the
// length of the chunk content in bytes plus a small fixed per-chunk overhead
// accounting for the surrounding prompt block (source/section labels and
// separators). It is exported so a deliberate change to the metric is visible
// to callers rather than accidental.
func Estimate(doc retrieval.Document) int {
	return len(doc.Content) + chunkOverhead
}

// chunkOverhead is the fixed per-chunk cost Estimate adds to the content
// length. It is a small, documented constant so the estimator stays a pure
// function of the document fields.
const chunkOverhead = 16

// section groups the input indexes that belong to one (source, section) pair,
// mirroring retrieval.SectionKey. Indexes are recorded in input (rank) order.
type section struct {
	source  string
	name    string
	indexes []int
}

// Build selects a subset of documents whose estimated size never exceeds
// budget, prioritising higher-ranked sections and round-robining across
// sections.
//
// documents must be ordered best-first (as retrieval results are). A budget of
// zero or less disables the builder and returns documents unchanged, preserving
// the current chunk-count behaviour. An empty input yields an empty output.
//
// Selection walks the sections in order of first appearance (i.e. by the rank
// of their best document) and, on each pass, offers every remaining section one
// chunk before any section receives a second. Within a section chunks are taken
// in input order. A chunk whose own estimate exceeds the remaining budget is
// skipped and never added, so a single oversize chunk cannot break the
// never-exceed guarantee. The returned slice preserves the input ordering of
// the documents it keeps.
func Build(documents []retrieval.Document, budget int) []retrieval.Document {
	if budget <= 0 {
		return documents
	}

	if len(documents) == 0 {
		return documents
	}

	// Group documents by (source, section), keeping sections in order of first
	// appearance so higher-ranked sections are considered first. Within each
	// section, chunk indexes are recorded in input order.
	order := make([]*section, 0, len(documents))
	byKey := make(map[retrieval.SectionKey]*section, len(documents))

	for index, doc := range documents {
		key := retrieval.SectionKey{Source: doc.Source, Section: doc.Section}

		group, ok := byKey[key]
		if !ok {
			group = &section{source: doc.Source, name: doc.Section}
			byKey[key] = group
			order = append(order, group)
		}

		group.indexes = append(group.indexes, index)
	}

	selected := make([]bool, len(documents))
	remaining := budget

	// Round-robin: one chunk per section per pass, so each section contributes
	// a chunk before any section contributes a second.
	maxPasses := 0

	for _, group := range order {
		if len(group.indexes) > maxPasses {
			maxPasses = len(group.indexes)
		}
	}

	for pass := 0; pass < maxPasses; pass++ {
		for _, group := range order {
			if pass >= len(group.indexes) {
				continue
			}

			index := group.indexes[pass]
			cost := Estimate(documents[index])

			if cost > remaining {
				// This chunk alone does not fit; skip it so the never-exceed
				// guarantee holds even for oversize chunks.
				continue
			}

			selected[index] = true
			remaining -= cost
		}
	}

	kept := make([]retrieval.Document, 0, len(documents))

	for index, doc := range documents {
		if selected[index] {
			kept = append(kept, doc)
		}
	}

	return kept
}

// EstimateAll returns the summed estimated size of documents. It is a
// convenience for callers that need to check the budget of a selection.
func EstimateAll(documents []retrieval.Document) int {
	total := 0

	for _, doc := range documents {
		total += Estimate(doc)
	}

	return total
}
