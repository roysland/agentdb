package parse

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/roysland/agentdb/internal/filefilter"
)

// ParseDirectory walks rootPath and parses each file using the first matching parser.
// Files with no matching parser are skipped.
func ParseDirectory(rootPath string, parsers []Parser) ([]FileResult, error) {
	var results []FileResult
	matcher := filefilter.NewMatcher(rootPath)

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if matcher.ShouldSkipDir(path, info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if !filefilter.IsConfinedRegularFile(rootPath, path, info) {
			return nil
		}

		if !matcher.IsCodeFile(path) {
			return nil
		}

		p := findParser(path, parsers)
		if p == nil {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", path, readErr)
		}

		relPath, relErr := filepath.Rel(rootPath, path)
		if relErr != nil {
			relPath = path
		}

		result, parseErr := p.Parse(relPath, content)
		if parseErr != nil {
			// Log but don't abort — partial results are valuable
			_, _ = fmt.Fprintf(os.Stderr, "warning: parse %s: %v\n", relPath, parseErr)
			return nil
		}
		results = append(results, result)
		return nil
	})

	return results, err
}

func findParser(filePath string, parsers []Parser) Parser {
	for _, p := range parsers {
		if p.CanParse(filePath) {
			return p
		}
	}
	return nil
}
