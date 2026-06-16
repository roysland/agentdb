package observe

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// SlidingWindow tracks the last N values for percentile computation.
type SlidingWindow struct {
	values []int64
	size   int
	pos    int
	full   bool
}

// NewSlidingWindow creates a sliding window with the given capacity.
func NewSlidingWindow(size int) *SlidingWindow {
	return &SlidingWindow{
		values: make([]int64, size),
		size:   size,
		pos:    0,
		full:   false,
	}
}

// Add inserts a value into the sliding window, overwriting the oldest value
// when the window is full.
func (w *SlidingWindow) Add(value int64) {
	w.values[w.pos] = value
	w.pos++
	if w.pos >= w.size {
		w.pos = 0
		w.full = true
	}
}

// Count returns the number of values currently stored in the window.
func (w *SlidingWindow) Count() int {
	if w.full {
		return w.size
	}
	return w.pos
}

// P95 computes the 95th percentile of the values in the window.
// Returns 0 if the window is empty.
// The p95 is the value at index ⌈0.95 × N⌉ - 1 in the sorted values,
// where N = min(count, 1000).
func (w *SlidingWindow) P95() int64 {
	n := w.Count()
	if n == 0 {
		return 0
	}

	// Copy the active portion of the window for sorting.
	snapshot := make([]int64, n)
	if w.full {
		copy(snapshot, w.values)
	} else {
		copy(snapshot, w.values[:w.pos])
	}

	sort.Slice(snapshot, func(i, j int) bool {
		return snapshot[i] < snapshot[j]
	})

	// p95 index: ⌈0.95 × min(N, 1000)⌉ - 1 (zero-based)
	effective := n
	if effective > 1000 {
		effective = 1000
	}
	idx := ceiling95(effective) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return snapshot[idx]
}

// ceiling95 computes ⌈0.95 × n⌉ using integer arithmetic.
func ceiling95(n int) int {
	// ⌈0.95 * n⌉ = ⌈(95 * n) / 100⌉ = (95*n + 99) / 100
	return (95*n + 99) / 100
}

// ToolMetrics holds accumulated metrics for a single tool.
type ToolMetrics struct {
	Count      int64
	TotalMs    int64
	ErrorCount int64
	Window     *SlidingWindow
}

// ParseStats tracks parser health counters across the process lifetime.
type ParseStats struct {
	TotalFiles    int64
	Complete      int64
	TextFallbacks int64 // degraded to plain-text chunking
	Partial       int64 // merge conflict or nil tree
	Panics        int64 // tree-sitter panics recovered
	ZeroSymbols   int64 // parsed OK but yielded 0 symbols from a non-empty file
}

// IndexStats tracks indexing pipeline throughput across all runs.
type IndexStats struct {
	RunCount          int64
	FilesIndexed      int64
	ChunksIndexed     int64
	EmbeddingFailures int64
	TotalDurationMs   int64
}

// GraphStats tracks call graph topology accumulated across analyze runs.
type GraphStats struct {
	TotalSymbols int64
	TotalEdges   int64
}

// SlowEntry records a single operation that exceeded the slow-query threshold.
type SlowEntry struct {
	Timestamp  string `json:"timestamp"`
	Operation  string `json:"operation"`
	DurationMs int64  `json:"duration_ms"`
}

// MetricsCollector accumulates per-tool metrics and domain diagnostics in memory.
type MetricsCollector struct {
	mu           sync.Mutex
	startTime    time.Time
	tools        map[string]*ToolMetrics
	parseStats   ParseStats
	indexStats   IndexStats
	graphStats   GraphStats
	slowLog      []SlowEntry // recent slow operations, capped at slowLogCap
	slowLogCap   int
	slowThreshMs int64   // operations >= this threshold are flagged (default 500ms)
	errTS        []int64 // recent error unix-millisecond timestamps, capped at errTSCap
	errTSCap     int
}

// NewMetricsCollector creates a new MetricsCollector with the start time set to now.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		startTime:    time.Now(),
		tools:        make(map[string]*ToolMetrics),
		slowLogCap:   20,
		slowThreshMs: 500,
		errTSCap:     120,
	}
}

// SetSlowQueryThreshold overrides the duration threshold (in ms) above which an
// operation is recorded in the slow-query log. Call before the first request.
func (m *MetricsCollector) SetSlowQueryThreshold(ms int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ms > 0 {
		m.slowThreshMs = ms
	}
}

// Record records a tool call with its duration and error state.
// It also captures slow-query entries and error timestamps for rate tracking.
func (m *MetricsCollector) Record(tool string, durationMs int64, isError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tm, ok := m.tools[tool]
	if !ok {
		tm = &ToolMetrics{
			Window: NewSlidingWindow(1000),
		}
		m.tools[tool] = tm
	}

	tm.Count++
	tm.TotalMs += durationMs
	if isError {
		tm.ErrorCount++
	}
	tm.Window.Add(durationMs)

	// Slow query detection
	if m.slowThreshMs > 0 && durationMs >= m.slowThreshMs {
		entry := SlowEntry{
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			Operation:  tool,
			DurationMs: durationMs,
		}
		m.slowLog = append(m.slowLog, entry)
		if len(m.slowLog) > m.slowLogCap {
			m.slowLog = m.slowLog[len(m.slowLog)-m.slowLogCap:]
		}
	}

	// Error timestamp ring for rate calculation
	if isError {
		m.errTS = append(m.errTS, time.Now().UnixMilli())
		if len(m.errTS) > m.errTSCap {
			m.errTS = m.errTS[len(m.errTS)-m.errTSCap:]
		}
	}
}

// RecordParseResult updates parser health counters for a single file.
// status is "complete", "text_fallback", or "partial".
// reason is the StatusReason string (used to detect tree-sitter panics).
// symbolCount is the number of symbols extracted (0 is flagged for complete parses).
func (m *MetricsCollector) RecordParseResult(status, reason string, symbolCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.parseStats.TotalFiles++
	switch status {
	case "complete":
		m.parseStats.Complete++
		if symbolCount == 0 {
			m.parseStats.ZeroSymbols++
		}
	case "text_fallback":
		m.parseStats.TextFallbacks++
	case "partial":
		m.parseStats.Partial++
		if strings.HasPrefix(reason, "tree-sitter panic") {
			m.parseStats.Panics++
		}
	default:
		m.parseStats.Partial++
	}
}

// RecordIndexRun records the outcome of one full or incremental index run.
func (m *MetricsCollector) RecordIndexRun(files, chunks, embeddingFailures, durationMs int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indexStats.RunCount++
	m.indexStats.FilesIndexed += files
	m.indexStats.ChunksIndexed += chunks
	m.indexStats.EmbeddingFailures += embeddingFailures
	m.indexStats.TotalDurationMs += durationMs
}

// RecordGraphUpdate accumulates symbol and edge counts from one analyze run.
func (m *MetricsCollector) RecordGraphUpdate(symbols, edges int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.graphStats.TotalSymbols += symbols
	m.graphStats.TotalEdges += edges
}

// ParseSummary is the parse-health section of ServerStats.
type ParseSummary struct {
	TotalFiles    int64   `json:"total_files"`
	Complete      int64   `json:"complete"`
	TextFallbacks int64   `json:"text_fallbacks"`
	Partial       int64   `json:"partial"`
	Panics        int64   `json:"panics"`
	ZeroSymbols   int64   `json:"zero_symbols"`
	DegradedPct   float64 `json:"degraded_pct"` // (text_fallbacks + partial) / total × 100
}

// IndexSummary is the indexing-throughput section of ServerStats.
type IndexSummary struct {
	RunCount          int64 `json:"run_count"`
	FilesIndexed      int64 `json:"files_indexed"`
	ChunksIndexed     int64 `json:"chunks_indexed"`
	EmbeddingFailures int64 `json:"embedding_failures"`
	AvgDurationMs     int64 `json:"avg_duration_ms"`
}

// GraphSummary is the call-graph topology section of ServerStats.
type GraphSummary struct {
	TotalSymbols int64   `json:"total_symbols"`
	TotalEdges   int64   `json:"total_edges"`
	Density      float64 `json:"density"` // edges / symbols; 0 when no symbols
}

// ServerStats holds the aggregated server statistics returned by Stats().
type ServerStats struct {
	UptimeSeconds   int64                  `json:"uptime_seconds"`
	TotalRequests   int64                  `json:"total_requests"`
	ActiveCodebases int                    `json:"active_codebases"`
	ErrorsLast60s   int64                  `json:"errors_last_60s"`
	Tools           map[string]ToolSummary `json:"tools"`
	Parse           ParseSummary           `json:"parse"`
	Index           IndexSummary           `json:"index"`
	Graph           GraphSummary           `json:"graph"`
	SlowQueries     []SlowEntry            `json:"slow_queries,omitempty"`
}

// ToolSummary holds the per-tool breakdown in server stats.
type ToolSummary struct {
	Count       int64 `json:"count"`
	AvgDuration int64 `json:"avg_duration_ms"`
	P95Duration int64 `json:"p95_duration_ms"`
	ErrorCount  int64 `json:"error_count"`
}

// Stats returns the current server statistics including uptime, total requests,
// per-tool breakdown, parse health, indexing throughput, graph density, and
// recent slow-query entries.
func (m *MetricsCollector) Stats() ServerStats {
	m.mu.Lock()
	defer m.mu.Unlock()

	var totalRequests int64
	tools := make(map[string]ToolSummary, len(m.tools))

	for name, tm := range m.tools {
		totalRequests += tm.Count

		var avgDuration int64
		if tm.Count > 0 {
			avgDuration = tm.TotalMs / tm.Count
		}

		tools[name] = ToolSummary{
			Count:       tm.Count,
			AvgDuration: avgDuration,
			P95Duration: tm.Window.P95(),
			ErrorCount:  tm.ErrorCount,
		}
	}

	// errors_last_60s: count timestamps within the last minute.
	cutoffMs := time.Now().UnixMilli() - 60_000
	var errLast60s int64
	for _, ts := range m.errTS {
		if ts >= cutoffMs {
			errLast60s++
		}
	}

	// Parse summary
	parseSummary := ParseSummary{
		TotalFiles:    m.parseStats.TotalFiles,
		Complete:      m.parseStats.Complete,
		TextFallbacks: m.parseStats.TextFallbacks,
		Partial:       m.parseStats.Partial,
		Panics:        m.parseStats.Panics,
		ZeroSymbols:   m.parseStats.ZeroSymbols,
	}
	if m.parseStats.TotalFiles > 0 {
		degraded := m.parseStats.TextFallbacks + m.parseStats.Partial
		parseSummary.DegradedPct = float64(degraded) / float64(m.parseStats.TotalFiles) * 100
	}

	// Index summary
	indexSummary := IndexSummary{
		RunCount:          m.indexStats.RunCount,
		FilesIndexed:      m.indexStats.FilesIndexed,
		ChunksIndexed:     m.indexStats.ChunksIndexed,
		EmbeddingFailures: m.indexStats.EmbeddingFailures,
	}
	if m.indexStats.RunCount > 0 {
		indexSummary.AvgDurationMs = m.indexStats.TotalDurationMs / m.indexStats.RunCount
	}

	// Graph summary
	graphSummary := GraphSummary{
		TotalSymbols: m.graphStats.TotalSymbols,
		TotalEdges:   m.graphStats.TotalEdges,
	}
	if m.graphStats.TotalSymbols > 0 {
		graphSummary.Density = float64(m.graphStats.TotalEdges) / float64(m.graphStats.TotalSymbols)
	}

	// Snapshot slow log (copy to avoid escaping the lock)
	var slowSnapshot []SlowEntry
	if len(m.slowLog) > 0 {
		slowSnapshot = make([]SlowEntry, len(m.slowLog))
		copy(slowSnapshot, m.slowLog)
	}

	return ServerStats{
		UptimeSeconds:   int64(time.Since(m.startTime).Seconds()),
		TotalRequests:   totalRequests,
		ActiveCodebases: 0, // populated by caller from database
		ErrorsLast60s:   errLast60s,
		Tools:           tools,
		Parse:           parseSummary,
		Index:           indexSummary,
		Graph:           graphSummary,
		SlowQueries:     slowSnapshot,
	}
}

// Reset zeroes all counters and resets the start time.
func (m *MetricsCollector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.startTime = time.Now()
	m.tools = make(map[string]*ToolMetrics)
	m.parseStats = ParseStats{}
	m.indexStats = IndexStats{}
	m.graphStats = GraphStats{}
	m.slowLog = nil
	m.errTS = nil
}
