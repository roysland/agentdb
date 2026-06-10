package search

import (
	"math"
	"sort"

	"github.com/roysland/agentdb/internal/store"
)

// SymbolHit represents a ranked search result.
type SymbolHit struct {
	Symbol store.Symbol `json:"symbol"`
	Score  float64      `json:"score"`
}

// MemoryHit represents a ranked memory search result.
type MemoryHit struct {
	store.Memory
	Score float64 `json:"score"`
}

// CosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns 0 if either vector is empty or they have different lengths.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// RankSymbolsByCosine computes cosine similarity between queryEmbedding
// and each symbol's embedding, returning the top `limit` results sorted
// by score descending. Symbols with nil embeddings are skipped.
func RankSymbolsByCosine(symbols []store.Symbol, queryEmbedding []float32, limit int) []SymbolHit {
	if limit <= 0 {
		limit = 10
	}

	hits := make([]SymbolHit, 0, len(symbols))
	for _, sym := range symbols {
		if len(sym.Embedding) == 0 {
			continue
		}
		score := CosineSimilarity(sym.Embedding, queryEmbedding)
		hits = append(hits, SymbolHit{Symbol: sym, Score: score})
	}

	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})

	if len(hits) > limit {
		hits = hits[:limit]
	}

	return hits
}

// RankMemoriesByCosine computes cosine similarity between queryEmbedding and each
// memory embedding, returning the top `limit` results sorted by score descending.
func RankMemoriesByCosine(memories []store.Memory, queryEmbedding []float32, limit int) []MemoryHit {
	if limit <= 0 {
		limit = 20
	}

	hits := make([]MemoryHit, 0, len(memories))
	for _, m := range memories {
		if len(m.Embedding) == 0 || len(m.Embedding) != len(queryEmbedding) {
			continue
		}
		score := CosineSimilarity(m.Embedding, queryEmbedding)
		hits = append(hits, MemoryHit{Memory: m, Score: score})
	}

	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})

	if len(hits) > limit {
		hits = hits[:limit]
	}

	return hits
}
