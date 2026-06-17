package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
	t.Setenv("AGENTDB_DB_DRIVER", "sqlite")

	resolved := Resolve(Runtime{})
	if resolved.DatabaseDriver != "sqlite" {
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
	content := "AGENTDB_DB_DRIVER = \"sqlite\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	resolved := Resolve(Runtime{})
	if resolved.DatabaseDriver != "sqlite" {
		t.Fatalf("database_driver = %q, want sqlite", resolved.DatabaseDriver)
	}
}
