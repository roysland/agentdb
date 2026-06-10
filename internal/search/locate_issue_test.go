package search

import (
	"math"
	"testing"
)

func TestComputeConfidenceScore(t *testing.T) {
	tests := []struct {
		name             string
		cosineSimilarity float64
		normalizedBM25   float64
		want             float64
	}{
		{
			name:             "both zero",
			cosineSimilarity: 0.0,
			normalizedBM25:   0.0,
			want:             0.0,
		},
		{
			name:             "both max",
			cosineSimilarity: 1.0,
			normalizedBM25:   1.0,
			want:             1.0,
		},
		{
			name:             "typical values",
			cosineSimilarity: 0.8,
			normalizedBM25:   0.6,
			want:             0.72, // 0.6*0.8 + 0.4*0.6 = 0.48 + 0.24
		},
		{
			name:             "clamps above 1.0",
			cosineSimilarity: 1.5,
			normalizedBM25:   1.5,
			want:             1.0,
		},
		{
			name:             "clamps below 0.0",
			cosineSimilarity: -1.0,
			normalizedBM25:   -1.0,
			want:             0.0,
		},
		{
			name:             "cosine only",
			cosineSimilarity: 0.9,
			normalizedBM25:   0.0,
			want:             0.54, // 0.6*0.9 + 0.4*0.0
		},
		{
			name:             "bm25 only",
			cosineSimilarity: 0.0,
			normalizedBM25:   0.9,
			want:             0.36, // 0.6*0.0 + 0.4*0.9
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeConfidenceScore(tt.cosineSimilarity, tt.normalizedBM25)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("ComputeConfidenceScore(%v, %v) = %v, want %v",
					tt.cosineSimilarity, tt.normalizedBM25, got, tt.want)
			}
		})
	}
}

func TestComputeConfidenceScoreLexicalOnly(t *testing.T) {
	tests := []struct {
		name           string
		normalizedBM25 float64
		want           float64
	}{
		{"zero", 0.0, 0.0},
		{"max", 1.0, 1.0},
		{"mid", 0.5, 0.5},
		{"clamps above", 1.5, 1.0},
		{"clamps below", -0.5, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeConfidenceScoreLexicalOnly(tt.normalizedBM25)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("ComputeConfidenceScoreLexicalOnly(%v) = %v, want %v",
					tt.normalizedBM25, got, tt.want)
			}
		})
	}
}

func TestNormalizeBM25(t *testing.T) {
	tests := []struct {
		name            string
		bm25Score       float64
		maxAbsBM25Score float64
		want            float64
	}{
		{
			name:            "typical negative score",
			bm25Score:       -5.0,
			maxAbsBM25Score: 10.0,
			want:            0.5, // -(-5.0) / 10.0
		},
		{
			name:            "best match equals max",
			bm25Score:       -10.0,
			maxAbsBM25Score: 10.0,
			want:            1.0, // -(-10.0) / 10.0
		},
		{
			name:            "zero score",
			bm25Score:       0.0,
			maxAbsBM25Score: 10.0,
			want:            0.0,
		},
		{
			name:            "max abs is zero (division by zero guard)",
			bm25Score:       -5.0,
			maxAbsBM25Score: 0.0,
			want:            0.0,
		},
		{
			name:            "weak match",
			bm25Score:       -1.0,
			maxAbsBM25Score: 10.0,
			want:            0.1, // -(-1.0) / 10.0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeBM25(tt.bm25Score, tt.maxAbsBM25Score)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("NormalizeBM25(%v, %v) = %v, want %v",
					tt.bm25Score, tt.maxAbsBM25Score, got, tt.want)
			}
		})
	}
}

func TestMaxAbsBM25Score(t *testing.T) {
	tests := []struct {
		name    string
		results []FTS5Result
		want    float64
	}{
		{
			name:    "empty results",
			results: nil,
			want:    0.0,
		},
		{
			name: "single result",
			results: []FTS5Result{
				{BM25Score: -7.5},
			},
			want: 7.5,
		},
		{
			name: "multiple results",
			results: []FTS5Result{
				{BM25Score: -3.0},
				{BM25Score: -10.0},
				{BM25Score: -1.5},
			},
			want: 10.0,
		},
		{
			name: "all zero scores",
			results: []FTS5Result{
				{BM25Score: 0.0},
				{BM25Score: 0.0},
			},
			want: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaxAbsBM25Score(tt.results)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("MaxAbsBM25Score() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClampFloat64(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		min, max float64
		want     float64
	}{
		{"within range", 0.5, 0.0, 1.0, 0.5},
		{"at min", 0.0, 0.0, 1.0, 0.0},
		{"at max", 1.0, 0.0, 1.0, 1.0},
		{"below min", -0.5, 0.0, 1.0, 0.0},
		{"above max", 1.5, 0.0, 1.0, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampFloat64(tt.value, tt.min, tt.max)
			if got != tt.want {
				t.Errorf("clampFloat64(%v, %v, %v) = %v, want %v",
					tt.value, tt.min, tt.max, got, tt.want)
			}
		})
	}
}

func TestLocateIssueConfidenceBonus(t *testing.T) {
	runtimeCandidate := &candidate{kind: "func", filePath: "internal/llm/openai.go"}
	constantCandidate := &candidate{kind: "const", filePath: "db/agents.sql.go"}

	runtimeBonus := locateIssueConfidenceBonus(runtimeCandidate)
	constantBonus := locateIssueConfidenceBonus(constantCandidate)

	if runtimeBonus <= constantBonus {
		t.Fatalf("expected runtime candidate bonus %v to exceed constant candidate bonus %v", runtimeBonus, constantBonus)
	}

	boostedRuntime := adjustLocateIssueConfidence(runtimeCandidate, 0.3)
	boostedConstant := adjustLocateIssueConfidence(constantCandidate, 0.3)

	if boostedRuntime <= boostedConstant {
		t.Fatalf("expected runtime candidate confidence %v to exceed constant candidate confidence %v", boostedRuntime, boostedConstant)
	}
}
