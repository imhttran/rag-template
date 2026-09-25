package retrieval

import "sort"

const rrfK = 60

type fusedDocument struct {
	document Document
	score    float64
	bestRank int
}

func Fuse(
	vectorDocuments []Document,
	keywordDocuments []Document,
	topK int,
) []Document {
	return FuseRankings(
		[][]Document{
			vectorDocuments,
			keywordDocuments,
		},
		topK,
	)
}

func FuseRankings(
	rankings [][]Document,
	topK int,
) []Document {
	scores := make(map[int64]*fusedDocument)

	for _, documents := range rankings {
		for i, doc := range documents {
			rank := i + 1

			entry, ok := scores[doc.ID]
			if !ok {
				entry = &fusedDocument{
					document: doc,
					bestRank: rank,
				}
				scores[doc.ID] = entry
			}

			entry.score += 1.0 / float64(rrfK+rank)

			if rank < entry.bestRank {
				entry.bestRank = rank
			}

			if doc.Similarity > entry.document.Similarity {
				entry.document.Similarity = doc.Similarity
			}

			if doc.KeywordScore > entry.document.KeywordScore {
				entry.document.KeywordScore = doc.KeywordScore
			}
		}
	}

	fused := make([]*fusedDocument, 0, len(scores))

	for _, entry := range scores {
		fused = append(fused, entry)
	}

	sort.Slice(fused, func(i, j int) bool {
		if fused[i].score != fused[j].score {
			return fused[i].score > fused[j].score
		}

		if fused[i].bestRank != fused[j].bestRank {
			return fused[i].bestRank < fused[j].bestRank
		}

		return fused[i].document.ID < fused[j].document.ID
	})

	topK = min(topK, len(fused))

	result := make([]Document, 0, topK)

	for _, entry := range fused[:topK] {
		doc := entry.document
		doc.FusionScore = entry.score

		result = append(result, doc)
	}

	return result
}
