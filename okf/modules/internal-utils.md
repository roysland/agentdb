---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/utils'
files:
- internal/utils/conversions.go
tags:
- module
timestamp: '2026-06-26'
title: internal/utils
type: Module
---

## What it does

Provides string conversion helpers for the utils package. Currently contains a single utility that normalizes empty or whitespace-only strings to `nil`, returning all other strings as-is.

## Public interface

```go
func nullIfEmpty(v string) any
```

Note: the function is unexported (lowercase), so it is only callable from within the `utils` package itself.

## Key invariants

- A string that is empty or contains only whitespace characters is returned as `nil`.
- A non-empty, non-whitespace string is returned unchanged as a `string` (boxed in `any`).
- The return type is `any`, so callers must type-assert to `string` or `nil` depending on context.

## Non-obvious decisions

- **Return type is `any` instead of `*string`**: A more conventional approach for "nullable string" in Go would be to return `*string` (nil pointer for empty). Returning `any` means the caller cannot distinguish between a `nil` interface and a `string` without a type switch/assertion. This suggests the result is being assigned to a context that expects `any` (e.g., a database `NULL` or a generic map), but the exact usage site is not visible from this file alone.
