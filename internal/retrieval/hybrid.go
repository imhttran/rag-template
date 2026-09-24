package retrieval

import "sort"

const rrfK = 60

type fusedDocument struct {
	document   Document
	score      float64
	vectorRank int
}

func Fuse(
	vectorDocuments []Document,
	keywordDocuments []Document,
	topK int,
) []Document {
	scores := make(map[int64]*fusedDocument)

	addRanking := func(documents []Document, isVector bool) {
		for i, doc := range documents {
			rank := i + 1

			entry, ok := scores[doc.ID]
			if !ok {
				entry = &fusedDocument{
					document:   doc,
					vectorRank: len(vectorDocuments) + 1,
				}
				scores[doc.ID] = entry
			}

			entry.score += 1.0 / float64(rrfK+rank)

			if isVector {
				entry.vectorRank = rank
				entry.document.Similarity = doc.Similarity
			} else {
				entry.document.KeywordScore = doc.KeywordScore
			}
		}
	}

	addRanking(vectorDocuments, true)
	addRanking(keywordDocuments, false)

	fused := make([]*fusedDocument, 0, len(scores))

	for _, entry := range scores {
		fused = append(fused, entry)
	}

	sort.Slice(fused, func(i, j int) bool {
		if fused[i].score != fused[j].score {
			return fused[i].score > fused[j].score
		}

		if fused[i].vectorRank != fused[j].vectorRank {
			return fused[i].vectorRank < fused[j].vectorRank
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
