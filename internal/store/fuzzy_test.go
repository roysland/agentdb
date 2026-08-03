package store

import "testing"

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"symbol", "symbol", 0},
		{"symbol", "sumbol", 1},
		{"symbol", "symbols", 1},
		{"symbol", "symbl", 1},
		{"gopher", "gophér", 1},
		{"FindByName", "FindByNameFuzzy", 5},
	}

	for _, tt := range tests {
		got := levenshtein(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("levenshtein(%q, %q) = %d; want %d", tt.a, tt.b, got, tt.expected)
		}
	}
}

func TestFuzzyThreshold(t *testing.T) {
	tests := []struct {
		name     string
		expected int
	}{
		{"a", 1},
		{"abc", 1},
		{"test", 1},
		{"symbolrepo", 2},
		{"verylongsymbolnamehere", 3},
	}

	for _, tt := range tests {
		got := fuzzyThreshold(tt.name)
		if got != tt.expected {
			t.Errorf("fuzzyThreshold(%q) = %d; want %d", tt.name, got, tt.expected)
		}
	}
}
