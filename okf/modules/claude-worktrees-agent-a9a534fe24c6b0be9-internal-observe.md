---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/observe'
files:
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/observe/logger.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/observe/logger_test.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/observe/metrics.go
- .claude/worktrees/agent-a9a534fe24c6b0be9/internal/observe/metrics_test.go
tags:
- module
timestamp: '2026-06-26'
title: .claude/worktrees/agent-a9a534fe24c6b0be9/internal/observe
type: Module
---

## What it does

The `observe` package provides structured logging and in-memory metrics collection for the MCP server. It emits JSON log entries to a configurable writer and accumulates per-tool latency distributions, parser health counters, indexing throughput, call-graph topology, and slow-query diagnostics that can be surfaced via a stats endpoint.

## Public interface

**Logging**

```go
type LogLevel int  // LevelDebug, LevelInfo, LevelWarn, LevelError

type LogEntry struct {
    Timestamp    string
    Level        string
    Operation    string
    DurationMs   int64
    Status       string
    Error        string
    Params       json.RawMessage
    ResponseSize int
}

func NewLogger(level LogLevel, output io.Writer) *Logger
func (l *Logger) Log(entry LogEntry)
func (l *Logger) IsDebug() bool
func ParseLogLevel(s string) LogLevel
```

**Metrics collection**

```go
func NewMetricsCollector() *MetricsCollector
func (m *MetricsCollector) SetSlowQueryThreshold(ms int64)
func (m *MetricsCollector) Record(tool string, durationMs int64, isError bool)
func (m *MetricsCollector) RecordParseResult(status, reason string, symbolCount int)
func (m *MetricsCollector) RecordIndexRun(files, chunks, embeddingFailures, durationMs int64)
func (m *MetricsCollector) RecordGraphUpdate(symbols, edges int64)
func (m *MetricsCollector) Stats() ServerStats
func (m *MetricsCollector) Reset()
```

**Stats snapshot**

```go
type ServerStats struct {
    UptimeSeconds   int64
    TotalRequests   int64
    ActiveCodebases int
    ErrorsLast60s   int64
    Tools           map[string]ToolSummary
    Parse           ParseSummary
    Index           IndexSummary
    Graph           GraphSummary
    SlowQueries     []SlowEntry
}
```

## Key invariants

- `Logger.Log` never blocks tool execution: JSON marshal failures and write errors are silently dropped.
- `MetricsCollector` is goroutine-safe; all mutation and reads go through a single `sync.Mutex`.
- Per-tool latency P95 is computed over a sliding window capped at 1000 samples; the window overwrites oldest values when full.
- The slow-query log and error-timestamp ring are bounded (`slowLogCap=20`, `errTSCap=120`) to prevent unbounded memory growth.
- `Stats()` returns a consistent snapshot taken under the lock; the slow-query slice is copied to avoid escaping the lock.
- `ActiveCodebases` is always 0 from the collector's perspective — the caller is expected to populate it from the database.

## Non-obvious decisions

- **P95 caps the effective sample count at 1000** (`effective := n; if effective > 1000 { effective = 1000 }`). This bounds the percentile index computation regardless of how many values the window holds, preventing the index from being dominated by a large window when only recent history matters.
- **`ceiling95` uses integer arithmetic** `(95*n + 99) / 100` rather than `math.Ceil(0.95*float64(n))`. This avoids floating-point rounding discrepancies and keeps the percentile index deterministic across platforms.
- **`RecordParseResult` treats an unrecognized status as `"partial"`** rather than ignoring it or returning an error. This ensures that new or unexpected parser states still contribute to the degraded-parse accounting rather than silently dropping from `TotalFiles`.
