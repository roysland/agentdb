package parse

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// PluginProcess wraps a parser plugin subprocess, implementing the Parser interface
// via JSON-RPC over stdin/stdout.
type PluginProcess struct {
	manifest   PluginManifest
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     *bufio.Reader
	languages  []string
	extensions []string
	mu         sync.Mutex
	nextID     int
}

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      *int        `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is a JSON-RPC 2.0 error object.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// capabilitiesResult is the response from the capabilities handshake.
type capabilitiesResult struct {
	Languages  []string `json:"languages"`
	Extensions []string `json:"extensions"`
}

// parseParams is the request payload for the parse method.
type parseParams struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

// StartPlugin launches a plugin subprocess and performs the capabilities handshake.
func StartPlugin(manifest PluginManifest, dir string) (*PluginProcess, error) {
	binaryPath := manifest.Binary
	if !filepath.IsAbs(binaryPath) {
		binaryPath = filepath.Join(dir, binaryPath)
	}

	cmd := exec.Command(binaryPath)
	cmd.Dir = dir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin %s: create stdin pipe: %w", manifest.Name, err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("plugin %s: create stdout pipe: %w", manifest.Name, err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("plugin %s: start subprocess: %w", manifest.Name, err)
	}

	p := &PluginProcess{
		manifest: manifest,
		cmd:      cmd,
		stdin:    stdin,
		stdout:   bufio.NewReader(stdout),
		nextID:   1,
	}

	// Perform capabilities handshake
	if err := p.handshake(); err != nil {
		// Kill the process on handshake failure
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("plugin %s: capabilities handshake: %w", manifest.Name, err)
	}

	return p, nil
}

// handshake sends the capabilities request and populates languages/extensions.
func (p *PluginProcess) handshake() error {
	resp, err := p.call("capabilities", struct{}{})
	if err != nil {
		return err
	}

	var caps capabilitiesResult
	if err := json.Unmarshal(resp, &caps); err != nil {
		return fmt.Errorf("decode capabilities response: %w", err)
	}

	if len(caps.Languages) == 0 {
		return fmt.Errorf("plugin reported no languages")
	}

	p.languages = caps.Languages
	p.extensions = caps.Extensions
	return nil
}

// Language returns the primary language supported by this plugin.
func (p *PluginProcess) Language() string {
	if len(p.languages) > 0 {
		return p.languages[0]
	}
	return ""
}

// CanParse returns true if the file extension matches one of the plugin's declared extensions.
func (p *PluginProcess) CanParse(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	for _, e := range p.extensions {
		if strings.ToLower(e) == ext {
			return true
		}
	}
	return false
}

// Parse sends a parse request to the plugin subprocess and returns the result.
// It enforces a 30-second timeout; if exceeded, the subprocess is killed.
func (p *PluginProcess) Parse(filePath string, content []byte) (FileResult, error) {
	params := parseParams{
		FilePath: filePath,
		Content:  base64.StdEncoding.EncodeToString(content),
	}

	type callResult struct {
		data json.RawMessage
		err  error
	}

	ch := make(chan callResult, 1)
	go func() {
		resp, err := p.call("parse", params)
		ch <- callResult{data: resp, err: err}
	}()

	select {
	case result := <-ch:
		if result.err != nil {
			return FileResult{}, fmt.Errorf("plugin %s: parse %s: %w", p.manifest.Name, filePath, result.err)
		}
		var fr FileResult
		if err := json.Unmarshal(result.data, &fr); err != nil {
			return FileResult{}, fmt.Errorf("plugin %s: decode parse response for %s: %w", p.manifest.Name, filePath, err)
		}
		return fr, nil

	case <-time.After(30 * time.Second):
		// Kill subprocess on timeout
		_ = p.cmd.Process.Kill()
		return FileResult{}, fmt.Errorf("plugin %s: parse %s: timeout after 30 seconds", p.manifest.Name, filePath)
	}
}

// Shutdown sends a shutdown notification to the plugin subprocess.
// This is a notification (no id field), so no response is expected.
func (p *PluginProcess) Shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "shutdown",
		Params:  struct{}{},
	}

	data, err := json.Marshal(req)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = p.stdin.Write(data)

	// Close stdin to signal EOF and wait for process to exit
	_ = p.stdin.Close()
	_ = p.cmd.Wait()
}

// call sends a JSON-RPC request and reads the response. It is not safe for
// concurrent use — callers must serialize access via the mutex.
func (p *PluginProcess) call(method string, params interface{}) (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	id := p.nextID
	p.nextID++

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := p.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Read one line of response (newline-delimited JSON)
	line, err := p.stdout.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}
