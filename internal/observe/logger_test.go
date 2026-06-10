package observe

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestNewLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(LevelInfo, &buf)
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
	if logger.level != LevelInfo {
		t.Errorf("expected level %d, got %d", LevelInfo, logger.level)
	}
}

func TestLogEmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(LevelInfo, &buf)

	logger.Log(LogEntry{
		Level:      "info",
		Operation:  "find_symbol",
		DurationMs: 42,
		Status:     "ok",
	})

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to unmarshal log output: %v", err)
	}

	if entry.Level != "info" {
		t.Errorf("expected level 'info', got %q", entry.Level)
	}
	if entry.Operation != "find_symbol" {
		t.Errorf("expected operation 'find_symbol', got %q", entry.Operation)
	}
	if entry.DurationMs != 42 {
		t.Errorf("expected duration_ms 42, got %d", entry.DurationMs)
	}
	if entry.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", entry.Status)
	}
}

func TestLogTimestampISO8601(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(LevelDebug, &buf)

	logger.Log(LogEntry{
		Level:     "info",
		Operation: "test_op",
	})

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify timestamp parses as RFC3339
	_, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
	if err != nil {
		t.Errorf("timestamp %q is not valid ISO 8601: %v", entry.Timestamp, err)
	}
}

func TestLogPreservesExplicitTimestamp(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(LevelDebug, &buf)

	ts := "2024-01-15T10:30:00.123Z"
	logger.Log(LogEntry{
		Timestamp: ts,
		Level:     "info",
		Operation: "test_op",
	})

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if entry.Timestamp != ts {
		t.Errorf("expected timestamp %q, got %q", ts, entry.Timestamp)
	}
}

func TestLogFiltersBelow(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(LevelWarn, &buf)

	// Debug and Info should be filtered
	logger.Log(LogEntry{Level: "debug", Operation: "op1"})
	logger.Log(LogEntry{Level: "info", Operation: "op2"})

	if buf.Len() != 0 {
		t.Errorf("expected no output for below-threshold levels, got %q", buf.String())
	}

	// Warn and Error should pass
	logger.Log(LogEntry{Level: "warn", Operation: "op3"})
	if buf.Len() == 0 {
		t.Error("expected output for warn level")
	}
}

func TestLogErrorField(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(LevelInfo, &buf)

	logger.Log(LogEntry{
		Level:     "error",
		Operation: "find_symbol",
		Status:    "error",
		Error:     "codebase not found",
	})

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if entry.Error != "codebase not found" {
		t.Errorf("expected error field 'codebase not found', got %q", entry.Error)
	}
}

func TestLogOmitsEmptyFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(LevelDebug, &buf)

	logger.Log(LogEntry{
		Level:     "info",
		Operation: "test_op",
	})

	// Check raw JSON doesn't contain omitempty fields
	raw := buf.String()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if _, ok := m["duration_ms"]; ok {
		t.Error("expected duration_ms to be omitted when zero")
	}
	if _, ok := m["status"]; ok {
		t.Error("expected status to be omitted when empty")
	}
	if _, ok := m["error"]; ok {
		t.Error("expected error to be omitted when empty")
	}
	if _, ok := m["params"]; ok {
		t.Error("expected params to be omitted when nil")
	}
	if _, ok := m["response_size"]; ok {
		t.Error("expected response_size to be omitted when zero")
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected LogLevel
	}{
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{"info", LevelInfo},
		{"INFO", LevelInfo},
		{"warn", LevelWarn},
		{"WARN", LevelWarn},
		{"error", LevelError},
		{"ERROR", LevelError},
		{"", LevelInfo},
		{"invalid", LevelInfo},
		{"  info  ", LevelInfo},
		{"  debug  ", LevelDebug},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseLogLevel(tt.input)
			if got != tt.expected {
				t.Errorf("ParseLogLevel(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestLogDebugFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(LevelDebug, &buf)

	params := json.RawMessage(`{"query":"foo"}`)
	logger.Log(LogEntry{
		Level:        "debug",
		Operation:    "find_symbol",
		Params:       params,
		ResponseSize: 1024,
	})

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if string(entry.Params) != `{"query":"foo"}` {
		t.Errorf("expected params %q, got %q", `{"query":"foo"}`, string(entry.Params))
	}
	if entry.ResponseSize != 1024 {
		t.Errorf("expected response_size 1024, got %d", entry.ResponseSize)
	}
}
