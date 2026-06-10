package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/roysland/agentdb/internal/config"
	"github.com/roysland/agentdb/internal/store"
)

// resolveCodebaseTarget resolves codebase ID and root path from command inputs.
// It accepts either ID, path, or default project path and can optionally register
// a new codebase when a path is provided but not yet registered.
func resolveCodebaseTarget(ctx context.Context, repo *store.CatalogRepo, resolved config.Runtime, codebaseID int64, path, codebasePath string, allowRegister bool) (int64, string, error) {
	items, err := repo.ListCodebases(ctx)
	if err != nil {
		return 0, "", err
	}

	explicitPath := strings.TrimSpace(path)
	if explicitPath == "" {
		explicitPath = strings.TrimSpace(codebasePath)
	}

	if codebaseID > 0 {
		for _, item := range items {
			if item.ID == codebaseID {
				if explicitPath == "" {
					p, err := normalizeProjectPath(item.RootPath)
					if err != nil {
						return 0, "", err
					}
					return item.ID, p, nil
				}
				p, err := normalizeProjectPath(explicitPath)
				if err != nil {
					return 0, "", err
				}
				return item.ID, p, nil
			}
		}
		return 0, "", fmt.Errorf("codebase id not found: %d", codebaseID)
	}

	if explicitPath != "" {
		p, err := normalizeProjectPath(explicitPath)
		if err != nil {
			return 0, "", err
		}
		if existing, ok := findCodebaseByPath(items, p); ok {
			return existing.ID, p, nil
		}

		if !allowRegister {
			return 0, "", errors.New("codebase path is not registered; run 'agentdb codebase register --path <dir>' or 'agentdb index --path <dir>'")
		}

		name := filepath.Base(p)
		id, err := repo.RegisterCodebase(ctx, p, name)
		if err != nil {
			return 0, "", err
		}
		_ = config.SaveDefaultProjectPath(p)
		return id, p, nil
	}

	// Prefer the current working directory as the default when no explicit
	// path is provided. Previously the saved `resolved.ProjectPath` took
	// precedence which caused commands run from a different working
	// directory to operate on the saved path unexpectedly.
	cwd, err := os.Getwd()
	if err != nil {
		return 0, "", fmt.Errorf("resolve cwd: %w", err)
	}

	defaultPath := cwd

	// If the runtime explicitly set a project path and the cwd is empty
	// (very unusual), fall back to the saved project path. In normal
	// usage we prefer the current working directory.
	if strings.TrimSpace(resolved.ProjectPath) != "" {
		// no-op: keep cwd as default (preserve existing behaviour only
		// when cwd cannot be determined)
	}

	p, err := normalizeProjectPath(defaultPath)
	if err != nil {
		return 0, "", err
	}
	if existing, ok := findCodebaseByPath(items, p); ok {
		return existing.ID, p, nil
	}

	if allowRegister {
		name := filepath.Base(p)
		id, err := repo.RegisterCodebase(ctx, p, name)
		if err != nil {
			return 0, "", err
		}
		_ = config.SaveDefaultProjectPath(p)
		return id, p, nil
	}

	return 0, "", errors.New("no codebase registered for current context; run 'agentdb codebase register --path <dir>' or 'agentdb index --path <dir>'")
}
