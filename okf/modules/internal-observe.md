---
commit: ca9fc700d18146947f08e753cfc9c793963f987b
description: 'Codebase knowledge for module: internal/observe'
files:
- internal/observe/logger.go
- internal/observe/logger_test.go
- internal/observe/metrics.go
- internal/observe/metrics_test.go
tags:
- module
timestamp: '2026-06-26'
title: internal/observe
type: Module
---

## What it does

The `observe` package provides structured JSON logging and in-memory metrics collection for the MCP server. It emits leveled log entries to a configurable writer and accumulates per-tool latency percentiles, parser health counters, indexing throughput, and graph topology statistics that can be snapshotted via `Stats()`.

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

func (m *MetricsCollector) Record(tool string, durationMs int64, isError bool)
func (m *MetricsCollector) RecordParseResult(status, reason string, symbolCount int)
func (m *MetricsCollector) RecordIndexRun(files, chunks, embeddingFailures, durationMs int64)
func (m *MetricsCollector) RecordGraphUpdate(symbols, edges int64)
func (m *MetricsCollector) SetSlowQueryThreshold(ms int64)
func (m *MetricsCollector) Stats() ServerStats
func (m *MetricsCollector) Reset()
```

**Returned stats structure**

```go
type ServerStats struct {
    UptimeSeconds   int64
    TotalRequests   int64
    ActiveCodebases int    // populated by caller, not by this package
    ErrorsLast60s   int64
    Tools           map[string]ToolSummary
    Parse           ParseSummary
    Index           IndexSummary
    Graph           GraphSummary
    SlowQueries     []SlowEntry
}
```

## Key invariants

- **Lock ordering**: Every public method on `MetricsCollector` acquires `m.mu` exactly once at entry and releases it on return; no method calls another locked method, so no nested-lock deadlock is possible.
- **Sliding window capacity is fixed at 1000** per tool — the `NewSlidingWindow` constructor is only called internally inside `Record`, so callers cannot resize it.
- **P95 caps the effective sample at 1000** even if the window holds more (it never does, given the capacity invariant above, but the guard is explicit).
- **Slow log and error-timestamp ring are append-only capped slices** — when capacity is exceeded, the slice is re-sliced to retain only the most recent entries (`len - cap:`), preserving order.
- **`ActiveCodebases` is always 0 from `Stats()`** — the struct comment explicitly states it is populated by the caller from the database; this package never sets it.
- **Log entries are silently dropped** on JSON marshal failure or write failure — logging never blocks or panics tool execution.
- **`ErrorsLast60s` is computed at snapshot time** by counting timestamps `>= now - 60_000 ms`, not by maintaining a rolling counter.

## Non-obvious decisions

- **P95 uses `⌈0.95 × N⌉ - 1` as the index** rather than the more common interpolation-based percentile. This is a deliberate choice to return an actual observed value from the dataset (a rank-based percentile) rather than a fractional interpolation, which matters when the window holds fewer than ~20 values and interpolation would produce a value no operation actually had.
- **`RecordParseResult` treats an unrecognized `status` as `"partial"`** rather than ignoring it or panicking. This means any future status string that isn't `"complete"` or `"text_fallback"` silently increments the partial counter — a defensive choice that keeps the parser pipeline running even if new statuses are added upstream without updating this switch.
- **The slow-query threshold defaults to 500 ms and can only be lowered via `SetSlowQueryThreshold`** — the setter rejects non-positive values, so once set to a valid threshold it can never be disabled (set to 0) through this method. The check `m.slowThreshMs > 0` in `Record` means a threshold of 0 would disable slow-query logging, but the setter prevents reaching that state.
