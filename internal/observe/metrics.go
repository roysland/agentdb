package observe

import (
	"sort"
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

// MetricsCollector accumulates per-tool metrics in memory.
type MetricsCollector struct {
	mu        sync.Mutex
	startTime time.Time
	tools     map[string]*ToolMetrics
}

// NewMetricsCollector creates a new MetricsCollector with the start time set to now.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		startTime: time.Now(),
		tools:     make(map[string]*ToolMetrics),
	}
}

// Record records a tool call with its duration and error state.
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
}

// ServerStats holds the aggregated server statistics returned by Stats().
type ServerStats struct {
	UptimeSeconds   int64                  `json:"uptime_seconds"`
	TotalRequests   int64                  `json:"total_requests"`
	ActiveCodebases int                    `json:"active_codebases"`
	Tools           map[string]ToolSummary `json:"tools"`
}

// ToolSummary holds the per-tool breakdown in server stats.
type ToolSummary struct {
	Count       int64 `json:"count"`
	AvgDuration int64 `json:"avg_duration_ms"`
	P95Duration int64 `json:"p95_duration_ms"`
	ErrorCount  int64 `json:"error_count"`
}

// Stats returns the current server statistics including uptime, total requests,
// and per-tool breakdown with average and p95 durations.
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

	return ServerStats{
		UptimeSeconds:   int64(time.Since(m.startTime).Seconds()),
		TotalRequests:   totalRequests,
		ActiveCodebases: 0, // populated by caller from database
		Tools:           tools,
	}
}

// Reset zeroes all counters and resets the start time.
func (m *MetricsCollector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.startTime = time.Now()
	m.tools = make(map[string]*ToolMetrics)
}
