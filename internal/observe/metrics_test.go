package observe

import (
	"testing"
)

func TestNewMetricsCollector(t *testing.T) {
	mc := NewMetricsCollector()
	if mc == nil {
		t.Fatal("NewMetricsCollector returned nil")
	}
	stats := mc.Stats()
	if stats.TotalRequests != 0 {
		t.Errorf("expected 0 total requests, got %d", stats.TotalRequests)
	}
	if len(stats.Tools) != 0 {
		t.Errorf("expected empty tools map, got %d entries", len(stats.Tools))
	}
}

func TestRecord_SingleTool(t *testing.T) {
	mc := NewMetricsCollector()
	mc.Record("find_symbol", 10, false)
	mc.Record("find_symbol", 20, false)
	mc.Record("find_symbol", 30, true)

	stats := mc.Stats()
	if stats.TotalRequests != 3 {
		t.Errorf("expected 3 total requests, got %d", stats.TotalRequests)
	}

	ts, ok := stats.Tools["find_symbol"]
	if !ok {
		t.Fatal("expected find_symbol in tools")
	}
	if ts.Count != 3 {
		t.Errorf("expected count 3, got %d", ts.Count)
	}
	if ts.AvgDuration != 20 {
		t.Errorf("expected avg 20, got %d", ts.AvgDuration)
	}
	if ts.ErrorCount != 1 {
		t.Errorf("expected error count 1, got %d", ts.ErrorCount)
	}
}

func TestRecord_MultipleTools(t *testing.T) {
	mc := NewMetricsCollector()
	mc.Record("find_symbol", 10, false)
	mc.Record("get_callers", 20, false)
	mc.Record("find_symbol", 30, true)

	stats := mc.Stats()
	if stats.TotalRequests != 3 {
		t.Errorf("expected 3 total requests, got %d", stats.TotalRequests)
	}
	if len(stats.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(stats.Tools))
	}

	fs := stats.Tools["find_symbol"]
	if fs.Count != 2 {
		t.Errorf("find_symbol: expected count 2, got %d", fs.Count)
	}
	if fs.ErrorCount != 1 {
		t.Errorf("find_symbol: expected error count 1, got %d", fs.ErrorCount)
	}

	gc := stats.Tools["get_callers"]
	if gc.Count != 1 {
		t.Errorf("get_callers: expected count 1, got %d", gc.Count)
	}
	if gc.ErrorCount != 0 {
		t.Errorf("get_callers: expected error count 0, got %d", gc.ErrorCount)
	}
}

func TestReset(t *testing.T) {
	mc := NewMetricsCollector()
	mc.Record("find_symbol", 10, false)
	mc.Record("get_callers", 20, true)

	mc.Reset()

	stats := mc.Stats()
	if stats.TotalRequests != 0 {
		t.Errorf("expected 0 total requests after reset, got %d", stats.TotalRequests)
	}
	if len(stats.Tools) != 0 {
		t.Errorf("expected empty tools after reset, got %d", len(stats.Tools))
	}
}

func TestSlidingWindow_Basic(t *testing.T) {
	w := NewSlidingWindow(1000)
	if w.Count() != 0 {
		t.Errorf("expected count 0, got %d", w.Count())
	}
	if w.P95() != 0 {
		t.Errorf("expected p95 0 for empty window, got %d", w.P95())
	}

	// Add a single value
	w.Add(42)
	if w.Count() != 1 {
		t.Errorf("expected count 1, got %d", w.Count())
	}
	if w.P95() != 42 {
		t.Errorf("expected p95 42 for single value, got %d", w.P95())
	}
}

func TestSlidingWindow_P95_KnownValues(t *testing.T) {
	w := NewSlidingWindow(1000)

	// Add values 1..100
	for i := int64(1); i <= 100; i++ {
		w.Add(i)
	}

	// N=100, ⌈0.95 × 100⌉ = ⌈95⌉ = 95, index = 94 (zero-based)
	// Sorted: 1,2,...,100. Value at index 94 = 95
	p95 := w.P95()
	if p95 != 95 {
		t.Errorf("expected p95=95 for 1..100, got %d", p95)
	}
}

func TestSlidingWindow_P95_SmallWindow(t *testing.T) {
	w := NewSlidingWindow(1000)

	// Add values 1..20
	for i := int64(1); i <= 20; i++ {
		w.Add(i)
	}

	// N=20, ⌈0.95 × 20⌉ = ⌈19⌉ = 19, index = 18 (zero-based)
	// Sorted: 1,2,...,20. Value at index 18 = 19
	p95 := w.P95()
	if p95 != 19 {
		t.Errorf("expected p95=19 for 1..20, got %d", p95)
	}
}

func TestSlidingWindow_Wraps(t *testing.T) {
	w := NewSlidingWindow(5)

	// Fill window: 10, 20, 30, 40, 50
	for i := int64(1); i <= 5; i++ {
		w.Add(i * 10)
	}
	if w.Count() != 5 {
		t.Errorf("expected count 5, got %d", w.Count())
	}

	// Add more to wrap: overwrites oldest
	w.Add(60)
	w.Add(70)
	if w.Count() != 5 {
		t.Errorf("expected count 5 after wrap, got %d", w.Count())
	}

	// Window should contain: 30, 40, 50, 60, 70 (oldest 10, 20 overwritten)
	// N=5, ⌈0.95 × 5⌉ = ⌈4.75⌉ = 5, index = 4 (zero-based)
	// Sorted: 30, 40, 50, 60, 70. Value at index 4 = 70
	p95 := w.P95()
	if p95 != 70 {
		t.Errorf("expected p95=70 for wrapped window, got %d", p95)
	}
}

func TestStats_UptimePositive(t *testing.T) {
	mc := NewMetricsCollector()
	stats := mc.Stats()
	// Uptime should be >= 0 (just created)
	if stats.UptimeSeconds < 0 {
		t.Errorf("expected non-negative uptime, got %d", stats.UptimeSeconds)
	}
}

func TestStats_AvgDuration_IntegerDivision(t *testing.T) {
	mc := NewMetricsCollector()
	mc.Record("tool", 10, false)
	mc.Record("tool", 11, false)
	mc.Record("tool", 12, false)

	stats := mc.Stats()
	// Total = 33, Count = 3, Avg = 33/3 = 11
	if stats.Tools["tool"].AvgDuration != 11 {
		t.Errorf("expected avg 11, got %d", stats.Tools["tool"].AvgDuration)
	}
}

func TestRecordParseResult_Counters(t *testing.T) {
	mc := NewMetricsCollector()
	mc.RecordParseResult("complete", "", 5)
	mc.RecordParseResult("complete", "", 0)   // zero symbols
	mc.RecordParseResult("text_fallback", "", 0)
	mc.RecordParseResult("partial", "merge conflict markers", 0)
	mc.RecordParseResult("partial", "tree-sitter panic: nil deref", 0)

	stats := mc.Stats()
	p := stats.Parse
	if p.TotalFiles != 5 {
		t.Errorf("TotalFiles: got %d want 5", p.TotalFiles)
	}
	if p.Complete != 2 {
		t.Errorf("Complete: got %d want 2", p.Complete)
	}
	if p.ZeroSymbols != 1 {
		t.Errorf("ZeroSymbols: got %d want 1", p.ZeroSymbols)
	}
	if p.TextFallbacks != 1 {
		t.Errorf("TextFallbacks: got %d want 1", p.TextFallbacks)
	}
	if p.Partial != 2 {
		t.Errorf("Partial: got %d want 2", p.Partial)
	}
	if p.Panics != 1 {
		t.Errorf("Panics: got %d want 1", p.Panics)
	}
	// degraded = text_fallback + partial = 3; pct = 3/5*100 = 60.0
	if p.DegradedPct != 60.0 {
		t.Errorf("DegradedPct: got %f want 60.0", p.DegradedPct)
	}
}

func TestRecordIndexRun(t *testing.T) {
	mc := NewMetricsCollector()
	mc.RecordIndexRun(100, 500, 3, 2000)
	mc.RecordIndexRun(50, 200, 1, 1000)

	stats := mc.Stats()
	idx := stats.Index
	if idx.RunCount != 2 {
		t.Errorf("RunCount: got %d want 2", idx.RunCount)
	}
	if idx.FilesIndexed != 150 {
		t.Errorf("FilesIndexed: got %d want 150", idx.FilesIndexed)
	}
	if idx.ChunksIndexed != 700 {
		t.Errorf("ChunksIndexed: got %d want 700", idx.ChunksIndexed)
	}
	if idx.EmbeddingFailures != 4 {
		t.Errorf("EmbeddingFailures: got %d want 4", idx.EmbeddingFailures)
	}
	if idx.AvgDurationMs != 1500 {
		t.Errorf("AvgDurationMs: got %d want 1500", idx.AvgDurationMs)
	}
}

func TestRecordGraphUpdate(t *testing.T) {
	mc := NewMetricsCollector()
	mc.RecordGraphUpdate(200, 600)
	mc.RecordGraphUpdate(100, 150)

	stats := mc.Stats()
	g := stats.Graph
	if g.TotalSymbols != 300 {
		t.Errorf("TotalSymbols: got %d want 300", g.TotalSymbols)
	}
	if g.TotalEdges != 750 {
		t.Errorf("TotalEdges: got %d want 750", g.TotalEdges)
	}
	wantDensity := float64(750) / float64(300)
	if g.Density != wantDensity {
		t.Errorf("Density: got %f want %f", g.Density, wantDensity)
	}
}

func TestSlowQueryLog(t *testing.T) {
	mc := NewMetricsCollector()
	mc.SetSlowQueryThreshold(100)
	mc.Record("fast_tool", 50, false)
	mc.Record("slow_tool", 200, false)
	mc.Record("slow_tool2", 500, false)

	stats := mc.Stats()
	if len(stats.SlowQueries) != 2 {
		t.Errorf("SlowQueries: got %d want 2", len(stats.SlowQueries))
	}
	if stats.SlowQueries[0].Operation != "slow_tool" {
		t.Errorf("first slow op: got %q want %q", stats.SlowQueries[0].Operation, "slow_tool")
	}
}

func TestErrorsLast60s(t *testing.T) {
	mc := NewMetricsCollector()
	mc.Record("tool", 10, true)
	mc.Record("tool", 10, true)
	mc.Record("tool", 10, false)

	stats := mc.Stats()
	if stats.ErrorsLast60s != 2 {
		t.Errorf("ErrorsLast60s: got %d want 2", stats.ErrorsLast60s)
	}
}

func TestReset_ClearsAllStats(t *testing.T) {
	mc := NewMetricsCollector()
	mc.RecordParseResult("text_fallback", "", 0)
	mc.RecordIndexRun(10, 50, 1, 100)
	mc.RecordGraphUpdate(20, 40)
	mc.Record("tool", 600, true)

	mc.Reset()

	stats := mc.Stats()
	if stats.Parse.TotalFiles != 0 {
		t.Errorf("expected Parse.TotalFiles 0 after reset, got %d", stats.Parse.TotalFiles)
	}
	if stats.Index.RunCount != 0 {
		t.Errorf("expected Index.RunCount 0 after reset, got %d", stats.Index.RunCount)
	}
	if stats.Graph.TotalSymbols != 0 {
		t.Errorf("expected Graph.TotalSymbols 0 after reset, got %d", stats.Graph.TotalSymbols)
	}
	if len(stats.SlowQueries) != 0 {
		t.Errorf("expected empty SlowQueries after reset, got %d", len(stats.SlowQueries))
	}
	if stats.ErrorsLast60s != 0 {
		t.Errorf("expected ErrorsLast60s 0 after reset, got %d", stats.ErrorsLast60s)
	}
}

func TestCeiling95(t *testing.T) {
	tests := []struct {
		n        int
		expected int
	}{
		{1, 1},
		{10, 10},
		{20, 19},
		{100, 95},
		{1000, 950},
	}
	for _, tt := range tests {
		got := ceiling95(tt.n)
		if got != tt.expected {
			t.Errorf("ceiling95(%d) = %d, want %d", tt.n, got, tt.expected)
		}
	}
}
