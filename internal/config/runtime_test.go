package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveEmbeddingFromConfigWhenEnvMissing(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("AGENTDB_EMBED_PROVIDER", "")
	t.Setenv("AGENTDB_EMBED_BASE_URL", "")
	t.Setenv("AGENTDB_EMBED_API_KEY", "")
	t.Setenv("AGENTDB_EMBED_MODEL", "")
	t.Setenv("AGENTDB_EMBED_TIMEOUT_SECONDS", "")

	cfgPath := filepath.Join(cfgDir, "agentdb", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	content := "AGENTDB_EMBED_PROVIDER = \"ollama\"\n" +
		"AGENTDB_EMBED_BASE_URL = \"http://localhost:11434/v1\"\n" +
		"AGENTDB_EMBED_API_KEY = \"from-config\"\n" +
		"AGENTDB_EMBED_MODEL = \"nomic-embed-text\"\n" +
		"AGENTDB_EMBED_TIMEOUT_SECONDS = \"45\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	resolved := Resolve(Runtime{})
	if resolved.EmbeddingProvider != "ollama" {
		t.Fatalf("provider = %q, want ollama", resolved.EmbeddingProvider)
	}
	if resolved.EmbeddingBaseURL != "http://localhost:11434/v1" {
		t.Fatalf("base_url = %q, want http://localhost:11434/v1", resolved.EmbeddingBaseURL)
	}
	if resolved.EmbeddingAPIKey != "from-config" {
		t.Fatalf("api_key = %q, want from-config", resolved.EmbeddingAPIKey)
	}
	if resolved.EmbeddingModel != "nomic-embed-text" {
		t.Fatalf("model = %q, want nomic-embed-text", resolved.EmbeddingModel)
	}
	if resolved.EmbeddingTimeoutSeconds != 45 {
		t.Fatalf("timeout = %d, want 45", resolved.EmbeddingTimeoutSeconds)
	}
}

func TestResolveEmbeddingEnvOverridesConfig(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("AGENTDB_EMBED_PROVIDER", "ollama")
	t.Setenv("AGENTDB_EMBED_BASE_URL", "http://localhost:11434/v1")
	t.Setenv("AGENTDB_EMBED_API_KEY", "from-env")
	t.Setenv("AGENTDB_EMBED_MODEL", "env-model")
	t.Setenv("AGENTDB_EMBED_TIMEOUT_SECONDS", "12")

	cfgPath := filepath.Join(cfgDir, "agentdb", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	content := "AGENTDB_EMBED_PROVIDER = \"disabled\"\n" +
		"AGENTDB_EMBED_BASE_URL = \"http://localhost:11434/v1\"\n" +
		"AGENTDB_EMBED_API_KEY = \"from-config\"\n" +
		"AGENTDB_EMBED_MODEL = \"config-model\"\n" +
		"AGENTDB_EMBED_TIMEOUT_SECONDS = \"99\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	resolved := Resolve(Runtime{})
	if resolved.EmbeddingProvider != "ollama" {
		t.Fatalf("provider = %q, want ollama", resolved.EmbeddingProvider)
	}
	if resolved.EmbeddingBaseURL != "http://localhost:11434/v1" {
		t.Fatalf("base_url = %q, want http://localhost:11434/v1", resolved.EmbeddingBaseURL)
	}
	if resolved.EmbeddingAPIKey != "from-env" {
		t.Fatalf("api_key = %q, want from-env", resolved.EmbeddingAPIKey)
	}
	if resolved.EmbeddingModel != "env-model" {
		t.Fatalf("model = %q, want env-model", resolved.EmbeddingModel)
	}
	if resolved.EmbeddingTimeoutSeconds != 12 {
		t.Fatalf("timeout = %d, want 12", resolved.EmbeddingTimeoutSeconds)
	}
}

func TestResolveEmbeddingProviderStaysDisabledWithAPIKeyOnly(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("AGENTDB_EMBED_PROVIDER", "")
	t.Setenv("AGENTDB_EMBED_API_KEY", "")

	cfgPath := filepath.Join(cfgDir, "agentdb", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	// Provider explicitly set to disabled; api key present — must NOT auto-enable.
	content := "AGENTDB_EMBED_PROVIDER = \"disabled\"\nAGENTDB_EMBED_API_KEY = \"from-config\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	resolved := Resolve(Runtime{})
	if resolved.EmbeddingProvider != "disabled" {
		t.Fatalf("provider = %q, want disabled (API key must not auto-enable provider)", resolved.EmbeddingProvider)
	}
}

func TestResolveLinesPerChunkFromConfigWhenEnvMissing(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("AGENTDB_LINES_PER_CHUNK", "")

	cfgPath := filepath.Join(cfgDir, "agentdb", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	content := "AGENTDB_LINES_PER_CHUNK = \"18\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	resolved := Resolve(Runtime{})
	if resolved.IndexLinesPerChunk != 18 {
		t.Fatalf("lines_per_chunk = %d, want 18", resolved.IndexLinesPerChunk)
	}
}

func TestResolveLinesPerChunkEnvOverridesConfig(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("AGENTDB_LINES_PER_CHUNK", "12")

	cfgPath := filepath.Join(cfgDir, "agentdb", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	content := "AGENTDB_LINES_PER_CHUNK = \"25\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	resolved := Resolve(Runtime{})
	if resolved.IndexLinesPerChunk != 12 {
		t.Fatalf("lines_per_chunk = %d, want 12", resolved.IndexLinesPerChunk)
	}
}

func TestResolveDatabaseURLFromCanonicalEnv(t *testing.T) {
	t.Setenv("AGENTDB_DB_URL", "sqlite:///from-db-url")

	resolved := Resolve(Runtime{})
	if resolved.DatabaseURL != "sqlite:///from-db-url" {
		t.Fatalf("database_url = %q, want sqlite:///from-db-url", resolved.DatabaseURL)
	}
}

func TestResolveDatabaseURLFromCanonicalConfigKey(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("AGENTDB_DB_URL", "")

	cfgPath := filepath.Join(cfgDir, "agentdb", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	content := "AGENTDB_DB_URL = \"sqlite:///from-config\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	resolved := Resolve(Runtime{})
	if resolved.DatabaseURL != "sqlite:///from-config" {
		t.Fatalf("database_url = %q, want sqlite:///from-config", resolved.DatabaseURL)
	}
}

func TestResolveDatabaseURLIgnoresLegacyEnv(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("AGENTDB_DB_URL", "")
	t.Setenv("AGENTDB_URL", "sqlite:///legacy-ignored")

	resolved := Resolve(Runtime{})
	if resolved.DatabaseURL == "sqlite:///legacy-ignored" {
		t.Fatalf("database_url resolved from deprecated AGENTDB_URL")
	}
}

func TestResolveDatabaseDriverFromCanonicalEnv(t *testing.T) {
	t.Setenv("AGENTDB_DB_DRIVER", "sqlite3")

	resolved := Resolve(Runtime{})
	if resolved.DatabaseDriver != "sqlite3" {
		t.Fatalf("database_driver = %q, want sqlite3", resolved.DatabaseDriver)
	}
}

func TestResolveDatabaseDriverFromConfigWhenEnvMissing(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("AGENTDB_DB_DRIVER", "")

	cfgPath := filepath.Join(cfgDir, "agentdb", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	content := "AGENTDB_DB_DRIVER = \"sqlite3\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	resolved := Resolve(Runtime{})
	if resolved.DatabaseDriver != "sqlite3" {
		t.Fatalf("database_driver = %q, want sqlite3", resolved.DatabaseDriver)
	}
}
