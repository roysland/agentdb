---
commit: 99a4eb8721ee6257678f63653cf97d856a65aa60
description: 'Codebase knowledge for module: internal/filefilter'
files:
- internal/filefilter/filter.go
- internal/filefilter/filter_test.go
tags:
- module
timestamp: '2026-06-28'
title: internal/filefilter
type: Module
---

# `internal/filefilter`

## What it does  
The `filefilter` module provides utilities to determine whether files and directories should be included or excluded during code traversal and indexing. It enforces built-in ignore rules (e.g., `.git`, `node_modules`), supports `.gitignore`-style patterns scoped to directories, and distinguishes between implementation, test, and non-implementation files (e.g., docs, configs).

## Public interface  
```go
func ShouldSkipDirName(name string) bool  
func IsTestFile(path string) bool  
func IsImplFile(path string) bool  
func ShouldIgnorePath(path string) bool  
func IsCodeFile(path string) bool  
func IsConfinedRegularFile(rootPath, path string, info os.FileInfo) bool  

type Matcher struct {  
    // ...  
}  
func NewMatcher(rootPath string) *Matcher  
func (m *Matcher) ShouldSkipDir(path, dirName string) bool  
func (m *Matcher) IsCodeFile(path string) bool  
```

## Key invariants  
- `IsCodeFile(path)` returns `false` if `ShouldIgnorePath(path)` is `true`.  
- `Matcher.IsCodeFile(path)` returns `false` if either `IsCodeFile(path)` is `false` *or* the path matches any applicable `.gitignore` rule.  
- `IsConfinedRegularFile` ensures the resolved path (after symlink resolution, if applicable) is strictly within `rootPath` and is a regular file.  
- `ShouldSkipDirName` is case-insensitive (normalizes via `strings.ToLower`).  
- `IsImplFile` excludes files with known non-implementation extensions (e.g., `.md`, `.yaml`) and files in known non-implementation directories (e.g., `docs`, `spec`).  

## Non-obvious decisions  
- **Ancestor-scoped `.gitignore` lookup (`ancestorScopes`)**: When checking if a file/directory is ignored, the module evaluates `.gitignore` files from the root down to the current path’s parent directories, not just the nearest one. This matches Git’s behavior where nested `.gitignore` files inherit and extend parent rules, but the implementation explicitly aggregates *all* ancestor scopes (including root) and applies them in order. A developer might expect only the nearest `.gitignore` to apply, but Git semantics require cumulative scoping.  
- **`relPath` returns `false` for paths outside `rootPath`**: The helper `relPath` rejects paths that resolve to `..` or absolute paths after `filepath.Rel`, ensuring `Matcher` operations are strictly bounded to the root. This prevents accidental traversal or matching of files outside the intended scope (e.g., via symlinks pointing out of bounds).  
- **`IsImplFile` excludes `.yaml`, `.yml`, `.json`, etc., unconditionally**: While some config files (e.g., `go.mod`) *are* implementation-critical, the module treats *all* such extensions as non-implementation to avoid agents modifying configs when fixing bugs. This is a conservative design choice—better to exclude edge cases than risk unintended edits.  

## Unclear intent  
None. All functions, parameters, and naming conventions are clear and consistent with their documented purposes. The use of `filepath.ToSlash` and `strings.ToLower` for cross-platform normalization is idiomatic and unambiguous.
