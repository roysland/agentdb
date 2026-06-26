package filefilter

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

var ignoredDirNames = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	".venv":        {},
	"venv":         {},
	"__pycache__":  {},
	"vendor":       {},
	"dist":         {},
	"build":        {},
	".idea":        {},
	".vscode":      {},
}

var ignoredExactFileNames = map[string]struct{}{
	".gitignore": {},
	".ds_store":  {},
	".env":       {},
}

var ignoredBasenameGlobs = []string{
	"*.log",
	"*.tmp",
}

var codeExtensions = map[string]struct{}{
	".go":   {},
	".py":   {},
	".js":   {},
	".ts":   {},
	".java": {},
	".cpp":  {},
	".c":    {},
	".h":    {},
	".rs":   {},
	".rb":   {},
	".php":  {},
	".sql":  {},
	".sh":   {},
	".md":   {},
	".json": {},
	".yaml": {},
	".yml":  {},
	".xml":  {},
	".html": {},
	".css":  {},
	".cs":   {},
}

// ShouldSkipDirName returns true when the directory name is excluded from traversal.
func ShouldSkipDirName(name string) bool {
	_, ok := ignoredDirNames[strings.ToLower(name)]
	return ok
}

var testSuffixes = []string{"_test.go", ".test.ts", ".test.js", ".spec.ts", ".spec.js"}

var nonImplExtensions = map[string]struct{}{
	".md": {}, ".markdown": {}, ".yaml": {}, ".yml": {},
	".json": {}, ".css": {}, ".html": {}, ".xml": {}, ".txt": {},
}

var nonImplDirs = []string{"okf", "docs", "doc", ".kiro", "kiro", "spec", "specs"}

// IsTestFile reports whether the file path is a test file that should be excluded
// from implementation-targeted search results.
func IsTestFile(path string) bool {
	normalized := filepath.ToSlash(path)
	base := filepath.Base(normalized)

	if slices.ContainsFunc(testSuffixes, func(s string) bool { return strings.HasSuffix(base, s) }) {
		return true
	}
	return slices.Contains(strings.Split(normalized, "/"), "__tests__")
}

// IsImplFile reports whether the file path is an implementation file, as opposed
// to docs, config, assets, or spec directories that agents would not edit to fix a bug.
func IsImplFile(path string) bool {
	normalized := filepath.ToSlash(strings.ToLower(path))

	if _, ok := nonImplExtensions[filepath.Ext(normalized)]; ok {
		return false
	}

	parts := strings.Split(normalized, "/")
	return !slices.ContainsFunc(parts, func(p string) bool {
		return slices.Contains(nonImplDirs, p)
	})
}

// ShouldIgnorePath applies canonical ignore rules to a file or directory path.
func ShouldIgnorePath(path string) bool {
	normalized := filepath.ToSlash(path)
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." {
			continue
		}
		if ShouldSkipDirName(part) {
			return true
		}
	}

	base := strings.ToLower(filepath.Base(path))
	if _, ok := ignoredExactFileNames[base]; ok {
		return true
	}

	for _, pattern := range ignoredBasenameGlobs {
		matched, err := filepath.Match(pattern, base)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// IsCodeFile reports whether the file path should be treated as source content.
func IsCodeFile(path string) bool {
	if ShouldIgnorePath(path) {
		return false
	}

	ext := strings.ToLower(filepath.Ext(path))
	_, ok := codeExtensions[ext]
	return ok
}

// IsConfinedRegularFile reports whether path resolves to a regular file that
// stays within rootPath. Symlinks are only allowed when they resolve inside
// rootPath and target a regular file.
func IsConfinedRegularFile(rootPath, path string, info os.FileInfo) bool {
	if strings.TrimSpace(rootPath) == "" || strings.TrimSpace(path) == "" {
		return false
	}

	if info == nil {
		var err error
		info, err = os.Lstat(path)
		if err != nil {
			return false
		}
	}

	if info.IsDir() {
		return false
	}

	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return false
	}

	absTarget, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	if info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return false
		}
		targetInfo, err := os.Stat(resolved)
		if err != nil || !targetInfo.Mode().IsRegular() {
			return false
		}
		absTarget, err = filepath.Abs(resolved)
		if err != nil {
			return false
		}
	} else if !info.Mode().IsRegular() {
		return false
	}

	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return false
	}

	return true
}

// Matcher applies built-in ignore rules plus optional .gitignore rules scoped to a root path.
type Matcher struct {
	rootPath string
	git      map[string]*ignore.GitIgnore
}

// NewMatcher creates a path matcher rooted at rootPath.
// If no .gitignore exists at the root, built-in rules are still applied.
func NewMatcher(rootPath string) *Matcher {
	m := &Matcher{rootPath: rootPath, git: loadGitignoreMap(rootPath)}
	return m
}

// ShouldSkipDir reports whether a directory should be skipped during traversal.
func (m *Matcher) ShouldSkipDir(path, dirName string) bool {
	if ShouldSkipDirName(dirName) {
		return true
	}
	if m == nil || m.git == nil {
		return false
	}
	return m.isGitIgnored(path, true)
}

// IsCodeFile reports whether a file should be treated as source content.
func (m *Matcher) IsCodeFile(path string) bool {
	if !IsCodeFile(path) {
		return false
	}
	if m == nil || m.git == nil {
		return true
	}
	return !m.isGitIgnored(path, false)
}

func (m *Matcher) relPath(path string) (string, bool) {
	rel, err := filepath.Rel(m.rootPath, path)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

func (m *Matcher) isGitIgnored(path string, isDir bool) bool {
	rel, ok := m.relPath(path)
	if !ok {
		return false
	}

	ignored := false
	for _, scope := range ancestorScopes(rel, isDir) {
		gi, ok := m.git[scope]
		if !ok || gi == nil {
			continue
		}
		target := rel
		if scope != "" {
			target = strings.TrimPrefix(rel, scope+"/")
		}
		if target == "" || target == "." {
			continue
		}
		if isDir {
			if gi.MatchesPath(target) || gi.MatchesPath(target+"/") {
				ignored = true
			}
			continue
		}
		if gi.MatchesPath(target) {
			ignored = true
		}
	}

	return ignored
}

func ancestorScopes(rel string, isDir bool) []string {
	parts := strings.Split(rel, "/")
	end := len(parts) - 1
	if isDir {
		end = len(parts)
	}
	if end < 0 {
		end = 0
	}

	out := []string{""}
	for i := 1; i <= end; i++ {
		scope := strings.Join(parts[:i], "/")
		if scope != "" {
			out = append(out, scope)
		}
	}
	return out
}

func loadGitignoreMap(rootPath string) map[string]*ignore.GitIgnore {
	out := make(map[string]*ignore.GitIgnore)
	entries := make([]string, 0)

	_ = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if ShouldSkipDirName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != ".gitignore" {
			return nil
		}
		relDir, relErr := filepath.Rel(rootPath, filepath.Dir(path))
		if relErr != nil {
			return nil
		}
		relDir = filepath.ToSlash(relDir)
		if relDir == "." {
			relDir = ""
		}
		entries = append(entries, relDir+"\x00"+path)
		return nil
	})

	sort.Strings(entries)
	for _, entry := range entries {
		parts := strings.SplitN(entry, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		scope := parts[0]
		path := parts[1]
		gi, err := ignore.CompileIgnoreFile(path)
		if err == nil {
			out[scope] = gi
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}
