package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestHashFile(t *testing.T) {
	// Create a temp file with known content
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	content := []byte("package main\n\nfunc main() {}\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile() error = %v", err)
	}

	// Compute expected hash
	h := sha256.Sum256(content)
	want := hex.EncodeToString(h[:])

	if got != want {
		t.Errorf("HashFile() = %q, want %q", got, want)
	}
}

func TestHashFile_NonExistent(t *testing.T) {
	_, err := HashFile("/nonexistent/path/file.go")
	if err == nil {
		t.Error("HashFile() expected error for non-existent file, got nil")
	}
}

func TestHashFile_DifferentContent(t *testing.T) {
	dir := t.TempDir()

	path1 := filepath.Join(dir, "a.go")
	path2 := filepath.Join(dir, "b.go")

	os.WriteFile(path1, []byte("content A"), 0644)
	os.WriteFile(path2, []byte("content B"), 0644)

	hash1, err := HashFile(path1)
	if err != nil {
		t.Fatal(err)
	}
	hash2, err := HashFile(path2)
	if err != nil {
		t.Fatal(err)
	}

	if hash1 == hash2 {
		t.Error("HashFile() should produce different hashes for different content")
	}
}

func TestHashFile_SameContent(t *testing.T) {
	dir := t.TempDir()

	path1 := filepath.Join(dir, "a.go")
	path2 := filepath.Join(dir, "b.go")

	content := []byte("same content")
	os.WriteFile(path1, content, 0644)
	os.WriteFile(path2, content, 0644)

	hash1, err := HashFile(path1)
	if err != nil {
		t.Fatal(err)
	}
	hash2, err := HashFile(path2)
	if err != nil {
		t.Fatal(err)
	}

	if hash1 != hash2 {
		t.Error("HashFile() should produce same hash for same content")
	}
}

func TestComputeDelta_AllAdded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, dir, "util.go", "package main\nfunc util() {}")

	result, err := ComputeDelta(context.Background(), 1, dir, map[string]string{})
	if err != nil {
		t.Fatalf("ComputeDelta() error = %v", err)
	}

	sort.Strings(result.Added)
	if len(result.Added) != 2 {
		t.Fatalf("expected 2 added files, got %d: %v", len(result.Added), result.Added)
	}
	if result.Added[0] != "main.go" || result.Added[1] != "util.go" {
		t.Errorf("Added = %v, want [main.go util.go]", result.Added)
	}
	if len(result.Changed) != 0 {
		t.Errorf("Changed = %v, want empty", result.Changed)
	}
	if len(result.Removed) != 0 {
		t.Errorf("Removed = %v, want empty", result.Removed)
	}
	if len(result.Unchanged) != 0 {
		t.Errorf("Unchanged = %v, want empty", result.Unchanged)
	}
}

func TestComputeDelta_AllUnchanged(t *testing.T) {
	dir := t.TempDir()
	content := "package main\nfunc main() {}"
	writeFile(t, dir, "main.go", content)

	hash := hashString(content)
	storedHashes := map[string]string{
		"main.go": hash,
	}

	result, err := ComputeDelta(context.Background(), 1, dir, storedHashes)
	if err != nil {
		t.Fatalf("ComputeDelta() error = %v", err)
	}

	if len(result.Unchanged) != 1 || result.Unchanged[0] != "main.go" {
		t.Errorf("Unchanged = %v, want [main.go]", result.Unchanged)
	}
	if len(result.Changed) != 0 {
		t.Errorf("Changed = %v, want empty", result.Changed)
	}
	if len(result.Added) != 0 {
		t.Errorf("Added = %v, want empty", result.Added)
	}
	if len(result.Removed) != 0 {
		t.Errorf("Removed = %v, want empty", result.Removed)
	}
}

func TestComputeDelta_Changed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\nfunc main() { /* updated */ }")

	storedHashes := map[string]string{
		"main.go": hashString("package main\nfunc main() {}"),
	}

	result, err := ComputeDelta(context.Background(), 1, dir, storedHashes)
	if err != nil {
		t.Fatalf("ComputeDelta() error = %v", err)
	}

	if len(result.Changed) != 1 || result.Changed[0] != "main.go" {
		t.Errorf("Changed = %v, want [main.go]", result.Changed)
	}
	if len(result.Unchanged) != 0 {
		t.Errorf("Unchanged = %v, want empty", result.Unchanged)
	}
}

func TestComputeDelta_Removed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")

	storedHashes := map[string]string{
		"main.go":    hashString("package main"),
		"deleted.go": hashString("package deleted"),
	}

	result, err := ComputeDelta(context.Background(), 1, dir, storedHashes)
	if err != nil {
		t.Fatalf("ComputeDelta() error = %v", err)
	}

	if len(result.Removed) != 1 || result.Removed[0] != "deleted.go" {
		t.Errorf("Removed = %v, want [deleted.go]", result.Removed)
	}
	if len(result.Unchanged) != 1 || result.Unchanged[0] != "main.go" {
		t.Errorf("Unchanged = %v, want [main.go]", result.Unchanged)
	}
}

func TestComputeDelta_MixedCategories(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "unchanged.go", "package pkg")
	writeFile(t, dir, "changed.go", "package pkg\n// modified")
	writeFile(t, dir, "added.go", "package pkg\nfunc new() {}")

	storedHashes := map[string]string{
		"unchanged.go": hashString("package pkg"),
		"changed.go":   hashString("package pkg\n// original"),
		"removed.go":   hashString("package removed"),
	}

	result, err := ComputeDelta(context.Background(), 1, dir, storedHashes)
	if err != nil {
		t.Fatalf("ComputeDelta() error = %v", err)
	}

	if len(result.Unchanged) != 1 || result.Unchanged[0] != "unchanged.go" {
		t.Errorf("Unchanged = %v, want [unchanged.go]", result.Unchanged)
	}
	if len(result.Changed) != 1 || result.Changed[0] != "changed.go" {
		t.Errorf("Changed = %v, want [changed.go]", result.Changed)
	}
	if len(result.Added) != 1 || result.Added[0] != "added.go" {
		t.Errorf("Added = %v, want [added.go]", result.Added)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "removed.go" {
		t.Errorf("Removed = %v, want [removed.go]", result.Removed)
	}
}

func TestComputeDelta_SkipsNonCodeFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, dir, "readme.txt", "not a code file")
	writeFile(t, dir, "image.png", "binary data")

	result, err := ComputeDelta(context.Background(), 1, dir, map[string]string{})
	if err != nil {
		t.Fatalf("ComputeDelta() error = %v", err)
	}

	if len(result.Added) != 1 || result.Added[0] != "main.go" {
		t.Errorf("Added = %v, want [main.go]", result.Added)
	}
}

func TestComputeDelta_Subdirectories(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "pkg", "sub"), 0755)
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, filepath.Join(dir, "pkg"), "util.go", "package pkg")
	writeFile(t, filepath.Join(dir, "pkg", "sub"), "deep.go", "package sub")

	result, err := ComputeDelta(context.Background(), 1, dir, map[string]string{})
	if err != nil {
		t.Fatalf("ComputeDelta() error = %v", err)
	}

	sort.Strings(result.Added)
	expected := []string{"main.go", "pkg/sub/deep.go", "pkg/util.go"}
	if len(result.Added) != 3 {
		t.Fatalf("expected 3 added files, got %d: %v", len(result.Added), result.Added)
	}
	for i, want := range expected {
		if result.Added[i] != want {
			t.Errorf("Added[%d] = %q, want %q", i, result.Added[i], want)
		}
	}
}

func TestComputeDelta_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := ComputeDelta(ctx, 1, dir, map[string]string{})
	if err == nil {
		t.Error("ComputeDelta() expected error for cancelled context, got nil")
	}
}

func TestComputeDelta_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	storedHashes := map[string]string{
		"old.go": hashString("package old"),
	}

	result, err := ComputeDelta(context.Background(), 1, dir, storedHashes)
	if err != nil {
		t.Fatalf("ComputeDelta() error = %v", err)
	}

	if len(result.Removed) != 1 || result.Removed[0] != "old.go" {
		t.Errorf("Removed = %v, want [old.go]", result.Removed)
	}
	if len(result.Added) != 0 {
		t.Errorf("Added = %v, want empty", result.Added)
	}
}

func TestComputeDelta_EmptyStoredHashes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")

	result, err := ComputeDelta(context.Background(), 1, dir, nil)
	if err != nil {
		t.Fatalf("ComputeDelta() error = %v", err)
	}

	if len(result.Added) != 1 || result.Added[0] != "main.go" {
		t.Errorf("Added = %v, want [main.go]", result.Added)
	}
}

func TestIsLegacyHash(t *testing.T) {
	tests := []struct {
		name string
		hash string
		want bool
	}{
		{"MD5 hash (32 chars)", "d41d8cd98f00b204e9800998ecf8427e", true},
		{"SHA-256 hash (64 chars)", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", false},
		{"Short hex string", "abcdef1234", true},
		{"Empty string", "", false},
		{"Non-hex characters", "xyz123notahash!", false},
		{"Exactly 64 hex chars", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"63 hex chars (just under)", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{"Longer than 64 hex chars", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa00", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsLegacyHash(tt.hash)
			if got != tt.want {
				t.Errorf("IsLegacyHash(%q) = %v, want %v", tt.hash, got, tt.want)
			}
		})
	}
}

func TestComputeDelta_LegacyHashTreatedAsChanged(t *testing.T) {
	dir := t.TempDir()
	content := "package main\nfunc main() {}"
	writeFile(t, dir, "main.go", content)

	// Use a legacy MD5 hash (32 chars) — even if content hasn't changed,
	// the file should be classified as "changed" to force re-indexing
	storedHashes := map[string]string{
		"main.go": "d41d8cd98f00b204e9800998ecf8427e", // MD5 hash
	}

	result, err := ComputeDelta(context.Background(), 1, dir, storedHashes)
	if err != nil {
		t.Fatalf("ComputeDelta() error = %v", err)
	}

	if len(result.Changed) != 1 || result.Changed[0] != "main.go" {
		t.Errorf("Changed = %v, want [main.go] (legacy hash should force re-index)", result.Changed)
	}
	if len(result.Unchanged) != 0 {
		t.Errorf("Unchanged = %v, want empty (legacy hash should not be treated as unchanged)", result.Unchanged)
	}
}

func TestComputeDelta_SHA256HashUnchanged(t *testing.T) {
	dir := t.TempDir()
	content := "package main\nfunc main() {}"
	writeFile(t, dir, "main.go", content)

	// Use a proper SHA-256 hash — file should be unchanged
	storedHashes := map[string]string{
		"main.go": hashString(content),
	}

	result, err := ComputeDelta(context.Background(), 1, dir, storedHashes)
	if err != nil {
		t.Fatalf("ComputeDelta() error = %v", err)
	}

	if len(result.Unchanged) != 1 || result.Unchanged[0] != "main.go" {
		t.Errorf("Unchanged = %v, want [main.go]", result.Unchanged)
	}
	if len(result.Changed) != 0 {
		t.Errorf("Changed = %v, want empty", result.Changed)
	}
}

// --- helpers ---

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
