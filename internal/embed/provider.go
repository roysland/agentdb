package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/roysland/agentdb/internal/config"
)

var ErrProviderNotConfigured = errors.New("embedding provider is not configured")

type Provider interface {
	Embed(ctx context.Context, input string) ([]float32, error)
	ModelName() string
}

type CachedProvider struct {
	inner Provider

	mu         sync.RWMutex
	cache      map[string][]float32
	order      []string
	maxEntries int
}

const defaultCachedProviderMaxEntries = 2048

func NewCachedProvider(inner Provider) Provider {
	return NewCachedProviderWithLimit(inner, defaultCachedProviderMaxEntries)
}

func NewCachedProviderWithLimit(inner Provider, maxEntries int) Provider {
	if maxEntries <= 0 {
		maxEntries = defaultCachedProviderMaxEntries
	}
	return &CachedProvider{
		inner:      inner,
		cache:      make(map[string][]float32),
		order:      make([]string, 0, maxEntries),
		maxEntries: maxEntries,
	}
}

func (p *CachedProvider) ModelName() string {
	return p.inner.ModelName()
}

func (p *CachedProvider) Embed(ctx context.Context, input string) ([]float32, error) {
	key := strings.TrimSpace(input)

	p.mu.RLock()
	if cached, ok := p.cache[key]; ok {
		p.mu.RUnlock()

		p.mu.Lock()
		p.touchKeyLocked(key)
		p.mu.Unlock()

		return cloneEmbedding(cached), nil
	}
	p.mu.RUnlock()

	vec, err := p.inner.Embed(ctx, input)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	if _, exists := p.cache[key]; exists {
		p.cache[key] = cloneEmbedding(vec)
		p.touchKeyLocked(key)
		p.mu.Unlock()
		return cloneEmbedding(vec), nil
	}

	if len(p.cache) >= p.maxEntries && len(p.order) > 0 {
		evict := p.order[0]
		delete(p.cache, evict)
		p.order = p.order[1:]
	}

	p.cache[key] = cloneEmbedding(vec)
	p.order = append(p.order, key)
	p.mu.Unlock()

	return cloneEmbedding(vec), nil
}

func (p *CachedProvider) touchKeyLocked(key string) {
	for i, existing := range p.order {
		if existing == key {
			copy(p.order[i:], p.order[i+1:])
			p.order[len(p.order)-1] = key
			return
		}
	}
}

type DisabledProvider struct{}

func NewDisabledProvider() DisabledProvider {
	return DisabledProvider{}
}

func (DisabledProvider) Embed(context.Context, string) ([]float32, error) {
	return nil, ErrProviderNotConfigured
}

func (DisabledProvider) ModelName() string {
	return "disabled"
}

type OpenAICompatibleProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// isLocalEndpoint returns true if the given URL points to a loopback address.
func isLocalEndpoint(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func NewProviderFromRuntime(cfg config.Runtime) (Provider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.EmbeddingProvider))
	if provider == "" || provider == "disabled" {
		return NewDisabledProvider(), nil
	}

	baseURL := strings.TrimSpace(cfg.EmbeddingBaseURL)
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}

	// Append /v1 when the path is missing (bare host:port form).
	if parsed, err := url.Parse(baseURL); err == nil {
		p := strings.TrimSpace(parsed.Path)
		if p == "" || p == "/" {
			parsed.Path = "/v1"
			baseURL = parsed.String()
		}
	}

	if !isLocalEndpoint(baseURL) {
		if os.Getenv("AGENTDB_EMBED_LOCAL_ONLY") == "1" {
			return nil, fmt.Errorf("embedding base URL %q is not localhost and AGENTDB_EMBED_LOCAL_ONLY=1", baseURL)
		}
		fmt.Fprintf(os.Stderr, "agentdb warning: embedding base URL %q is not localhost — code snippets will be sent to a remote host\n", baseURL)
	}

	model := strings.TrimSpace(cfg.EmbeddingModel)
	if model == "" {
		model = "nomic-embed-text"
	}

	timeout := cfg.EmbeddingTimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}

	return OpenAICompatibleProvider{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  strings.TrimSpace(cfg.EmbeddingAPIKey),
		model:   model,
		client: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}, nil
}

func (p OpenAICompatibleProvider) ModelName() string {
	return p.model
}

func (p OpenAICompatibleProvider) Embed(ctx context.Context, input string) ([]float32, error) {
	body := map[string]any{
		"model":           p.model,
		"input":           input,
		"encoding_format": "float",
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}

	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, errors.New("embedding response did not include vector data")
	}

	out := make([]float32, 0, len(parsed.Data[0].Embedding))
	for _, v := range parsed.Data[0].Embedding {
		out = append(out, float32(v))
	}

	return out, nil
}

func cloneEmbedding(in []float32) []float32 {
	out := make([]float32, len(in))
	copy(out, in)
	return out
}
