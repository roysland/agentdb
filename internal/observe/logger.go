package observe

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"
)

// LogLevel represents the severity level of a log entry.
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

// LogEntry represents a structured log entry emitted by the MCP server.
type LogEntry struct {
	Timestamp    string          `json:"timestamp"`
	Level        string          `json:"level"`
	Operation    string          `json:"operation"`
	DurationMs   int64           `json:"duration_ms,omitempty"`
	Status       string          `json:"status,omitempty"`
	Error        string          `json:"error,omitempty"`
	Params       json.RawMessage `json:"params,omitempty"`
	ResponseSize int             `json:"response_size,omitempty"`
}

// Logger emits structured JSON logs to a configured output writer.
type Logger struct {
	level  LogLevel
	output io.Writer
	mu     sync.Mutex
}

// NewLogger creates a new Logger that emits entries at or above the given level.
func NewLogger(level LogLevel, output io.Writer) *Logger {
	return &Logger{
		level:  level,
		output: output,
	}
}

// Log serializes the entry to JSON and writes it to the output if the entry's
// level meets or exceeds the logger's configured level.
func (l *Logger) Log(entry LogEntry) {
	entryLevel := parseLevelString(entry.Level)
	if entryLevel < l.level {
		return
	}

	// Ensure timestamp is set in ISO 8601 format
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		// Silently drop log entry on marshal failure (never block tool execution)
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Write JSON followed by newline; silently drop on write failure
	_, _ = l.output.Write(append(data, '\n'))
}

// IsDebug returns true when the logger's configured level is Debug.
func (l *Logger) IsDebug() bool {
	return l.level <= LevelDebug
}

// ParseLogLevel converts a string to a LogLevel. Returns LevelInfo for
// unrecognized values.
func ParseLogLevel(s string) LogLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// parseLevelString converts a log entry's level string to a LogLevel for
// comparison against the logger's configured threshold.
func parseLevelString(level string) LogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}
