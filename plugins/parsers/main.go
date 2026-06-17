//go:build treesitter

// agentdb-parsers is a parser plugin for agentdb that provides Python, TypeScript,
// TSX, JavaScript, and Rust language support via the agentdb plugin protocol
// (JSON-RPC 2.0 over stdin/stdout).
//
// Install:
//
//	go install -tags treesitter github.com/roysland/agentdb/plugins/parsers@latest
//
// Then place the binary at ~/.agentdb/plugins/parsers/agentdb-parsers/ alongside
// a manifest.json (see agentdb plugin documentation).
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/roysland/agentdb/internal/parse"
)

func main() {
	parsers := []parse.Parser{
		parse.NewPythonParser(),
		parse.NewTypeScriptParser(),
		parse.NewTSXParser(),
		parse.NewJavaScriptParser(),
		parse.NewRustParser(),
	}

	scanner := bufio.NewScanner(os.Stdin)
	// Increase buffer: base64-encoded source files can be large.
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			writeError(req.ID, -32700, "parse error: "+err.Error())
			continue
		}

		switch req.Method {
		case "capabilities":
			writeResult(req.ID, buildCapabilities(parsers))

		case "parse":
			var params struct {
				FilePath string `json:"file_path"`
				Content  string `json:"content"` // base64-encoded
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				writeError(req.ID, -32602, "invalid params: "+err.Error())
				continue
			}
			content, err := base64.StdEncoding.DecodeString(params.Content)
			if err != nil {
				writeError(req.ID, -32602, "base64 decode: "+err.Error())
				continue
			}
			p := findParser(params.FilePath, parsers)
			if p == nil {
				writeError(req.ID, -32602, fmt.Sprintf("no parser for %s", params.FilePath))
				continue
			}
			result, err := p.Parse(params.FilePath, content)
			if err != nil {
				writeError(req.ID, -32603, "parse: "+err.Error())
				continue
			}
			writeResult(req.ID, result)

		case "shutdown":
			os.Exit(0)

		default:
			if req.ID != nil {
				writeError(req.ID, -32601, "method not found: "+req.Method)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "agentdb-parsers: stdin read error: %v\n", err)
		os.Exit(1)
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      *int      `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

func writeResult(id *int, result any) {
	emit(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeError(id *int, code int, msg string) {
	emit(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func emit(resp rpcResponse) {
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	os.Stdout.Write(data)
}

type capabilitiesResult struct {
	Languages  []string `json:"languages"`
	Extensions []string `json:"extensions"`
}

func buildCapabilities(parsers []parse.Parser) capabilitiesResult {
	langSet := make(map[string]struct{})
	extSet := make(map[string]struct{})

	knownExts := []string{".py", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".rs"}
	for _, p := range parsers {
		langSet[p.Language()] = struct{}{}
		for _, ext := range knownExts {
			if p.CanParse("file" + ext) {
				extSet[ext] = struct{}{}
			}
		}
	}

	langs := make([]string, 0, len(langSet))
	for l := range langSet {
		langs = append(langs, l)
	}
	exts := make([]string, 0, len(extSet))
	for e := range extSet {
		exts = append(exts, e)
	}
	sort.Strings(langs)
	sort.Strings(exts)

	return capabilitiesResult{Languages: langs, Extensions: exts}
}

func findParser(filePath string, parsers []parse.Parser) parse.Parser {
	for _, p := range parsers {
		if p.CanParse(filePath) {
			return p
		}
	}
	return nil
}
