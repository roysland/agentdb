---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/utils'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/utils/conversions.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/utils
type: Module
---

## What it does

Provides a string-to-nullable conversion utility that treats blank strings as absent values. It returns `nil` for strings that are empty or contain only whitespace, and the original string otherwise.

## Public interface

```go
func nullIfEmpty(v string) any
```

Note: the function is unexported (lowercase), so it is only callable from within the `utils` package.

## Key invariants

- A string consisting solely of whitespace characters is treated the same as an empty string — both yield `nil`.
- A non-blank string is returned unchanged (not a copy).
- The return type is `any`, so callers must type-assert to `string` or check for `nil` when consuming the result.

## Non-obvious decisions

- **Whitespace-only strings are nulled, not just empty ones.** A developer might expect `nullIfEmpty` to only check `len(v) == 0`. The use of `strings.TrimSpace` means strings like `"  "` are also treated as absent, which is a semantic choice that depends on how callers pass input — this could be surprising if downstream code distinguishes between whitespace-only and truly empty strings.
