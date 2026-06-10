package filefilter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsCodeFile(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "go source", path: "main.go", want: true},
		{name: "markdown", path: "readme.md", want: true},
		{name: "ignored temp glob", path: "cache/session.tmp", want: false},
		{name: "ignored log glob", path: "logs/app.log", want: false},
		{name: "ignored dir", path: "node_modules/pkg/index.js", want: false},
		{name: "hidden git dir", path: ".git/config", want: false},
		{name: "build dir", path: "build/output.go", want: false},
		{name: "not extension listed", path: "notes.txt", want: false},
		{name: "does not false-match dot git substring", path: ".github/workflow.go", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCodeFile(tt.path); got != tt.want {
				t.Fatalf("IsCodeFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestShouldSkipDirName(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want bool
	}{
		{name: "git", dir: ".git", want: true},
		{name: "vendor", dir: "vendor", want: true},
		{name: "regular dir", dir: "src", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldSkipDirName(tt.dir); got != tt.want {
				t.Fatalf("ShouldSkipDirName(%q) = %v, want %v", tt.dir, got, tt.want)
			}
		})
	}
}

func TestMatcherHonorsRootGitignoreForFiles(t *testing.T) {
	root := t.TempDir()

	gitignore := "ignored.js\nassets/*.css\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	matcher := NewMatcher(root)

	if matcher.IsCodeFile(filepath.Join(root, "ignored.js")) {
		t.Fatalf("expected ignored.js to be excluded by .gitignore")
	}
	if matcher.IsCodeFile(filepath.Join(root, "assets", "bundle.css")) {
		t.Fatalf("expected assets/bundle.css to be excluded by .gitignore")
	}
	if !matcher.IsCodeFile(filepath.Join(root, "main.go")) {
		t.Fatalf("expected main.go to remain indexable")
	}
}

func TestMatcherHonorsRootGitignoreForDirectories(t *testing.T) {
	root := t.TempDir()

	gitignore := "wwwroot/\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	matcher := NewMatcher(root)
	wwwrootPath := filepath.Join(root, "wwwroot")
	if !matcher.ShouldSkipDir(wwwrootPath, "wwwroot") {
		t.Fatalf("expected wwwroot directory to be skipped by .gitignore")
	}
}

func TestMatcherHonorsNestedGitignoreForFiles(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "web", "assets"), 0o755); err != nil {
		t.Fatalf("mkdir web/assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "web", ".gitignore"), []byte("*.js\n"), 0o644); err != nil {
		t.Fatalf("write nested .gitignore: %v", err)
	}

	matcher := NewMatcher(root)

	if matcher.IsCodeFile(filepath.Join(root, "web", "bundle.js")) {
		t.Fatalf("expected nested web/bundle.js to be ignored")
	}
	if !matcher.IsCodeFile(filepath.Join(root, "api", "main.js")) {
		t.Fatalf("expected api/main.js to remain indexable")
	}
}

func TestMatcherHonorsNestedGitignoreForDirectories(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "frontend", "dist"), 0o755); err != nil {
		t.Fatalf("mkdir frontend/dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "frontend", ".gitignore"), []byte("dist/\n"), 0o644); err != nil {
		t.Fatalf("write nested .gitignore: %v", err)
	}

	matcher := NewMatcher(root)
	if !matcher.ShouldSkipDir(filepath.Join(root, "frontend", "dist"), "dist") {
		t.Fatalf("expected frontend/dist directory to be skipped by nested .gitignore")
	}
}
