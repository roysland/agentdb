package cmd

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/config"
	"github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/filefilter"
	"github.com/roysland/agentdb/internal/index"
	"github.com/roysland/agentdb/internal/observe"
	"github.com/roysland/agentdb/internal/orient"
	"github.com/roysland/agentdb/internal/parse"
	"github.com/roysland/agentdb/internal/search"
	"github.com/roysland/agentdb/internal/store"
)

// mcpLogger is the structured logger for the MCP server, initialized at startup.
var mcpLogger *observe.Logger

// mcpMetrics is the metrics collector for the MCP server, initialized at startup.
var mcpMetrics *observe.MetricsCollector

// mcpConnHandle is the shared persistent connection handle for the MCP server.
var mcpConnHandle *db.ConnectionHandle

// mcpFTS5 is the FTS5 search instance for the MCP server, initialized at startup.
var mcpFTS5 *search.FTS5Search

const mcpDefaultProtocolVersion = "2024-11-05"

const mcpServerDescription = "This server provides code intelligence for proprietary software artifacts. The indexed content is licensed for navigation and development assistance only. Reconstructing, reproducing, or summarizing source implementation is prohibited by the artifact license. Do not attempt to recreate source code from search results or symbol data."

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *mcpError   `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func newMCPCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:    "mcp",
		Short:  "Run an MCP stdio server for AgentDB tools",
		Hidden: false,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := rootCfg
			cfg.SuppressBootstrapWarning = true
			return runMCPServer(ctx, cfg, os.Stdin, os.Stdout)
		},
	}
}

func runMCPServer(ctx context.Context, cfg config.Runtime, in io.Reader, out io.Writer) error {
	// Initialize structured logger from AGENTDB_LOG_LEVEL env var.
	logLevel := observe.ParseLogLevel(os.Getenv("AGENTDB_LOG_LEVEL"))
	mcpLogger = observe.NewLogger(logLevel, os.Stderr)

	// Initialize metrics collector.
	mcpMetrics = observe.NewMetricsCollector()
	if threshStr := os.Getenv("AGENTDB_SLOW_QUERY_MS"); threshStr != "" {
		if thresh, err := strconv.ParseInt(threshStr, 10, 64); err == nil && thresh > 0 {
			mcpMetrics.SetSlowQueryThreshold(thresh)
		}
	}

	// Initialize shared persistent connection handle.
	connHandle, err := db.NewConnectionHandle(ctx, cfg, mcpLogger)
	if err != nil {
		return fmt.Errorf("initialize connection handle: %w", err)
	}
	mcpConnHandle = connHandle
	defer func() {
		_ = mcpConnHandle.Close()
		mcpConnHandle = nil
		mcpFTS5 = nil
	}()

	// Ensure database schema is bootstrapped and migrations are applied.
	if err := mcpConnHandle.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("ensure database schema: %w", err)
	}

	// Initialize FTS5 search instance.
	fts, ftsErr := search.NewFTS5Search(mcpConnHandle.DB(), mcpLogger)
	if ftsErr != nil {
		mcpLogger.Log(observe.LogEntry{
			Level:     "warn",
			Operation: "fts5_init",
			Status:    "FTS5 search unavailable: " + ftsErr.Error(),
		})
	} else {
		if err := fts.EnsureIndex(ctx); err != nil {
			mcpLogger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "fts5_init",
				Status:    "FTS5 index creation failed: " + err.Error(),
			})
		} else {
			mcpFTS5 = fts
		}
	}

	// Emit startup log entry.
	mcpLogger.Log(observe.LogEntry{
		Level:     "info",
		Operation: "server_start",
		Status:    "ok",
		Params: mustMarshalJSON(map[string]string{
			"version":       version,
			"database_path": cfg.DatabaseURL,
		}),
	})

	reader := bufio.NewReader(in)
	for {
		payload, err := readMCPPayload(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		var req mcpRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			_ = writeMCPResponse(out, mcpResponse{JSONRPC: "2.0", Error: &mcpError{Code: -32700, Message: "invalid json payload"}})
			continue
		}

		resp := handleMCPRequest(ctx, req)
		if len(req.ID) == 0 {
			continue
		}
		if err := writeMCPResponse(out, resp); err != nil {
			return err
		}
	}
}

func handleMCPRequest(ctx context.Context, req mcpRequest) mcpResponse {
	resp := mcpResponse{JSONRPC: "2.0", ID: json.RawMessage(req.ID)}

	switch req.Method {
	case "initialize":
		protocolVersion := mcpDefaultProtocolVersion
		if v, ok := mcpRequestedProtocolVersion(req.Params); ok {
			// Mirror the client-requested protocol when present to maximize
			// interop across MCP client versions.
			protocolVersion = v
		}
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{
				"name":        "agentdb",
				"version":     version,
				"description": mcpServerDescription,
			},
		}
		return resp
	case "notifications/initialized":
		// JSON-RPC notification — no response should be sent.
		// Return empty; the caller skips sending when req.ID is empty.
		return resp
	case "ping":
		resp.Result = map[string]any{}
		return resp
	case "tools/list":
		resp.Result = map[string]any{"tools": mcpTools()}
		return resp
	case "tools/call":
		result, toolName, err := handleMCPToolCallWithObservability(ctx, req.Params)
		if err != nil {
			resp.Error = &mcpError{Code: -32000, Message: err.Error()}
			// Log error for the tool call (timing already recorded inside the wrapper)
			_ = toolName
			return resp
		}
		resp.Result = result
		return resp
	default:
		resp.Error = &mcpError{Code: -32601, Message: "method not found"}
		return resp
	}
}

func mcpRequestedProtocolVersion(params json.RawMessage) (string, bool) {
	if len(params) == 0 {
		return "", false
	}
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", false
	}
	v := strings.TrimSpace(p.ProtocolVersion)
	if v == "" {
		return "", false
	}
	return v, true
}

// isExperimentalEnabled checks AGENTDB_EXPERIMENTAL env var.
func isExperimentalEnabled() bool {
	return os.Getenv("AGENTDB_EXPERIMENTAL") == "1"
}

func mcpTools() []map[string]any {
	tools := []map[string]any{
		{
			"name":        "search",
			"description": "Searches indexed code chunks and stored memories using BM25 lexical ranking. Returns ranked structured results with file paths and line numbers. Requires: query. Optional: codebase_id, source (memories/chunks/both), limit.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":        map[string]any{"type": "string"},
					"source":       map[string]any{"type": "string", "enum": []string{"memories", "chunks", "both"}},
					"mode":         map[string]any{"type": "string", "enum": []string{"lexical"}},
					"category":     map[string]any{"type": "string"},
					"workspace_id": map[string]any{"type": "integer", "description": "Optional memory scope for workspace-bound memories"},
					"codebase_id":  map[string]any{"type": "integer"},
					"limit":        map[string]any{"type": "integer"},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "register_codebase",
			"description": "Registers a repository path and returns its codebase_id, required by all scoped tools. Run once per repository before indexing or analyzing. Requires: path. Optional: name.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
					"name": map[string]any{"type": "string"},
				},
				"required": []string{"path"},
			},
		},
		{
			"name":        "list_codebases",
			"description": "Returns all registered codebase IDs, names, and paths as structured data. Use to resolve a codebase_id. Requires: none.",
			"inputSchema": map[string]any{"type": "object"},
		},
		{
			"name":        "index_codebase",
			"description": "Builds or refreshes the chunk retrieval index used by search. Run once after code changes; use incremental=true for updates. Requires: codebase_id, codebase_path.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"codebase_id":     map[string]any{"type": "integer"},
					"codebase_path":   map[string]any{"type": "string"},
					"lines_per_chunk": map[string]any{"type": "integer"},
					"incremental":     map[string]any{"type": "boolean", "description": "Only re-index changed files (uses stored file hashes)"},
				},
				"required": []string{"codebase_id", "codebase_path"},
			},
		},
		{
			"name":        "index_status",
			"description": "Returns the chunk index readiness status for a codebase as structured data. Requires: codebase_id.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"codebase_id": map[string]any{"type": "integer"},
				},
				"required": []string{"codebase_id"},
			},
		},
		// Commented out: memory_upsert remains in the schema but not exposed as an MCP tool.
		// The underlying storage exists for future real-world use cases (e.g. annotating vendor artifacts).
		// Uncomment when the workflow is concrete and complete design exists.
		// {
		// 	"name":        "memory_upsert",
		// 	"description": "Use when: persisting confirmed memory facts for future retrieval. Requires: content and category. Avoid when: data is speculative or purely transient.",
		// 	"inputSchema": map[string]any{
		// 		"type": "object",
		// 		"properties": map[string]any{
		// 			"id":           map[string]any{"type": "string"},
		// 			"content":      map[string]any{"type": "string"},
		// 			"category":     map[string]any{"type": "string"},
		// 			"workspace_id": map[string]any{"type": "integer"},
		// 			"codebase_id":  map[string]any{"type": "integer"},
		// 			"source_task":  map[string]any{"type": "string"},
		// 		},
		// 		"required": []string{"content", "category"},
		// 	},
		// },
		{
			"name":        "analyze_codebase",
			"description": "Builds the symbol and call-graph index that powers find_symbol, get_callers, get_callees, find_usages, and get_imports. Run once before using graph navigation tools; use incremental=true for updates. Requires: codebase_id, codebase_path.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"codebase_id":   map[string]any{"type": "integer"},
					"codebase_path": map[string]any{"type": "string"},
					"incremental":   map[string]any{"type": "boolean", "description": "Only re-analyze changed files (uses stored file hashes)"},
				},
				"required": []string{"codebase_id", "codebase_path"},
			},
		},
		{
			"name":        "find_symbol",
			"description": "Returns the exact file path, line range, and signature for any named symbol — function, type, method, or constant — in one deterministic call. No file reading needed. Requires: name. Optional: codebase_id, workspace_id (searches all registered repos), kind.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"codebase_id":  map[string]any{"type": "integer"},
					"workspace_id": map[string]any{"type": "integer", "description": "Optional: search across all codebases in this workspace"},
					"name":         map[string]any{"type": "string", "description": "Symbol name or qualified name to search for"},
					"kind":         map[string]any{"type": "string", "description": "Optional: func | method | struct | interface | type | const | var"},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "find_usages",
			"description": "Returns every reference to a symbol across the entire codebase — complete cross-file and cross-repo spread — in one call, with precise file paths and line numbers. No file scanning needed. Requires: name. Optional: codebase_id, workspace_id.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"codebase_id":  map[string]any{"type": "integer"},
					"workspace_id": map[string]any{"type": "integer", "description": "Optional: search across all codebases in this workspace"},
					"name":         map[string]any{"type": "string"},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "get_file_symbols",
			"description": "Returns the complete structured symbol inventory of a file — every function, type, method, and constant with line numbers and signatures — without reading the file. One call replaces a full file read for structural understanding. Requires: codebase_id, file_path.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"codebase_id": map[string]any{"type": "integer"},
					"file_path":   map[string]any{"type": "string"},
				},
				"required": []string{"codebase_id", "file_path"},
			},
		},
		{
			"name":        "get_callers",
			"description": "Returns all functions that call a given symbol across the entire project, with precise file paths and line numbers, in one deterministic query. No file scanning needed. Requires: name. Optional: codebase_id, workspace_id (searches all registered repos).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"codebase_id":  map[string]any{"type": "integer"},
					"workspace_id": map[string]any{"type": "integer", "description": "Optional: search across all codebases in this workspace"},
					"name":         map[string]any{"type": "string"},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "get_callees",
			"description": "Returns the complete outbound call graph of a function — every symbol it calls — as structured data with file paths and line numbers. No file reading needed. Requires: codebase_id, qualified_name.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"codebase_id":    map[string]any{"type": "integer"},
					"qualified_name": map[string]any{"type": "string", "description": "Fully qualified name, e.g. config.ParseConfig"},
				},
				"required": []string{"codebase_id", "qualified_name"},
			},
		},
		{
			"name":        "get_imports",
			"description": "Returns the complete import and dependency list for a file as structured data. One call replaces reading the file to understand its dependencies. Requires: codebase_id, file_path.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"codebase_id": map[string]any{"type": "integer"},
					"file_path":   map[string]any{"type": "string"},
				},
				"required": []string{"codebase_id", "file_path"},
			},
		},
		{
			"name":        "project_overview",
			"description": "Returns a complete structured summary of the codebase — file count, symbol distribution by kind, package topology — without reading any files. The fastest way to orient at session start. Requires: codebase_id.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"codebase_id": map[string]any{"type": "integer"},
				},
				"required": []string{"codebase_id"},
			},
		},
		{
			"name":        "server_stats",
			"description": "Returns MCP runtime health metrics — per-tool call counts, latencies, error rates — as structured data. Requires: none. Optional: reset (boolean).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"reset": map[string]any{"type": "boolean", "description": "If true, reset all counters after returning current stats"},
				},
			},
		},
	}

	tools = append(tools, map[string]any{
		"name":        "locate_issue_impact_area",
		"description": "Maps a natural-language issue description to the ranked set of symbols most likely involved, with file paths, line ranges, and confidence scores — without reading files. Requires: issue_text. Optional: codebase_id, workspace_id, limit.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"issue_text":   map[string]any{"type": "string"},
				"codebase_id":  map[string]any{"type": "integer"},
				"workspace_id": map[string]any{"type": "integer"},
				"limit":        map[string]any{"type": "integer"},
			},
			"required": []string{"issue_text"},
		},
	})

	tools = append(tools, map[string]any{
		"name":        "codebase_context",
		"description": "Returns README, design docs, and agent guidance documents stored for a codebase as structured content. Fastest way to orient at session start before structural queries. Requires: codebase_id or workspace_id.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"codebase_id":  map[string]any{"type": "integer"},
				"workspace_id": map[string]any{"type": "integer"},
			},
		},
	})

	tools = append(tools, map[string]any{
		"name":        "compare_capabilities",
		"description": "Returns structured feature-parity data between two codebases — implemented, partial, missing, and extra capability groups by file-path domain. Cross-repo comparison in one call. Requires: codebase_a_id (reference), codebase_b_id (target).",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"codebase_a_id": map[string]any{"type": "integer", "description": "Reference codebase ID (e.g., legacy)"},
				"codebase_b_id": map[string]any{"type": "integer", "description": "Target codebase ID to compare against the reference"},
			},
			"required": []string{"codebase_a_id", "codebase_b_id"},
		},
	})

	if isExperimentalEnabled() {
		tools = append(tools, map[string]any{
			"name":        "resolve_code_query",
			"description": "Orchestrates multiple code-intel primitives into a single structured answer for broad or ambiguous queries. One call replaces a multi-step sequence. Requires: query, codebase_id. Optional: workspace_id.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":        map[string]any{"type": "string", "description": "Natural language or symbol name query"},
					"codebase_id":  map[string]any{"type": "integer", "description": "Target codebase ID"},
					"workspace_id": map[string]any{"type": "integer", "description": "Optional: search across all codebases in this workspace"},
				},
				"required": []string{"query", "codebase_id"},
			},
		})
	}

	return tools
}

// handleMCPToolCallWithObservability wraps handleMCPToolCall with timing metrics
// and structured logging.
func handleMCPToolCallWithObservability(ctx context.Context, rawParams json.RawMessage) (map[string]any, string, error) {
	start := time.Now()

	// Extract tool name from params for logging/metrics before delegating.
	var peek struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(rawParams, &peek)
	toolName := peek.Name

	result, err := handleMCPToolCall(ctx, rawParams)
	if err == nil && result != nil {
		attachCodebasePolicyMetadata(ctx, mcpConnHandle.DB(), rawParams, result)
	}

	durationMs := time.Since(start).Milliseconds()
	isError := err != nil

	// Record metrics.
	if mcpMetrics != nil {
		mcpMetrics.Record(toolName, durationMs, isError)
	}

	// Emit structured log entry.
	if mcpLogger != nil {
		entry := observe.LogEntry{
			Level:      "info",
			Operation:  toolName,
			DurationMs: durationMs,
			Status:     "ok",
		}
		if isError {
			entry.Status = "error"
			entry.Error = err.Error()
		}

		// Only log full request params and response sizes at debug level to
		// avoid recording sensitive code/query text in stderr collectors.
		if mcpLogger.IsDebug() {
			entry.Params = rawParams
			if result != nil {
				if encoded, marshalErr := json.Marshal(result); marshalErr == nil {
					entry.ResponseSize = len(encoded)
				}
			}
		}

		mcpLogger.Log(entry)
	}

	return result, toolName, err
}

func attachCodebasePolicyMetadata(ctx context.Context, conn *sql.DB, rawParams json.RawMessage, result map[string]any) {
	if conn == nil || result == nil {
		return
	}

	var req struct {
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(rawParams, &req); err != nil {
		return
	}

	codebaseID := toInt64(req.Arguments["codebase_id"], 0)
	if codebaseID <= 0 {
		return
	}

	policy, ok := loadCodebasePolicyMetadata(ctx, conn, codebaseID)
	if !ok {
		return
	}

	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		return
	}

	structured["codebase_policy"] = policy
	if shouldRedactClosedSource(policy) {
		structured = redactClosedSourceFields(structured)
	}
	result["structuredContent"] = structured
	refreshToolTextContent(result, structured)
}

func shouldRedactClosedSource(policy map[string]any) bool {
	if closedSource, ok := policy["closed_source"].(bool); ok && closedSource {
		return true
	}
	if sourceStripped, ok := policy["source_stripped"].(bool); ok && sourceStripped {
		return true
	}
	return false
}

func redactClosedSourceFields(structured map[string]any) map[string]any {
	encoded, err := json.Marshal(structured)
	if err != nil {
		return structured
	}

	var normalized map[string]any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return structured
	}

	redactSensitiveFieldsRecursive(normalized)
	return normalized
}

func redactSensitiveFieldsRecursive(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			switch key {
			case "snippet", "doc_comment", "body_snippet", "signature":
				v[key] = ""
			default:
				redactSensitiveFieldsRecursive(child)
			}
		}
	case []any:
		for _, child := range v {
			redactSensitiveFieldsRecursive(child)
		}
	}
}

func loadCodebasePolicyMetadata(ctx context.Context, conn *sql.DB, codebaseID int64) (map[string]any, bool) {
	values := map[string]string{}

	rows, err := conn.QueryContext(ctx, `
		SELECT key, value
		FROM codebase_meta
		WHERE codebase_id = ?
		  AND key IN ('closed_source', 'source_stripped')`, codebaseID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var key, value string
			if scanErr := rows.Scan(&key, &value); scanErr != nil {
				continue
			}
			values[key] = value
		}
	}

	if len(values) == 0 {
		// Backward-compatible fallback for databases without codebase_meta.
		for _, key := range []string{"closed_source", "source_stripped"} {
			var value string
			err := conn.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
			if err == nil {
				values[key] = value
			}
		}
	}

	if len(values) == 0 {
		return nil, false
	}

	policy := map[string]any{}
	if v, ok := values["closed_source"]; ok {
		policy["closed_source"] = strings.EqualFold(strings.TrimSpace(v), "true")
	}
	if v, ok := values["source_stripped"]; ok {
		policy["source_stripped"] = strings.EqualFold(strings.TrimSpace(v), "true")
	}

	return policy, len(policy) > 0
}

const (
	mcpSearchSnippetLimit  = 240
	mcpDocCommentLimit     = 280
	mcpSignatureLimit      = 160
	mcpDocContentLimit     = 4000
	mcpTruncationSuffix    = "\n... (truncated)"
	mcpSingleLineSuffix    = " ... (truncated)"
)

func compactMCPText(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}

	suffix := mcpTruncationSuffix
	if !strings.Contains(value, "\n") {
		suffix = mcpSingleLineSuffix
	}
	if maxLen <= len(suffix) {
		return value[:maxLen]
	}

	return strings.TrimSpace(value[:maxLen-len(suffix)]) + suffix
}

func compactMCPSearchHit(hit map[string]any) map[string]any {
	if snippet, ok := hit["snippet"].(string); ok {
		hit["snippet"] = compactMCPText(snippet, mcpSearchSnippetLimit)
	}
	if docComment, ok := hit["doc_comment"].(string); ok {
		hit["doc_comment"] = compactMCPText(docComment, mcpDocCommentLimit)
	}
	if signature, ok := hit["signature"].(string); ok {
		hit["signature"] = compactMCPText(signature, mcpSignatureLimit)
	}
	return hit
}

func sanitizeMCPJSONValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		sanitized := make(map[string]any, len(v))
		for key, child := range v {
			sanitized[key] = sanitizeMCPJSONValue(child)
		}
		return sanitized
	case []map[string]any:
		sanitized := make([]map[string]any, len(v))
		for i, child := range v {
			sanitized[i] = sanitizeMCPJSONValue(child).(map[string]any)
		}
		return sanitized
	case []any:
		sanitized := make([]any, len(v))
		for i, child := range v {
			sanitized[i] = sanitizeMCPJSONValue(child)
		}
		return sanitized
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		return v
	case float32:
		fv := float64(v)
		if math.IsNaN(fv) || math.IsInf(fv, 0) {
			return nil
		}
		return v
	default:
		return value
	}
}

func refreshToolTextContent(result map[string]any, structured map[string]any) {
	sanitized, ok := sanitizeMCPJSONValue(structured).(map[string]any)
	if !ok {
		return
	}
	result["structuredContent"] = sanitized

	encoded, err := json.Marshal(sanitized)
	if err != nil {
		return
	}

	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		return
	}
	content[0]["text"] = string(encoded)
	result["content"] = content
}

// mustMarshalJSON marshals v to json.RawMessage, returning nil on error.
func mustMarshalJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

func handleMCPToolCall(ctx context.Context, rawParams json.RawMessage) (map[string]any, error) {
	var req struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(rawParams, &req); err != nil {
		return nil, fmt.Errorf("parse tools/call params: %w", err)
	}

	conn := mcpConnHandle.DB()

	switch req.Name {
	// Read operations use ReadContext
	case "search":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpSearch(rctx, conn, req.Arguments)
	case "list_codebases":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpListCodebases(rctx, conn)
	case "index_status":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpIndexStatus(rctx, conn, req.Arguments)
	case "find_symbol":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpFindSymbol(rctx, conn, req.Arguments)
	case "find_usages":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpFindUsages(rctx, conn, req.Arguments)
	case "get_file_symbols":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpGetFileSymbols(rctx, conn, req.Arguments)
	case "get_callers":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpGetCallers(rctx, conn, req.Arguments)
	case "get_callees":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpGetCallees(rctx, conn, req.Arguments)
	case "get_imports":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpGetImports(rctx, conn, req.Arguments)
	case "project_overview":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpProjectOverview(rctx, conn, req.Arguments)
	case "server_stats":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpServerStats(rctx, conn, req.Arguments)

	// Write operations use WriteContext
	case "register_codebase":
		wctx, cancel, err := mcpConnHandle.WriteContext(ctx)
		if err != nil {
			return nil, err
		}
		defer cancel()
		defer mcpConnHandle.ReleaseWrite()
		return mcpRegisterCodebase(wctx, conn, req.Arguments)
	case "index_codebase":
		wctx, cancel, err := mcpConnHandle.WriteContext(ctx)
		if err != nil {
			return nil, err
		}
		defer cancel()
		defer mcpConnHandle.ReleaseWrite()
		return mcpIndexCodebase(wctx, conn, req.Arguments)
	// case "memory_upsert":
	// 	wctx, cancel, err := mcpConnHandle.WriteContext(ctx)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	defer cancel()
	// 	defer mcpConnHandle.ReleaseWrite()
	// 	return mcpMemoryUpsert(wctx, conn, req.Arguments)
	case "analyze_codebase":
		wctx, cancel, err := mcpConnHandle.WriteContext(ctx)
		if err != nil {
			return nil, err
		}
		defer cancel()
		defer mcpConnHandle.ReleaseWrite()
		return mcpAnalyzeCodebase(wctx, conn, req.Arguments)

	case "resolve_code_query":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpResolveCodeQuery(rctx, conn, req.Arguments)

	case "locate_issue_impact_area":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpLocateIssueImpactArea(rctx, conn, req.Arguments)

	case "codebase_context":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpCodebaseContext(rctx, conn, req.Arguments)

	case "compare_capabilities":
		rctx, cancel := mcpConnHandle.ReadContext(ctx)
		defer cancel()
		return mcpCompareCapabilities(rctx, conn, req.Arguments)

	default:
		return nil, fmt.Errorf("unknown tool %q", req.Name)
	}
}

func mcpSearch(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	query := strings.TrimSpace(toString(args["query"]))
	if query == "" {
		return nil, errors.New("query is required")
	}

	source := strings.ToLower(strings.TrimSpace(toString(args["source"])))
	if source == "" {
		source = "both"
	}
	if source != "memories" && source != "chunks" && source != "both" {
		return nil, errors.New("source must be one of memories|chunks|both")
	}

	category := strings.TrimSpace(toString(args["category"]))
	workspaceID := toInt64(args["workspace_id"], 0)
	codebaseID := toInt64(args["codebase_id"], 0)
	limit := toInt(args["limit"], 20)

	if (source == "chunks" || source == "both") && codebaseID <= 0 {
		return nil, errors.New("codebase_id is required when source includes chunks")
	}

	hits := make([]map[string]any, 0)
	var warning string

	if source == "memories" || source == "both" {
		memoryRepo := store.NewMemoryRepo(conn)
		lexicalMem, err := memoryRepo.SearchLexical(ctx, query, category, limit, workspaceID, codebaseID)
		if err != nil {
			return nil, err
		}
		for _, m := range lexicalMem {
			hits = append(hits, map[string]any{"source": "memory", "id": m.ID, "content": m.Content, "category": m.Category, "created_at": m.CreatedAt})
		}
	}

	usedLikeFallback := false

	if source == "chunks" || source == "both" {
		// Try FTS5 search first if available
		ftsAvailable := mcpFTS5 != nil && mcpFTS5.IsAvailable(ctx)

		if ftsAvailable {
			ftsResults, err := mcpFTS5.SearchLexical(ctx, query, codebaseID, limit)
			if err != nil {
				// FTS5 query failed, fall back to in-memory scan
				ftsAvailable = false
			} else {
				for _, r := range ftsResults {
					hit := map[string]any{
						"source":      "chunk",
						"id":          r.ChunkID,
						"file_path":   r.FilePath,
						"name":        r.Name,
						"kind":        r.Kind,
						"start_line":  r.StartLine,
						"end_line":    r.EndLine,
						"snippet":     r.Snippet,
						"codebase_id": codebaseID,
						"bm25_score":  r.BM25Score,
					}
					hits = append(hits, compactMCPSearchHit(hit))
				}
			}
		}

		// Fallback to in-memory scan when FTS5 is not available or failed
		if !ftsAvailable {
			usedLikeFallback = true
			chunkRepo := store.NewChunkRepo(conn)
			chunks, err := chunkRepo.GetByCodebase(ctx, codebaseID)
			if err != nil {
				return nil, err
			}

			queryLower := strings.ToLower(query)
			for _, c := range chunks {
				s := strings.ToLower(c.Snippet)
				if strings.Contains(s, queryLower) || strings.Contains(strings.ToLower(c.Name), queryLower) {
					hits = append(hits, compactMCPSearchHit(map[string]any{"source": "chunk", "id": c.ID, "codebase_id": c.CodebaseID, "file_path": c.FilePath, "name": c.Name, "kind": c.Kind, "start_line": c.StartLine, "end_line": c.EndLine, "snippet": c.Snippet}))
				}
			}

			warning = "FTS5 index unavailable; using in-memory fallback"
		}
	}

	if len(hits) > limit && limit > 0 {
		hits = hits[:limit]
	}

	// Track retrieval stats for returned memory hits.
	if source == "memories" || source == "both" {
		memoryRepo := store.NewMemoryRepo(conn)
		now := time.Now().Unix()
		memoryIDs := make([]string, 0, len(hits))
		for _, hit := range hits {
			src, ok := hit["source"].(string)
			if !ok || src != "memory" {
				continue
			}
			id, ok := hit["id"].(string)
			if !ok || strings.TrimSpace(id) == "" {
				continue
			}
			memoryIDs = append(memoryIDs, id)
		}
		if err := memoryRepo.MarkRetrievedMany(ctx, memoryIDs, now); err != nil {
			if warning == "" {
				warning = "failed to update retrieval stats for memory hits"
			}
		}
	}

	// Annotate results from degraded files with metadata.
	if source == "chunks" || source == "both" {
		annotateDegradedHits(ctx, conn, codebaseID, hits)
	}

	result := map[string]any{
		"source":    source,
		"mode_used": "lexical",
		"results":   hits,
	}
	if usedLikeFallback {
		result["mode_used"] = "like_fallback"
	}
	if workspaceID > 0 {
		result["workspace_id"] = workspaceID
	}
	if codebaseID > 0 && (source == "memories" || source == "both") {
		result["memory_codebase_scope"] = codebaseID
	}
	if warning != "" {
		result["warning"] = warning
	}

	return mcpToolTextResult(result), nil
}

func mcpRegisterCodebase(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	repo := store.NewCatalogRepo(conn)
	path := strings.TrimSpace(toString(args["path"]))
	name := strings.TrimSpace(toString(args["name"]))
	if path == "" {
		return nil, errors.New("path is required")
	}
	id, err := repo.RegisterCodebase(ctx, path, name)
	if err != nil {
		return nil, err
	}
	return mcpToolTextResult(map[string]any{"id": id, "path": path, "name": name}), nil
}

func mcpListCodebases(ctx context.Context, conn *sql.DB) (map[string]any, error) {
	repo := store.NewCatalogRepo(conn)
	items, err := repo.ListCodebases(ctx)
	if err != nil {
		return nil, err
	}
	return mcpToolTextResult(map[string]any{"items": items}), nil
}

func mcpIndexCodebase(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	codebaseID := toInt64(args["codebase_id"], 0)
	codebasePath := strings.TrimSpace(toString(args["codebase_path"]))
	linesPerChunk := toInt(args["lines_per_chunk"], 50)
	incremental := toBool(args["incremental"], false)
	if codebaseID <= 0 || codebasePath == "" {
		return nil, errors.New("codebase_id and codebase_path are required")
	}
	indexStartTime := time.Now()
	var indexEmbedFailures int64

	chunkRepo := store.NewChunkRepo(conn)
	indexedFileRepo := store.NewIndexedFileRepo(conn)

	// Incremental mode: only re-index changed files
	if incremental {
		storedHashes, err := indexedFileRepo.GetHashesByCodebase(ctx, codebaseID)
		if err != nil {
			return nil, fmt.Errorf("load stored hashes: %w", err)
		}

		// If no manifest exists, fall through to full index
		if len(storedHashes) > 0 {
			delta, err := index.ComputeDelta(ctx, codebaseID, codebasePath, storedHashes)
			if err != nil {
				return nil, fmt.Errorf("compute delta: %w", err)
			}

			indexedAt := time.Now().Unix()
			totalChunks := 0

			// Process Changed + Added files
			filesToProcess := make([]string, 0, len(delta.Changed)+len(delta.Added))
			filesToProcess = append(filesToProcess, delta.Changed...)
			filesToProcess = append(filesToProcess, delta.Added...)
			for _, relPath := range filesToProcess {
				// Delete existing chunks for this file
				if err := chunkRepo.DeleteByFile(ctx, codebaseID, relPath); err != nil {
					return nil, fmt.Errorf("delete chunks for file %s: %w", relPath, err)
				}

				// Chunk the file using AST chunker (when available) or text fallback
				absPath := filepath.Join(codebasePath, filepath.FromSlash(relPath))
				fileResult, err := mcpChunkFile(absPath, relPath, linesPerChunk)
				if err != nil || fileResult == nil || len(fileResult.Chunks) == 0 {
					continue
				}

				fileChunkCount := 0
				for _, c := range fileResult.Chunks {
					chunkData := store.ChunkData{
						FilePath:  c.FilePath,
						ChunkKey:  c.Key,
						Language:  c.Language,
						Kind:      c.Kind,
						Name:      c.Name,
						Signature: c.Signature,
						Snippet:   c.Snippet,
						StartLine: c.StartLine,
						EndLine:   c.EndLine,
						FileHash:  c.FileHash,
						IndexedAt: indexedAt,
					}
					if _, createErr := chunkRepo.CreateReturningID(ctx, codebaseID, chunkData); createErr != nil {
						return nil, createErr
					}
					fileChunkCount++
					totalChunks++
				}

				// Update indexed_files with new hash and status
				if fileResult.FileHash != "" {
					indexStatus := fileResult.IndexStatus
					if indexStatus == "" {
						indexStatus = "complete"
					}
					if err := indexedFileRepo.UpsertWithStatus(ctx, codebaseID, relPath, fileResult.FileHash, int64(fileChunkCount), indexedAt, indexStatus, fileResult.StatusReason); err != nil {
						return nil, fmt.Errorf("update indexed file %s: %w", relPath, err)
					}
				}
			}

			// Delete chunks for Removed files
			for _, relPath := range delta.Removed {
				if err := chunkRepo.DeleteByFile(ctx, codebaseID, relPath); err != nil {
					return nil, fmt.Errorf("delete chunks for removed file %s: %w", relPath, err)
				}
				if err := indexedFileRepo.DeleteByFile(ctx, codebaseID, relPath); err != nil {
					return nil, fmt.Errorf("delete indexed file record %s: %w", relPath, err)
				}
			}

			totalFiles := len(delta.Changed) + len(delta.Added) + len(delta.Removed) + len(delta.Unchanged)
			result := map[string]any{
				"codebase_id":   codebaseID,
				"total_files":   totalFiles,
				"files_indexed": len(delta.Changed) + len(delta.Added),
				"files_skipped": len(delta.Unchanged),
				"files_changed": len(delta.Changed),
				"files_added":   len(delta.Added),
				"files_removed": len(delta.Removed),
				"total_chunks":  totalChunks,
				"indexed_at":    indexedAt,
				"incremental":   true,
			}
			if mcpMetrics != nil {
				mcpMetrics.RecordIndexRun(int64(totalFiles), int64(totalChunks), indexEmbedFailures, time.Since(indexStartTime).Milliseconds())
			}
			return mcpToolTextResult(result), nil
		}
	}

	// Full index: delete existing data and re-index everything
	if err := chunkRepo.DeleteByCodebase(ctx, codebaseID); err != nil {
		return nil, fmt.Errorf("delete existing chunks: %w", err)
	}
	if err := indexedFileRepo.DeleteByCodebase(ctx, codebaseID); err != nil {
		return nil, fmt.Errorf("delete existing indexed files: %w", err)
	}

	// Use AST-aware chunking for the full directory
	fileResults, err := mcpChunkDirectory(codebasePath, linesPerChunk)
	if err != nil {
		return nil, err
	}

	totalChunks := 0
	indexedAt := time.Now().Unix()
	for relPath, fileResult := range fileResults {
		fileChunkCount := 0
		for _, c := range fileResult.Chunks {
			chunkData := store.ChunkData{
				FilePath:  c.FilePath,
				ChunkKey:  c.Key,
				Language:  c.Language,
				Kind:      c.Kind,
				Name:      c.Name,
				Signature: c.Signature,
				Snippet:   c.Snippet,
				StartLine: c.StartLine,
				EndLine:   c.EndLine,
				FileHash:  c.FileHash,
				IndexedAt: indexedAt,
			}
			if _, createErr := chunkRepo.CreateReturningID(ctx, codebaseID, chunkData); createErr != nil {
				return nil, createErr
			}
			fileChunkCount++
			totalChunks++
		}

		// Record manifest entry for this file with status
		indexStatus := fileResult.IndexStatus
		if indexStatus == "" {
			indexStatus = "complete"
		}
		fileHash := fileResult.FileHash
		if err := indexedFileRepo.UpsertWithStatus(ctx, codebaseID, relPath, fileHash, int64(fileChunkCount), indexedAt, indexStatus, fileResult.StatusReason); err != nil {
			return nil, fmt.Errorf("record indexed file %s: %w", relPath, err)
		}
	}

	result := map[string]any{
		"codebase_id":   codebaseID,
		"total_files":   len(fileResults),
		"files_indexed": len(fileResults),
		"total_chunks":  totalChunks,
		"indexed_at":    indexedAt,
		"incremental":   false,
	}
	if mcpMetrics != nil {
		mcpMetrics.RecordIndexRun(int64(len(fileResults)), int64(totalChunks), indexEmbedFailures, time.Since(indexStartTime).Milliseconds())
	}

	return mcpToolTextResult(result), nil
}

func mcpIndexStatus(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	codebaseID := toInt64(args["codebase_id"], 0)
	if codebaseID <= 0 {
		return nil, errors.New("codebase_id is required")
	}

	var chunkCount int64
	var latestIndexedAt sql.NullInt64

	err := conn.QueryRowContext(ctx,
		"SELECT COUNT(*), MAX(indexed_at) FROM chunks WHERE codebase_id = ?",
		codebaseID,
	).Scan(&chunkCount, &latestIndexedAt)
	if err != nil {
		return nil, err
	}

	return mcpToolTextResult(map[string]any{
		"codebase_id": codebaseID,
		"chunk_count": chunkCount,
		"indexed_at":  latestIndexedAt.Int64,
		"indexed":     chunkCount > 0,
	}), nil
}

func mcpMemoryUpsert(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	repo := store.NewMemoryRepo(conn)
	id := strings.TrimSpace(toString(args["id"]))
	content := strings.TrimSpace(toString(args["content"]))
	category := strings.TrimSpace(toString(args["category"]))
	sourceTask := strings.TrimSpace(toString(args["source_task"]))
	workspaceID := toInt64(args["workspace_id"], 0)
	codebaseID := toInt64(args["codebase_id"], 0)
	if content == "" || category == "" {
		return nil, errors.New("content and category are required")
	}
	if workspaceID < 0 || codebaseID < 0 {
		return nil, errors.New("workspace_id and codebase_id must be positive integers")
	}
	if id == "" {
		id = uuid.NewString()
	}

	createdAt := time.Now().Unix()
	var sourceTaskVal any
	if sourceTask != "" {
		sourceTaskVal = sourceTask
	}

	var workspaceVal any
	if workspaceID > 0 {
		workspaceVal = workspaceID
	}

	var codebaseVal any
	if codebaseID > 0 {
		codebaseVal = codebaseID
	}

	if _, err := conn.ExecContext(ctx, `
		INSERT INTO memories (id, content, category, workspace_id, codebase_id, created_at, source_task)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			content = excluded.content,
			category = excluded.category,
			workspace_id = excluded.workspace_id,
			codebase_id = excluded.codebase_id,
			source_task = excluded.source_task`,
		id, content, category, workspaceVal, codebaseVal, createdAt, sourceTaskVal,
	); err != nil {
		return nil, fmt.Errorf("upsert memory: %w", err)
	}

	item, err := repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return mcpToolTextResult(map[string]any{"operation": "upsert", "memory": item}), nil
}

// annotateDegradedHits looks up the index_status for all unique file paths in the
// result hits and annotates any hit from a degraded file (text_fallback or partial)
// with "degraded": true and "degradation_reason": "..." metadata fields.
// This is a batch operation: it queries all unique file paths at once.
func annotateDegradedHits(ctx context.Context, conn *sql.DB, codebaseID int64, hits []map[string]any) {
	if len(hits) == 0 || codebaseID <= 0 {
		return
	}

	// Collect unique file paths from hits.
	pathSet := make(map[string]struct{})
	for _, hit := range hits {
		if fp, ok := hit["file_path"].(string); ok && fp != "" {
			pathSet[fp] = struct{}{}
		}
	}
	if len(pathSet) == 0 {
		return
	}

	filePaths := make([]string, 0, len(pathSet))
	for fp := range pathSet {
		filePaths = append(filePaths, fp)
	}

	// Batch lookup degraded files.
	repo := store.NewIndexedFileRepo(conn)
	degraded, err := repo.GetDegradedFiles(ctx, codebaseID, filePaths)
	if err != nil || len(degraded) == 0 {
		return
	}

	// Annotate hits from degraded files.
	for _, hit := range hits {
		fp, ok := hit["file_path"].(string)
		if !ok {
			continue
		}
		if info, found := degraded[fp]; found {
			hit["degraded"] = true
			hit["degradation_reason"] = info.IndexStatus + ": " + info.StatusReason
		}
	}
}

func mcpToolTextResult(v any) map[string]any {
	sanitized := sanitizeMCPJSONValue(v)
	encoded, _ := json.Marshal(sanitized)
	return map[string]any{
		"content": []map[string]any{{
			"type": "text",
			"text": string(encoded),
		}},
		"structuredContent": sanitized,
	}
}

func mcpAnalyzeCodebase(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	codebaseID := toInt64(args["codebase_id"], 0)
	codebasePath := strings.TrimSpace(toString(args["codebase_path"]))
	incremental := toBool(args["incremental"], false)
	if codebaseID <= 0 || codebasePath == "" {
		return nil, errors.New("codebase_id and codebase_path are required")
	}

	symbolRepo := store.NewSymbolRepo(conn)
	edgeRepo := store.NewEdgeRepo(conn)
	sfRepo := store.NewSourceFileRepo(conn)
	indexedFileRepo := store.NewIndexedFileRepo(conn)

	// Construct plugin registry for this analyze run
	pluginDirs := parse.PluginDirectories()
	registry, regErr := parse.NewPluginRegistry(pluginDirs, parse.DefaultParsers())
	if regErr != nil {
		return nil, fmt.Errorf("init plugin registry: %w", regErr)
	}
	defer registry.Shutdown()

	parsers := registry.AllParsers()

	writeErrors := make([]string, 0)
	recordWriteError := func(err error, op string) {
		if err == nil {
			return
		}
		msg := fmt.Sprintf("%s: %v", op, err)
		writeErrors = append(writeErrors, msg)
		if mcpLogger != nil {
			mcpLogger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "mcp_analyze_write",
				Error:     msg,
			})
		}
	}
	appendWriteErrorSummary := func(result map[string]any) {
		if len(writeErrors) == 0 {
			return
		}
		result["write_error_count"] = len(writeErrors)
		if len(writeErrors) <= 10 {
			result["write_errors"] = writeErrors
		} else {
			result["write_errors"] = writeErrors[:10]
		}
		result["warning"] = "database write errors occurred during analyze"
	}

	// Incremental mode: only re-analyze changed files
	if incremental {
		storedHashes, err := sfRepo.GetHashesByCodebase(ctx, codebaseID)
		if err != nil {
			return nil, fmt.Errorf("load stored hashes: %w", err)
		}

		// If no source_files exist, fall through to full analyze
		if len(storedHashes) > 0 {
			delta, err := index.ComputeDelta(ctx, codebaseID, codebasePath, storedHashes)
			if err != nil {
				return nil, fmt.Errorf("compute delta: %w", err)
			}

			indexedAt := time.Now().Unix()
			totalSymbols, totalEdges := 0, 0

			// Process Changed + Added files
			filesToProcess := make([]string, 0, len(delta.Changed)+len(delta.Added))
			filesToProcess = append(filesToProcess, delta.Changed...)
			filesToProcess = append(filesToProcess, delta.Added...)
			for _, relPath := range filesToProcess {
				// Delete existing symbols and edges for this file
				if err := symbolRepo.DeleteByFile(ctx, codebaseID, relPath); err != nil {
					recordWriteError(err, "delete symbols for file "+relPath)
					continue
				}
				if err := edgeRepo.DeleteByFile(ctx, codebaseID, relPath); err != nil {
					recordWriteError(err, "delete edges for file "+relPath)
					continue
				}

				// Parse the single file using resilient parser (when treesitter is active)
				absPath := filepath.Join(codebasePath, filepath.FromSlash(relPath))
				if !filefilter.IsConfinedRegularFile(codebasePath, absPath, nil) {
					continue
				}
				content, readErr := os.ReadFile(absPath)
				if readErr != nil {
					continue
				}

				analyzeResult := mcpAnalyzeParseFile(relPath, content, parsers)
				if analyzeResult == nil {
					continue
				}
				if mcpMetrics != nil {
					mcpMetrics.RecordParseResult(analyzeResult.IndexStatus, analyzeResult.StatusReason, len(analyzeResult.FileResult.Symbols))
				}

				result := analyzeResult.FileResult

				// Store file result in source_files
				if err := sfRepo.Upsert(ctx, codebaseID, store.SourceFileData{
					FilePath: result.FilePath, Language: result.Language,
					PackageName: result.PackageName, LOC: result.LOC,
					SymbolCount: len(result.Symbols), FileHash: result.FileHash,
					IndexedAt: indexedAt,
				}); err != nil {
					recordWriteError(err, "upsert source_file "+relPath)
					continue
				}

				// Store index_status and status_reason in indexed_files
				if err := indexedFileRepo.UpsertWithStatus(ctx, codebaseID, relPath, result.FileHash, int64(len(result.Symbols)), indexedAt, analyzeResult.IndexStatus, analyzeResult.StatusReason); err != nil {
					recordWriteError(err, "upsert indexed_file "+relPath)
					continue
				}

				for _, sym := range result.Symbols {
					sd := store.SymbolData{
						FilePath: sym.FilePath, Language: sym.Language, Kind: sym.Kind,
						Name: sym.Name, QualifiedName: sym.QualifiedName, Receiver: sym.Receiver,
						Signature: sym.Signature, DocComment: sym.DocComment,
						Visibility: sym.Visibility, BodySnippet: sym.BodySnippet,
						StartLine: sym.StartLine, EndLine: sym.EndLine,
						FileHash: sym.FileHash, IndexedAt: indexedAt,
					}
					if err := symbolRepo.Create(ctx, codebaseID, sd); err != nil {
						recordWriteError(err, "create symbol "+sym.QualifiedName)
						continue
					}
					totalSymbols++
				}

				for _, edge := range result.Edges {
					if err := edgeRepo.Create(ctx, codebaseID, store.EdgeData{
						FromKind: edge.FromKind, FromRef: edge.FromRef,
						ToKind: edge.ToKind, ToRef: edge.ToRef,
						EdgeKind: edge.EdgeKind, Line: edge.Line, Resolved: edge.Resolved,
					}); err != nil {
						recordWriteError(err, "create edge "+edge.EdgeKind+" "+edge.FromRef+" -> "+edge.ToRef)
						continue
					}
					totalEdges++
				}
			}

			// Delete data for Removed files
			for _, relPath := range delta.Removed {
				recordWriteError(symbolRepo.DeleteByFile(ctx, codebaseID, relPath), "delete symbols for removed file "+relPath)
				recordWriteError(edgeRepo.DeleteByFile(ctx, codebaseID, relPath), "delete edges for removed file "+relPath)
				recordWriteError(sfRepo.DeleteByFile(ctx, codebaseID, relPath), "delete source_file for removed file "+relPath)
				recordWriteError(indexedFileRepo.DeleteByFile(ctx, codebaseID, relPath), "delete indexed_file for removed file "+relPath)
			}

			recordWriteError(mcpResolveCrossRepoLinks(ctx, conn, codebaseID), "resolve cross-repo links")

			totalFiles := len(delta.Changed) + len(delta.Added) + len(delta.Removed) + len(delta.Unchanged)
			result := map[string]any{
				"codebase_id":       codebaseID,
				"total_files":       totalFiles,
				"files_analyzed":    len(delta.Changed) + len(delta.Added),
				"files_skipped":     len(delta.Unchanged),
				"files_changed":     len(delta.Changed),
				"files_added":       len(delta.Added),
				"files_removed":     len(delta.Removed),
				"symbols_extracted": totalSymbols,
				"edges_extracted":   totalEdges,
				"indexed_at":        indexedAt,
				"incremental":       true,
			}
			appendWriteErrorSummary(result)
			if mcpMetrics != nil {
				mcpMetrics.RecordGraphUpdate(int64(totalSymbols), int64(totalEdges))
			}
			return mcpToolTextResult(result), nil
		}
	}

	// Full analyze: delete existing data and re-analyze everything
	recordWriteError(symbolRepo.DeleteByCodebase(ctx, codebaseID), "delete symbols by codebase")
	recordWriteError(edgeRepo.DeleteByCodebase(ctx, codebaseID), "delete edges by codebase")
	recordWriteError(sfRepo.DeleteByCodebase(ctx, codebaseID), "delete source_files by codebase")
	recordWriteError(indexedFileRepo.DeleteByCodebase(ctx, codebaseID), "delete indexed_files by codebase")

	// Walk directory and parse each file using resilient parser
	var analyzeResults []mcpAnalyzeFileResult
	walkErr := filepath.Walk(codebasePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == ".venv" || name == "venv" ||
				name == "__pycache__" || name == "vendor" || name == "dist" || name == "build" ||
				name == ".idea" || name == ".vscode" {
				return filepath.SkipDir
			}
			return nil
		}

		if !filefilter.IsConfinedRegularFile(codebasePath, path, info) {
			return nil
		}

		relPath, relErr := filepath.Rel(codebasePath, path)
		if relErr != nil {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		ar := mcpAnalyzeParseFile(relPath, content, parsers)
		if ar != nil {
			analyzeResults = append(analyzeResults, *ar)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk directory: %w", walkErr)
	}

	indexedAt := time.Now().Unix()
	totalSymbols, totalEdges, totalFiles := 0, 0, 0

	for _, ar := range analyzeResults {
		if mcpMetrics != nil {
			mcpMetrics.RecordParseResult(ar.IndexStatus, ar.StatusReason, len(ar.FileResult.Symbols))
		}
		result := ar.FileResult
		if err := sfRepo.Upsert(ctx, codebaseID, store.SourceFileData{
			FilePath: result.FilePath, Language: result.Language,
			PackageName: result.PackageName, LOC: result.LOC,
			SymbolCount: len(result.Symbols), FileHash: result.FileHash,
			IndexedAt: indexedAt,
		}); err != nil {
			recordWriteError(err, "upsert source_file "+result.FilePath)
			continue
		}

		// Store index_status and status_reason in indexed_files
		if err := indexedFileRepo.UpsertWithStatus(ctx, codebaseID, result.FilePath, result.FileHash, int64(len(result.Symbols)), indexedAt, ar.IndexStatus, ar.StatusReason); err != nil {
			recordWriteError(err, "upsert indexed_file "+result.FilePath)
			continue
		}

		totalFiles++

		for _, sym := range result.Symbols {
			sd := store.SymbolData{
				FilePath: sym.FilePath, Language: sym.Language, Kind: sym.Kind,
				Name: sym.Name, QualifiedName: sym.QualifiedName, Receiver: sym.Receiver,
				Signature: sym.Signature, DocComment: sym.DocComment,
				Visibility: sym.Visibility, BodySnippet: sym.BodySnippet,
				StartLine: sym.StartLine, EndLine: sym.EndLine,
				FileHash: sym.FileHash, IndexedAt: indexedAt,
			}
			if err := symbolRepo.Create(ctx, codebaseID, sd); err != nil {
				recordWriteError(err, "create symbol "+sym.QualifiedName)
				continue
			}
			totalSymbols++
		}

		for _, edge := range result.Edges {
			if err := edgeRepo.Create(ctx, codebaseID, store.EdgeData{
				FromKind: edge.FromKind, FromRef: edge.FromRef,
				ToKind: edge.ToKind, ToRef: edge.ToRef,
				EdgeKind: edge.EdgeKind, Line: edge.Line, Resolved: edge.Resolved,
			}); err != nil {
				recordWriteError(err, "create edge "+edge.EdgeKind+" "+edge.FromRef+" -> "+edge.ToRef)
				continue
			}
			totalEdges++
		}
	}

	recordWriteError(mcpResolveCrossRepoLinks(ctx, conn, codebaseID), "resolve cross-repo links")

	result := map[string]any{
		"codebase_id":       codebaseID,
		"files_analyzed":    totalFiles,
		"symbols_extracted": totalSymbols,
		"edges_extracted":   totalEdges,
		"indexed_at":        indexedAt,
		"incremental":       false,
	}
	appendWriteErrorSummary(result)
	if mcpMetrics != nil {
		mcpMetrics.RecordGraphUpdate(int64(totalSymbols), int64(totalEdges))
	}

	return mcpToolTextResult(result), nil
}

func mcpResolveCrossRepoLinks(ctx context.Context, conn *sql.DB, codebaseID int64) error {
	wsRepo := store.NewWorkspaceRepo(conn)
	edgeRepo := store.NewEdgeRepo(conn)
	symbolRepo := store.NewSymbolRepo(conn)

	workspaces, err := wsRepo.GetWorkspacesForCodebase(ctx, codebaseID)
	if err != nil || len(workspaces) == 0 {
		return err
	}

	unresolvedImports, err := edgeRepo.GetUnresolvedImports(ctx, codebaseID)
	if err != nil || len(unresolvedImports) == 0 {
		return err
	}

	for _, ws := range workspaces {
		memberIDs, err := wsRepo.GetMemberIDs(ctx, ws.ID)
		if err != nil {
			continue
		}

		otherIDs := make([]int64, 0, len(memberIDs))
		for _, id := range memberIDs {
			if id != codebaseID {
				otherIDs = append(otherIDs, id)
			}
		}
		if len(otherIDs) == 0 {
			continue
		}

		for _, edge := range unresolvedImports {
			symbols, err := symbolRepo.FindByNameMulti(ctx, otherIDs, edge.ToRef)
			if err != nil || len(symbols) == 0 {
				continue
			}

			for _, sym := range symbols {
				if sym.QualifiedName == edge.ToRef || sym.Name == edge.ToRef {
					_ = edgeRepo.ResolveCrossRepoEdge(ctx, edge.ID, sym.CodebaseID)
					break
				}
			}
		}
	}

	return nil
}

func resolveScopedCodebaseIDs(ctx context.Context, conn *sql.DB, codebaseID, workspaceID int64) ([]int64, error) {
	if codebaseID <= 0 && workspaceID <= 0 {
		return nil, errors.New("at least one of codebase_id or workspace_id is required")
	}

	codebaseIDs := make([]int64, 0, 8)
	seen := make(map[int64]struct{}, 8)
	add := func(id int64) {
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		codebaseIDs = append(codebaseIDs, id)
	}

	add(codebaseID)

	if workspaceID > 0 {
		wsRepo := store.NewWorkspaceRepo(conn)
		memberIDs, err := wsRepo.GetMemberIDs(ctx, workspaceID)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace members: %w", err)
		}
		if len(memberIDs) == 0 {
			return nil, errors.New("workspace has no member codebases")
		}
		for _, mid := range memberIDs {
			add(mid)
		}
	}

	return codebaseIDs, nil
}

func mcpFindSymbol(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	codebaseID := toInt64(args["codebase_id"], 0)
	workspaceID := toInt64(args["workspace_id"], 0)
	name := strings.TrimSpace(toString(args["name"]))
	kind := strings.TrimSpace(toString(args["kind"]))
	if name == "" {
		return nil, errors.New("name is required")
	}
	if codebaseID <= 0 && workspaceID <= 0 {
		return nil, errors.New("either codebase_id or workspace_id is required")
	}

	repo := store.NewSymbolRepo(conn)

	// Workspace-scoped query: resolve member IDs and use Multi method.
	if workspaceID > 0 {
		wsRepo := store.NewWorkspaceRepo(conn)
		members, err := wsRepo.GetMembers(ctx, workspaceID)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace members: %w", err)
		}
		if len(members) == 0 {
			return mcpToolTextResult(map[string]any{"symbols": []any{}, "count": 0}), nil
		}

		// Build codebase ID list and name lookup map.
		codebaseIDs := make([]int64, len(members))
		nameMap := make(map[int64]string, len(members))
		for i, m := range members {
			codebaseIDs[i] = m.ID
			nameMap[m.ID] = m.Name
		}

		symbols, err := repo.FindByNameMulti(ctx, codebaseIDs, name)
		if err != nil {
			return nil, err
		}

		// Filter by kind if provided.
		if kind != "" {
			filtered := symbols[:0]
			for _, s := range symbols {
				if s.Kind == kind {
					filtered = append(filtered, s)
				}
			}
			symbols = filtered
		}

		// Build results with codebase_id and codebase_name.
		results := make([]map[string]any, 0, len(symbols))
		for _, s := range symbols {
			item := map[string]any{
				"id":             s.ID,
				"codebase_id":    s.CodebaseID,
				"codebase_name":  nameMap[s.CodebaseID],
				"file_path":      s.FilePath,
				"language":       s.Language,
				"kind":           s.Kind,
				"name":           s.Name,
				"qualified_name": s.QualifiedName,
				"signature":      s.Signature,
				"start_line":     s.StartLine,
				"end_line":       s.EndLine,
			}
			if s.Receiver != "" {
				item["receiver"] = s.Receiver
			}
			if s.DocComment != "" {
				item["doc_comment"] = s.DocComment
			}
			results = append(results, compactMCPSearchHit(item))
		}

		// Annotate results from degraded files per codebase.
		for _, cbID := range codebaseIDs {
			annotateDegradedHits(ctx, conn, cbID, results)
		}

		return mcpToolTextResult(map[string]any{"symbols": results, "count": len(results), "workspace_id": workspaceID}), nil
	}

	// Single-codebase query (original behavior).
	symbols, err := repo.FindByName(ctx, codebaseID, name)
	if err != nil {
		return nil, err
	}

	// Filter by kind if provided
	if kind != "" {
		filtered := symbols[:0]
		for _, s := range symbols {
			if s.Kind == kind {
				filtered = append(filtered, s)
			}
		}
		symbols = filtered
	}

	// Convert to maps and annotate degraded results.
	results := make([]map[string]any, 0, len(symbols))
	for _, s := range symbols {
		item := map[string]any{
			"id":             s.ID,
			"codebase_id":    s.CodebaseID,
			"file_path":      s.FilePath,
			"language":       s.Language,
			"kind":           s.Kind,
			"name":           s.Name,
			"qualified_name": s.QualifiedName,
			"signature":      s.Signature,
			"start_line":     s.StartLine,
			"end_line":       s.EndLine,
		}
		if s.Receiver != "" {
			item["receiver"] = s.Receiver
		}
		if s.DocComment != "" {
			item["doc_comment"] = s.DocComment
		}
		results = append(results, compactMCPSearchHit(item))
	}
	annotateDegradedHits(ctx, conn, codebaseID, results)

	return mcpToolTextResult(map[string]any{"symbols": results, "count": len(results)}), nil
}

func mcpFindUsages(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	codebaseID := toInt64(args["codebase_id"], 0)
	workspaceID := toInt64(args["workspace_id"], 0)
	name := strings.TrimSpace(toString(args["name"]))
	if name == "" {
		return nil, errors.New("name is required")
	}
	if codebaseID <= 0 && workspaceID <= 0 {
		return nil, errors.New("either codebase_id or workspace_id is required")
	}

	repo := store.NewEdgeRepo(conn)

	// Workspace-scoped query: resolve member IDs and use Multi method.
	if workspaceID > 0 {
		wsRepo := store.NewWorkspaceRepo(conn)
		members, err := wsRepo.GetMembers(ctx, workspaceID)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace members: %w", err)
		}
		if len(members) == 0 {
			return mcpToolTextResult(map[string]any{"usages": []any{}, "count": 0}), nil
		}

		// Build codebase ID list and name lookup map.
		codebaseIDs := make([]int64, len(members))
		nameMap := make(map[int64]string, len(members))
		for i, m := range members {
			codebaseIDs[i] = m.ID
			nameMap[m.ID] = m.Name
		}

		edges, err := repo.FindUsagesMulti(ctx, codebaseIDs, name)
		if err != nil {
			return nil, err
		}

		// Build results with codebase_id and codebase_name.
		results := make([]map[string]any, 0, len(edges))
		for _, e := range edges {
			results = append(results, map[string]any{
				"id":            e.ID,
				"codebase_id":   e.CodebaseID,
				"codebase_name": nameMap[e.CodebaseID],
				"from_kind":     e.FromKind,
				"from_ref":      e.FromRef,
				"to_kind":       e.ToKind,
				"to_ref":        e.ToRef,
				"edge_kind":     e.EdgeKind,
				"line":          e.Line,
				"resolved":      e.Resolved,
			})
		}

		return mcpToolTextResult(map[string]any{"usages": results, "count": len(results), "workspace_id": workspaceID}), nil
	}

	// Single-codebase query (original behavior).
	edges, err := repo.FindUsages(ctx, codebaseID, name)
	if err != nil {
		return nil, err
	}

	return mcpToolTextResult(map[string]any{"usages": edges, "count": len(edges)}), nil
}

func mcpGetFileSymbols(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	codebaseID := toInt64(args["codebase_id"], 0)
	filePath := strings.TrimSpace(toString(args["file_path"]))
	if codebaseID <= 0 || filePath == "" {
		return nil, errors.New("codebase_id and file_path are required")
	}

	repo := store.NewSymbolRepo(conn)
	symbols, err := repo.GetByFile(ctx, codebaseID, filePath)
	if err != nil {
		return nil, err
	}

	compacted := make([]map[string]any, 0, len(symbols))
	for _, s := range symbols {
		item := map[string]any{
			"name":           s.Name,
			"qualified_name": s.QualifiedName,
			"kind":           s.Kind,
			"language":       s.Language,
			"signature":      s.Signature,
			"start_line":     s.StartLine,
			"end_line":       s.EndLine,
		}
		if s.Receiver != "" {
			item["receiver"] = s.Receiver
		}
		if s.DocComment != "" {
			item["doc_comment"] = s.DocComment
		}
		if s.Visibility != "" {
			item["visibility"] = s.Visibility
		}
		compacted = append(compacted, compactMCPSearchHit(item))
	}

	result := map[string]any{"file_path": filePath, "symbols": compacted, "count": len(compacted)}

	// Check if this file has degraded index status.
	indexedFileRepo := store.NewIndexedFileRepo(conn)
	degraded, degErr := indexedFileRepo.GetDegradedFiles(ctx, codebaseID, []string{filePath})
	if degErr == nil {
		if info, found := degraded[filePath]; found {
			result["degraded"] = true
			result["degradation_reason"] = info.IndexStatus + ": " + info.StatusReason
		}
	}

	return mcpToolTextResult(result), nil
}

func mcpGetCallers(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	codebaseID := toInt64(args["codebase_id"], 0)
	workspaceID := toInt64(args["workspace_id"], 0)
	name := strings.TrimSpace(toString(args["name"]))
	if name == "" {
		return nil, errors.New("name is required")
	}
	if codebaseID <= 0 && workspaceID <= 0 {
		return nil, errors.New("either codebase_id or workspace_id is required")
	}

	repo := store.NewEdgeRepo(conn)

	// Workspace-scoped query: resolve member IDs and use Multi method.
	if workspaceID > 0 {
		wsRepo := store.NewWorkspaceRepo(conn)
		members, err := wsRepo.GetMembers(ctx, workspaceID)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace members: %w", err)
		}
		if len(members) == 0 {
			return mcpToolTextResult(map[string]any{"target": name, "callers": []any{}, "count": 0}), nil
		}

		// Build codebase ID list and name lookup map.
		codebaseIDs := make([]int64, len(members))
		nameMap := make(map[int64]string, len(members))
		for i, m := range members {
			codebaseIDs[i] = m.ID
			nameMap[m.ID] = m.Name
		}

		edges, err := repo.GetCallersMulti(ctx, codebaseIDs, name)
		if err != nil {
			return nil, err
		}

		// Build results with codebase_id and codebase_name.
		results := make([]map[string]any, 0, len(edges))
		for _, e := range edges {
			results = append(results, map[string]any{
				"id":            e.ID,
				"codebase_id":   e.CodebaseID,
				"codebase_name": nameMap[e.CodebaseID],
				"from_kind":     e.FromKind,
				"from_ref":      e.FromRef,
				"to_kind":       e.ToKind,
				"to_ref":        e.ToRef,
				"edge_kind":     e.EdgeKind,
				"line":          e.Line,
				"resolved":      e.Resolved,
			})
		}

		return mcpToolTextResult(map[string]any{"target": name, "callers": results, "count": len(results), "workspace_id": workspaceID}), nil
	}

	// Single-codebase query (original behavior).
	edges, err := repo.GetCallers(ctx, codebaseID, name)
	if err != nil {
		return nil, err
	}

	return mcpToolTextResult(map[string]any{"target": name, "callers": edges, "count": len(edges)}), nil
}

func mcpGetCallees(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	codebaseID := toInt64(args["codebase_id"], 0)
	qualifiedName := strings.TrimSpace(toString(args["qualified_name"]))
	if codebaseID <= 0 || qualifiedName == "" {
		return nil, errors.New("codebase_id and qualified_name are required")
	}

	repo := store.NewEdgeRepo(conn)
	edges, err := repo.GetCallees(ctx, codebaseID, qualifiedName)
	if err != nil {
		return nil, err
	}

	return mcpToolTextResult(map[string]any{"from": qualifiedName, "callees": edges, "count": len(edges)}), nil
}

func mcpGetImports(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	codebaseID := toInt64(args["codebase_id"], 0)
	filePath := strings.TrimSpace(toString(args["file_path"]))
	if codebaseID <= 0 || filePath == "" {
		return nil, errors.New("codebase_id and file_path are required")
	}

	repo := store.NewEdgeRepo(conn)
	edges, err := repo.GetImports(ctx, codebaseID, filePath)
	if err != nil {
		return nil, err
	}

	imports := make([]string, 0, len(edges))
	for _, e := range edges {
		imports = append(imports, e.ToRef)
	}

	return mcpToolTextResult(map[string]any{"file_path": filePath, "imports": imports, "count": len(imports)}), nil
}

func mcpProjectOverview(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	codebaseID := toInt64(args["codebase_id"], 0)
	if codebaseID <= 0 {
		return nil, errors.New("codebase_id is required")
	}

	sfRepo := store.NewSourceFileRepo(conn)
	symRepo := store.NewSymbolRepo(conn)

	fileStats, err := sfRepo.Stats(ctx, codebaseID)
	if err != nil {
		return nil, fmt.Errorf("file stats: %w", err)
	}

	symbolStats, err := symRepo.Stats(ctx, codebaseID)
	if err != nil {
		return nil, fmt.Errorf("symbol stats: %w", err)
	}

	packages, err := sfRepo.PackageList(ctx, codebaseID)
	if err != nil {
		return nil, fmt.Errorf("package list: %w", err)
	}

	topFiles, err := symRepo.TopFilesBySymbolCount(ctx, codebaseID, 10)
	if err != nil {
		return nil, fmt.Errorf("top files: %w", err)
	}

	entryPoints, err := symRepo.ExportedFuncs(ctx, codebaseID, 20)
	if err != nil {
		return nil, fmt.Errorf("entry points: %w", err)
	}

	// Summarize entry points (signature + location only)
	epSummary := make([]map[string]any, 0, len(entryPoints))
	for _, ep := range entryPoints {
		epSummary = append(epSummary, compactMCPSearchHit(map[string]any{
			"name":        ep.QualifiedName,
			"kind":        ep.Kind,
			"signature":   ep.Signature,
			"file_path":   ep.FilePath,
			"start_line":  ep.StartLine,
			"doc_comment": ep.DocComment,
		}))
	}

	return mcpToolTextResult(map[string]any{
		"codebase_id":    codebaseID,
		"files":          fileStats,
		"symbols":        symbolStats,
		"packages":       packages,
		"top_files":      topFiles,
		"exported_funcs": epSummary,
	}), nil
}

// mcpResolveCodeQuery orchestrates four sub-queries in parallel (find_symbol,
// get_callers, get_callees, find_usages) and merges results
// into a single structured response. Individual sub-query failures are captured
// as error annotations rather than failing the entire call.
func mcpResolveCodeQuery(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	query := strings.TrimSpace(toString(args["query"]))
	if query == "" {
		return nil, errors.New("query is required")
	}

	codebaseID := toInt64(args["codebase_id"], 0)
	if codebaseID <= 0 {
		return nil, errors.New("codebase_id is required")
	}

	workspaceID := toInt64(args["workspace_id"], 0)

	// Prepare response sections.
	var (
		matchedSymbols any
		callers        any
		callees        any
		usages         any
	)

	errMap := make(map[string]string)
	var mu sync.Mutex

	// Build sub-query argument maps.
	findSymbolArgs := map[string]any{"name": query, "codebase_id": codebaseID}
	findUsagesArgs := map[string]any{"name": query, "codebase_id": codebaseID}
	getCallersArgs := map[string]any{"name": query, "codebase_id": codebaseID}

	if workspaceID > 0 {
		findSymbolArgs["workspace_id"] = workspaceID
		findUsagesArgs["workspace_id"] = workspaceID
		getCallersArgs["workspace_id"] = workspaceID
	}

	var wg sync.WaitGroup
	wg.Add(4)

	// 1. find_symbol
	go func() {
		defer wg.Done()
		result, err := mcpFindSymbol(ctx, conn, findSymbolArgs)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			errMap["find_symbol"] = err.Error()
			matchedSymbols = []any{}
		} else {
			matchedSymbols = extractStructuredField(result, "symbols", []any{})
		}
	}()

	// 2. get_callers
	go func() {
		defer wg.Done()
		result, err := mcpGetCallers(ctx, conn, getCallersArgs)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			errMap["callers"] = err.Error()
			callers = []any{}
		} else {
			callers = extractStructuredField(result, "callers", []any{})
		}
	}()

	// 3. get_callees — requires codebase_id and qualified_name; use query as qualified_name
	go func() {
		defer wg.Done()
		calleesArgs := map[string]any{"codebase_id": codebaseID, "qualified_name": query}
		result, err := mcpGetCallees(ctx, conn, calleesArgs)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			errMap["callees"] = err.Error()
			callees = []any{}
		} else {
			callees = extractStructuredField(result, "callees", []any{})
		}
	}()

	// 4. find_usages
	go func() {
		defer wg.Done()
		result, err := mcpFindUsages(ctx, conn, findUsagesArgs)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			errMap["usages"] = err.Error()
			usages = []any{}
		} else {
			usages = extractStructuredField(result, "usages", []any{})
		}
	}()

	wg.Wait()

	response := map[string]any{
		"matched_symbols": matchedSymbols,
		"callers":         callers,
		"callees":         callees,
		"usages":          usages,
	}

	if len(errMap) > 0 {
		response["errors"] = errMap
	}

	return mcpToolTextResult(response), nil
}

// mcpLocateIssueImpactArea handles the locate_issue_impact_area MCP tool call.
// It performs hybrid search (FTS5 + optional semantic) to rank symbols by issue
// relevance with blast radius and confidence scores.
func mcpLocateIssueImpactArea(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	issueText := toString(args["issue_text"])
	codebaseID := toInt64(args["codebase_id"], 0)
	workspaceID := toInt64(args["workspace_id"], 0)
	limit := toInt(args["limit"], 10)

	response, err := locateIssueImpactArea(ctx, conn, issueText, codebaseID, workspaceID, limit, mcpLogger)
	if err != nil {
		return nil, err
	}

	return mcpToolTextResult(response), nil
}

// mcpCodebaseContext retrieves orientation documents (README, design docs, agent instructions)
// for session bootstrapping. No search query needed — uses file path pattern matching.
func mcpCodebaseContext(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	codebaseID := toInt64(args["codebase_id"], 0)
	workspaceID := toInt64(args["workspace_id"], 0)

	codebaseIDs, err := resolveScopedCodebaseIDs(ctx, conn, codebaseID, workspaceID)
	if err != nil {
		return nil, err
	}

	// Load orient config from the first codebase's root path.
	// If the codebase root can't be resolved, use defaults.
	catalogRepo := store.NewCatalogRepo(conn)
	orientCfg := orient.DefaultConfig()
	if len(codebaseIDs) > 0 {
		cb, err := catalogRepo.GetByID(ctx, codebaseIDs[0])
		if err == nil && cb.RootPath != "" {
			loaded, loadErr := orient.Load(cb.RootPath, mcpLogger)
			if loadErr == nil {
				orientCfg = loaded
			}
		}
	}

	// Call Retrieve to get orientation documents.
	retrieveCfg := orient.RetrieveConfig{
		CodebaseIDs: codebaseIDs,
		Config:      orientCfg,
	}

	docs, err := orient.Retrieve(ctx, conn, retrieveCfg)
	if err != nil {
		return nil, fmt.Errorf("retrieve orientation docs: %w", err)
	}

	// Proprietary-artifact mode: when the codebase is marked closed_source, suppress
	// doc types that can reveal internal design (architecture, design, todo, feature-list).
	// Only safe navigational/usage docs (readme, contributing, agent-instructions) are kept.
	if len(codebaseIDs) > 0 {
		if policy, ok := loadCodebasePolicyMetadata(ctx, conn, codebaseIDs[0]); ok {
			if closedSource, _ := policy["closed_source"].(bool); closedSource {
				safeTypes := map[orient.DocType]bool{
					orient.DocTypeReadme:            true,
					orient.DocTypeContributing:      true,
					orient.DocTypeAgentInstructions: true,
				}
				filtered := docs[:0]
				for _, doc := range docs {
					if safeTypes[doc.DocType] {
						filtered = append(filtered, doc)
					}
				}
				docs = filtered
			}
		}
	}

	// Fallback: when no orientation docs found, call mcpProjectOverview and wrap with "fallback": true.
	if len(docs) == 0 {
		// Use the first codebase_id for the project overview fallback.
		fallbackID := codebaseIDs[0]
		overviewResult, overviewErr := mcpProjectOverview(ctx, conn, map[string]any{"codebase_id": fallbackID})
		if overviewErr != nil {
			return nil, fmt.Errorf("fallback project overview: %w", overviewErr)
		}

		// Extract the structured content and wrap with fallback flag.
		response := map[string]any{
			"fallback":    true,
			"codebase_id": fallbackID,
		}
		if sc, ok := overviewResult["structuredContent"]; ok {
			response["overview"] = sc
		}
		return mcpToolTextResult(response), nil
	}

	// Build response from orientation docs.
	docResults := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		docResults = append(docResults, map[string]any{
			"file_path": doc.FilePath,
			"content":   compactMCPText(doc.Content, mcpDocContentLimit),
			"doc_type":  string(doc.DocType),
		})
	}

	response := map[string]any{
		"documents": docResults,
		"count":     len(docResults),
	}

	return mcpToolTextResult(response), nil
}

// extractStructuredField extracts a field from an MCP tool result's structuredContent.
// Returns fallback if the field is not found or the result structure is unexpected.
func extractStructuredField(result map[string]any, field string, fallback any) any {
	if result == nil {
		return fallback
	}
	sc, ok := result["structuredContent"]
	if !ok {
		return fallback
	}
	scMap, ok := sc.(map[string]any)
	if !ok {
		return fallback
	}
	val, ok := scMap[field]
	if !ok {
		return fallback
	}
	return val
}

func mcpServerStats(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	// Get current stats from the metrics collector.
	stats := mcpMetrics.Stats()

	// Query active codebase count from the database.
	var codebaseCount int
	err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM codebases").Scan(&codebaseCount)
	if err != nil {
		return nil, fmt.Errorf("query active codebases: %w", err)
	}
	stats.ActiveCodebases = codebaseCount

	// If reset is requested, reset counters after capturing stats.
	if toBool(args["reset"], false) {
		mcpMetrics.Reset()
	}

	// Marshal the stats struct to a generic map for the response.
	encoded, err := json.Marshal(stats)
	if err != nil {
		return nil, fmt.Errorf("marshal stats: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("unmarshal stats: %w", err)
	}

	return mcpToolTextResult(result), nil
}

func trimChunks(in []store.Chunk, limit int) []store.Chunk {
	if limit <= 0 || limit >= len(in) {
		return in
	}
	return in[:limit]
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return ""
	}
}

func toInt(v any, fallback int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(t))
		if err == nil {
			return i
		}
	}
	return fallback
}

func toInt64(v any, fallback int64) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int:
		return int64(t)
	case int64:
		return t
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		if err == nil {
			return i
		}
	}
	return fallback
}

func toBool(v any, fallback bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(t))
		if err == nil {
			return b
		}
	}
	return fallback
}

func readMCPPayload(r *bufio.Reader) ([]byte, error) {
	// MCP stdio transport uses newline-delimited JSON. Read one complete JSON
	// object per message, ignoring blank lines.
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && len(strings.TrimSpace(line)) > 0 {
				// Partial last line with no trailing newline — try to parse it.
				return []byte(line), nil
			}
			return nil, err
		}
		trimmed := strings.TrimSpace(line)
		if len(trimmed) == 0 {
			continue // skip blank lines
		}
		return []byte(trimmed), nil
	}
}

func mcpCompareCapabilities(ctx context.Context, conn *sql.DB, args map[string]any) (map[string]any, error) {
	aID := toInt64(args["codebase_a_id"], 0)
	bID := toInt64(args["codebase_b_id"], 0)
	if aID <= 0 || bID <= 0 {
		return nil, errors.New("codebase_a_id and codebase_b_id are required")
	}
	if aID == bID {
		return nil, errors.New("codebase_a_id and codebase_b_id must be different")
	}

	repo := store.NewSymbolRepo(conn)
	aSymbols, err := repo.ListCapabilities(ctx, aID)
	if err != nil {
		return nil, fmt.Errorf("load codebase_a symbols: %w", err)
	}
	bSymbols, err := repo.ListCapabilities(ctx, bID)
	if err != nil {
		return nil, fmt.Errorf("load codebase_b symbols: %w", err)
	}

	// capKey uniquely identifies a symbol across codebases.
	type capKey struct{ name, kind string }

	// Group symbols by domain (first path segment).
	domain := func(filePath string) string {
		if i := strings.Index(filePath, "/"); i > 0 {
			return filePath[:i+1]
		}
		return "(root)"
	}

	type domainEntry struct {
		a map[capKey]struct{}
		b map[capKey]struct{}
	}
	domains := map[string]*domainEntry{}

	ensure := func(d string) *domainEntry {
		if domains[d] == nil {
			domains[d] = &domainEntry{
				a: map[capKey]struct{}{},
				b: map[capKey]struct{}{},
			}
		}
		return domains[d]
	}

	for _, s := range aSymbols {
		e := ensure(domain(s.FilePath))
		e.a[capKey{s.Name, s.Kind}] = struct{}{}
	}
	for _, s := range bSymbols {
		e := ensure(domain(s.FilePath))
		e.b[capKey{s.Name, s.Kind}] = struct{}{}
	}

	type symbolRef struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	}

	type domainResult struct {
		Domain   string      `json:"domain"`
		Status   string      `json:"status"`
		ACount   int         `json:"codebase_a_symbols"`
		BCount   int         `json:"codebase_b_symbols"`
		InBoth   int         `json:"in_both"`
		OnlyInA  []symbolRef `json:"only_in_a"`
		OnlyInB  []symbolRef `json:"only_in_b"`
	}

	summary := map[string]int{"implemented": 0, "partial": 0, "missing": 0, "extra": 0}

	// Collect and sort domain names for stable output.
	domainNames := make([]string, 0, len(domains))
	for d := range domains {
		domainNames = append(domainNames, d)
	}
	sort.Strings(domainNames)

	results := make([]domainResult, 0, len(domainNames))
	for _, d := range domainNames {
		e := domains[d]

		var onlyInA, onlyInB []symbolRef
		inBoth := 0

		for k := range e.a {
			if _, ok := e.b[k]; ok {
				inBoth++
			} else {
				onlyInA = append(onlyInA, symbolRef{k.name, k.kind})
			}
		}
		for k := range e.b {
			if _, ok := e.a[k]; !ok {
				onlyInB = append(onlyInB, symbolRef{k.name, k.kind})
			}
		}

		var status string
		switch {
		case len(e.a) == 0:
			status = "extra" // domain only in B
		case len(e.b) == 0:
			status = "missing" // domain only in A
		case inBoth*10 >= len(e.a)*7: // B has >= 70% of A's symbols
			status = "implemented"
		default:
			status = "partial"
		}
		summary[status]++

		results = append(results, domainResult{
			Domain:  d,
			Status:  status,
			ACount:  len(e.a),
			BCount:  len(e.b),
			InBoth:  inBoth,
			OnlyInA: onlyInA,
			OnlyInB: onlyInB,
		})
	}

	out := map[string]any{
		"codebase_a_id": aID,
		"codebase_b_id": bID,
		"domains":       results,
		"summary":       summary,
	}
	return mcpToolTextResult(out), nil
}

func writeMCPResponse(w io.Writer, resp mcpResponse) error {
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	// MCP stdio transport uses newline-delimited JSON (not LSP Content-Length framing).
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}
