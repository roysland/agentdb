package embed

import (
	"testing"

	"github.com/roysland/agentdb/internal/config"
)

func TestNewProviderFromRuntime_OllamaProviderSupported(t *testing.T) {
	cfg := config.Runtime{
		EmbeddingProvider: "ollama",
		EmbeddingBaseURL:  "http://localhost:11434",
		EmbeddingModel:    "nomic-embed-text",
	}

	provider, err := NewProviderFromRuntime(cfg)
	if err != nil {
		t.Fatalf("expected ollama provider to initialize, got error: %v", err)
	}

	op, ok := provider.(OpenAICompatibleProvider)
	if !ok {
		t.Fatalf("expected OpenAICompatibleProvider, got %T", provider)
	}

	if op.baseURL != "http://localhost:11434/v1" {
		t.Fatalf("baseURL = %q, want http://localhost:11434/v1", op.baseURL)
	}
	if op.model != "nomic-embed-text" {
		t.Fatalf("model = %q, want nomic-embed-text", op.model)
	}
}

func TestNewProviderFromRuntime_OllamaDefaultBaseURL(t *testing.T) {
	cfg := config.Runtime{
		EmbeddingProvider: "ollama",
		EmbeddingModel:    "nomic-embed-text",
	}

	provider, err := NewProviderFromRuntime(cfg)
	if err != nil {
		t.Fatalf("expected ollama provider to initialize, got error: %v", err)
	}

	op, ok := provider.(OpenAICompatibleProvider)
	if !ok {
		t.Fatalf("expected OpenAICompatibleProvider, got %T", provider)
	}

	if op.baseURL != "http://localhost:11434/v1" {
		t.Fatalf("baseURL = %q, want http://localhost:11434/v1", op.baseURL)
	}
}

func TestNewProviderFromRuntime_AnyProviderNameAccepted(t *testing.T) {
	for _, name := range []string{"ollama", "openai", "llamacpp", "anything"} {
		cfg := config.Runtime{
			EmbeddingProvider: name,
			EmbeddingBaseURL:  "http://localhost:8080/v1",
			EmbeddingModel:    "nomic-embed-text",
		}
		_, err := NewProviderFromRuntime(cfg)
		if err != nil {
			t.Fatalf("provider %q: expected success, got error: %v", name, err)
		}
	}
}

func TestNewProviderFromRuntime_LocalOnlyRejectsRemoteBaseURL(t *testing.T) {
	t.Setenv("AGENTDB_EMBED_LOCAL_ONLY", "1")

	cfg := config.Runtime{
		EmbeddingProvider: "ollama",
		EmbeddingBaseURL:  "https://example.com/v1",
		EmbeddingModel:    "nomic-embed-text",
	}

	_, err := NewProviderFromRuntime(cfg)
	if err == nil {
		t.Fatalf("expected error when AGENTDB_EMBED_LOCAL_ONLY=1 and base URL is remote")
	}
}
