package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/roysland/agentdb/internal/store"
)

func normalizeProjectPath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", errors.New("path is required")
	}
	path = os.ExpandEnv(path)
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				path = home
			} else {
				path = filepath.Join(home, path[2:])
			}
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	clean := filepath.Clean(absPath)

	info, err := os.Stat(clean)
	if err != nil {
		return "", fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path must be a directory: %s", clean)
	}

	return clean, nil
}

func findCodebaseByPath(items []store.Codebase, targetPath string) (store.Codebase, bool) {
	target, err := normalizeProjectPath(targetPath)
	if err != nil {
		return store.Codebase{}, false
	}

	for _, item := range items {
		normalized, err := normalizeProjectPath(item.RootPath)
		if err != nil {
			continue
		}
		if normalized == target {
			return item, true
		}
	}
	return store.Codebase{}, false
}
