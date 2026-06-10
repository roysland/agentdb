package orient

import (
	"testing"
)

func TestBuildQuery_DefaultConfig(t *testing.T) {
	config := DefaultConfig()
	codebaseIDs := []int64{1, 2}

	whereClause, args := buildQuery(config, codebaseIDs)

	if whereClause == "" {
		t.Fatal("expected non-empty WHERE clause")
	}

	// Should have codebase IDs as first args
	if len(args) < 2 {
		t.Fatalf("expected at least 2 args, got %d", len(args))
	}
	if args[0].(int64) != 1 || args[1].(int64) != 2 {
		t.Errorf("expected first args to be codebase IDs, got %v", args[:2])
	}

	// Should contain codebase_id IN clause
	if !contains(whereClause, "c.codebase_id IN") {
		t.Error("expected WHERE clause to contain codebase_id IN")
	}

	// Should contain LIKE conditions
	if !contains(whereClause, "c.file_path LIKE") {
		t.Error("expected WHERE clause to contain file_path LIKE conditions")
	}

	// LIKE patterns are escaped by globToSQL, so query must declare ESCAPE.
	if !contains(whereClause, "ESCAPE '\\'") {
		t.Error("expected WHERE clause to include ESCAPE '\\' for LIKE patterns")
	}
}

func TestBuildQuery_EmptyCodebaseIDs(t *testing.T) {
	config := DefaultConfig()
	whereClause, args := buildQuery(config, nil)

	if whereClause != "" {
		t.Errorf("expected empty WHERE clause for nil codebase IDs, got %q", whereClause)
	}
	if args != nil {
		t.Errorf("expected nil args, got %v", args)
	}
}

func TestBuildQuery_EmptyConfig(t *testing.T) {
	config := Config{}
	whereClause, args := buildQuery(config, []int64{1})

	if whereClause != "" {
		t.Errorf("expected empty WHERE clause for empty config, got %q", whereClause)
	}
	if args != nil {
		t.Errorf("expected nil args, got %v", args)
	}
}

func TestGlobToSQL(t *testing.T) {
	tests := []struct {
		glob     string
		expected string
	}{
		{"README*", "%README%"},
		{"*.md", "%%.md"},
		{"CLAUDE.md", "%CLAUDE.md"},
		{"design*", "%design%"},
	}

	for _, tt := range tests {
		t.Run(tt.glob, func(t *testing.T) {
			result := globToSQL(tt.glob)
			if result != tt.expected {
				t.Errorf("globToSQL(%q) = %q, want %q", tt.glob, result, tt.expected)
			}
		})
	}
}

func TestEnforceMaxItems(t *testing.T) {
	config := Config{
		DocTypeGeneral: {
			Patterns: []string{"*.md"},
			Priority: 8,
			MaxItems: 2,
		},
		DocTypeReadme: {
			Patterns: []string{"README*"},
			Priority: 1,
			MaxItems: 0, // unlimited
		},
	}

	docs := []OrientationDoc{
		{FilePath: "README.md", DocType: DocTypeReadme, Priority: 1},
		{FilePath: "doc1.md", DocType: DocTypeGeneral, Priority: 8},
		{FilePath: "doc2.md", DocType: DocTypeGeneral, Priority: 8},
		{FilePath: "doc3.md", DocType: DocTypeGeneral, Priority: 8},
		{FilePath: "doc4.md", DocType: DocTypeGeneral, Priority: 8},
	}

	result := enforceMaxItems(docs, config)

	// Should have README (unlimited) + 2 general docs (capped at 2)
	if len(result) != 3 {
		t.Errorf("expected 3 docs after MaxItems enforcement, got %d", len(result))
	}

	// Verify the README is still there
	if result[0].FilePath != "README.md" {
		t.Errorf("expected first doc to be README.md, got %s", result[0].FilePath)
	}

	// Count general docs
	generalCount := 0
	for _, doc := range result {
		if doc.DocType == DocTypeGeneral {
			generalCount++
		}
	}
	if generalCount != 2 {
		t.Errorf("expected 2 general docs after cap, got %d", generalCount)
	}
}

func TestEnforceMaxItems_NoCapWhenZero(t *testing.T) {
	config := Config{
		DocTypeReadme: {
			Patterns: []string{"README*"},
			Priority: 1,
			MaxItems: 0, // unlimited
		},
	}

	docs := []OrientationDoc{
		{FilePath: "README.md", DocType: DocTypeReadme, Priority: 1},
		{FilePath: "sub/README.md", DocType: DocTypeReadme, Priority: 11},
		{FilePath: "other/README.txt", DocType: DocTypeReadme, Priority: 11},
	}

	result := enforceMaxItems(docs, config)
	if len(result) != 3 {
		t.Errorf("expected all 3 docs (no cap), got %d", len(result))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
