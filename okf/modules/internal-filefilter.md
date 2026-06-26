---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/filefilter'
files:
- internal/filefilter/filter.go
- internal/filefilter/filter_test.go
tags:
- module
timestamp: '2026-06-26'
title: internal/filefilter
type: Module
---

## What it does

The `filefilter` module provides utilities for deciding which files and directories should be included or excluded during source tree traversal. It combines a hardcoded set of ignored directory names, file name patterns, and extensions with optional `.gitignore` support through a `Matcher` type that scopes gitignore rules to a given root path.

## Public interface

```go
func ShouldSkipDirName(name string) bool
func ShouldIgnorePath(path string) bool
func IsCodeFile(path string) bool
func IsConfinedRegularFile(rootPath, path string, info os.FileInfo) bool

type Matcher struct {
    func NewMatcher(rootPath string) *Matcher
    func (m *Matcher) ShouldSkipDir(path, dirName string) bool
    func (m *Matcher) IsCodeFile(path string) bool
}
```

## Key invariants

- `ShouldSkipDirName` uses case-insensitive comparison against a fixed set of directory names (`.git`, `node_modules`, `.venv`, `venv`, `__pycache__`, `vendor`, `dist`, `build`, `.idea`, `.vscode`).
- `ShouldIgnorePath` checks every path segment against `ShouldSkipDirName`, so a file inside any ignored directory is itself ignored.
- `IsCodeFile` returns `false` if `ShouldIgnorePath` returns `true`; extension matching is case-insensitive.
- `IsConfinedRegularFile` rejects any path whose resolved target falls outside `rootPath`. Symlinks are only accepted when they resolve to a regular file strictly inside the root.
- `Matcher` scopes each `.gitignore` file to its own directory subtree. A `.gitignore` at `web/.gitignore` only affects paths under `web/`.
- `loadGitignoreMap` walks the root using `filepath.WalkDir` and skips the same hardcoded ignored directories, so `.gitignore` files inside `node_modules` or `.git` are never loaded.
- `ancestorScopes` returns the empty scope `""` (root gitignore) plus each ancestor directory up to (and including, for directories) the target, so a `MatchesPath` check is performed at every relevant scope.

## Non-obvious decisions

- **`loadGitignoreMap` collects entries into a slice and sorts before compiling**: The walk visits directories in arbitrary order depending on the OS. Sorting ensures deterministic behavior when multiple `.gitignore` files exist, though the current code does not actually depend on insertion order since each scope maps to exactly one compiled ignore file. The sort appears to be defensive or carry-over from an earlier design.
- **`isGitIgnored` iterates all ancestor scopes and does not short-circuit on first match**: A file can be ignored by a parent `.gitignore` even if a closer one doesn't mention it, and vice versa. This is correct gitignore semantics, but the loop sets `ignored = true` monotonically — once ignored, a later scope cannot un-ignore it. This matches how git itself behaves (a match at any level wins).
- **`IsConfinedRegularFile` calls `filepath.EvalSymlinks` but `IsCodeFile` does not**: The `Matcher` methods operate on logical paths and do not resolve symlinks. Only `IsConfinedRegularFile` performs symlink resolution, meaning a symlink to a code file inside the root will pass `IsCodeFile` but its confinement must be separately verified.

## Unclear intent

- **The `go-gitignore` library (`github.com/sabhiram/go-gitignore`)**: This is an external dependency, not one of the listed internal modules. Its `MatchesPath` semantics for directories (whether trailing `/` matters, how negation patterns interact with directory-only rules) are not fully visible from this code alone. The `isGitIgnored` method passes both `target` and `target+"/"` for directory checks, suggesting uncertainty about which form the library requires — this dual-call pattern may be redundant or may be necessary depending on the library's internal behavior.
