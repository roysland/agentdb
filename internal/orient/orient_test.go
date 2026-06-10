package orient

import "testing"

func TestClassify_DeterministicTieBreak(t *testing.T) {
	cfg := Config{
		DocTypeDesign: {
			Patterns: []string{"README.md"},
			Priority: 1,
		},
		DocTypeArchitecture: {
			Patterns: []string{"README.md"},
			Priority: 1,
		},
	}

	// architecture < design lexicographically, so tie-break should be stable.
	expected := DocTypeArchitecture

	for i := 0; i < 200; i++ {
		got := Classify("README.md", cfg)
		if got.DocType != expected {
			t.Fatalf("iteration %d: Classify doc type = %q, want %q", i, got.DocType, expected)
		}
		if got.Priority != 1 {
			t.Fatalf("iteration %d: Classify priority = %d, want %d", i, got.Priority, 1)
		}
	}
}
