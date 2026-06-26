---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/filefilter'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/filefilter/filter.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/filefilter/filter_test.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/filefilter
type: Module
---

## What it does

Provides path filtering for file traversal, combining a built-in blocklist of common directories and file patterns with optional `.gitignore` rules scoped to a project root. It determines which directories to skip, which files count as source content, and whether a path resolves to a regular file confined within a given root.

## Public interface

- `func ShouldSkipDirName(name string) bool` — returns true for well-known excluded directory names (`.git`, `node_modules`, `vendor`, etc.).
- `func ShouldIgnorePath(path string) bool` — applies canonical ignore rules (directory names, exact file names, basename globs) to a path.
- `func IsCodeFile(path string) bool` — reports whether a path should be treated as source content based on extension and ignore rules.
- `func IsConfinedRegularFile(rootPath, path string, info os.FileInfo) bool` — checks that `path` resolves to a regular file inside `rootPath`, allowing symlinks only when they stay within the root.
- `type Matcher struct` — applies built-in rules plus `.gitignore` rules scoped to a root path.
- `func NewMatcher(rootPath string) *Matcher` — creates a matcher rooted at `rootPath`, loading all `.gitignore` files found during traversal.
- `func (m *Matcher) ShouldSkipDir(path, dirName string) bool` — reports whether a directory should be skipped during traversal.
- `func (m *Matcher) IsCodeFile(path string) bool` — reports whether a file is source content after applying both built-in and gitignore rules.

## Key invariants

- Directory-name checks are case-insensitive; file-extension checks are case-insensitive.
- Symlinks are only accepted when they resolve to a regular file whose absolute path remains inside the resolved absolute root.
- `.gitignore` rules are scoped by directory: a nested `.gitignore` only affects paths beneath its own directory, and ancestor scopes are checked in order from root downward.
- `loadGitignoreMap` skips traversal into directories matched by `ShouldSkipDirName`, so `.gitignore` files inside excluded directories (e.g., `node_modules`) are never loaded.

## Non-obvious decisions

- `ancestorScopes` walks every ancestor directory of the target path and checks each scope's `.gitignore` for a match, rather than only the nearest one. This means a pattern in a parent `.gitignore` can still exclude a file even when a closer `.gitignore` exists, matching git's actual behavior where multiple `.gitignore` files can apply to the same path.
- `IsConfinedRegularFile` uses `os.Lstat` when `info` is nil but `os.Stat` after symlink resolution, deliberately distinguishing symlink metadata from target metadata to enforce the "regular file" requirement on the final target rather than the link itself.
