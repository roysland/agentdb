package parse

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifest_Valid(t *testing.T) {
	dir := t.TempDir()
	content := `{"name":"csharp-parser","version":"1.0.0","languages":["csharp"],"binary":"./bin/csharp-parser"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "csharp-parser" {
		t.Errorf("expected name csharp-parser, got %s", m.Name)
	}
	if m.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", m.Version)
	}
	if len(m.Languages) != 1 || m.Languages[0] != "csharp" {
		t.Errorf("expected languages [csharp], got %v", m.Languages)
	}
	if m.Binary != "./bin/csharp-parser" {
		t.Errorf("expected binary ./bin/csharp-parser, got %s", m.Binary)
	}
}

func TestLoadManifest_MultipleLanguages(t *testing.T) {
	dir := t.TempDir()
	content := `{"name":"jvm-parser","version":"2.0.0","languages":["java","kotlin"],"binary":"./jvm-parser"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Languages) != 2 {
		t.Errorf("expected 2 languages, got %d", len(m.Languages))
	}
}

func TestLoadManifest_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("expected error for missing manifest.json")
	}
}

func TestLoadManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{not json}"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadManifest_EmptyName(t *testing.T) {
	dir := t.TempDir()
	content := `{"name":"","version":"1.0.0","languages":["go"],"binary":"./parser"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestLoadManifest_EmptyVersion(t *testing.T) {
	dir := t.TempDir()
	content := `{"name":"parser","version":"","languages":["go"],"binary":"./parser"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("expected error for empty version")
	}
}

func TestLoadManifest_EmptyLanguages(t *testing.T) {
	dir := t.TempDir()
	content := `{"name":"parser","version":"1.0.0","languages":[],"binary":"./parser"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("expected error for empty languages")
	}
}

func TestLoadManifest_EmptyBinary(t *testing.T) {
	dir := t.TempDir()
	content := `{"name":"parser","version":"1.0.0","languages":["go"],"binary":""}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("expected error for empty binary")
	}
}

func TestLoadManifest_MissingFields(t *testing.T) {
	dir := t.TempDir()
	// JSON with only name field - missing version, languages, binary
	content := `{"name":"parser"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("expected error for missing required fields")
	}
}
