package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func LoadDefaultDatabaseURL() string {
	return loadConfigValue("AGENTDB_DB_URL")
}

func LoadDefaultDatabaseDriver() string {
	return loadConfigValue("AGENTDB_DB_DRIVER")
}

func LoadDefaultProjectPath() string {
	return loadConfigValue("AGENTDB_PROJECT_PATH")
}

func LoadDefaultLinesPerChunk() int {
	return loadFirstConfigIntValue("AGENTDB_LINES_PER_CHUNK")
}

func LoadDefaultEmbedProvider() string {
	return loadFirstConfigValue("AGENTDB_EMBED_PROVIDER")
}

func LoadDefaultEmbedBaseURL() string {
	return loadFirstConfigValue("AGENTDB_EMBED_BASE_URL")
}

func LoadDefaultEmbedAPIKey() string {
	return loadFirstConfigValue("AGENTDB_EMBED_API_KEY")
}

func LoadDefaultEmbedModel() string {
	return loadFirstConfigValue("AGENTDB_EMBED_MODEL")
}

func LoadDefaultEmbedTimeoutSeconds() int {
	return loadFirstConfigIntValue("AGENTDB_EMBED_TIMEOUT_SECONDS")
}

// expandTilde replaces a leading ~ with the user's home directory.
func expandTilde(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func SaveDefaultDatabaseURL(dbURL string) error {
	dbURL = strings.TrimSpace(dbURL)
	if dbURL == "" {
		return nil
	}
	return upsertConfigValue("AGENTDB_DB_URL", dbURL)
}

func SaveDefaultProjectPath(projectPath string) error {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return nil
	}
	return upsertConfigValue("AGENTDB_PROJECT_PATH", projectPath)
}

func loadConfigValue(key string) string {
	cfgPath := configFilePath()
	if cfgPath == "" {
		return ""
	}

	values := parseConfigValues(cfgPath)
	val, ok := values[key]
	if !ok {
		return ""
	}
	return expandTilde(strings.TrimSpace(val))
}

func loadFirstConfigValue(keys ...string) string {
	for _, key := range keys {
		if val := loadConfigValue(key); val != "" {
			return val
		}
	}
	return ""
}

func loadFirstConfigIntValue(keys ...string) int {
	for _, key := range keys {
		raw := strings.TrimSpace(loadConfigValue(key))
		if raw == "" {
			continue
		}
		parsed, err := strconv.Atoi(raw)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func upsertConfigValue(key, value string) error {
	cfgPath := configFilePath()
	if cfgPath == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return err
	}

	values := parseConfigValues(cfgPath)
	values[key] = value

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Keep frequently used keys first for readability.
	ordered := make([]string, 0, len(keys))
	if containsKey(values, "AGENTDB_DB_URL") {
		ordered = append(ordered, "AGENTDB_DB_URL")
	}
	if containsKey(values, "AGENTDB_DB_DRIVER") {
		ordered = append(ordered, "AGENTDB_DB_DRIVER")
	}
	if containsKey(values, "AGENTDB_PROJECT_PATH") {
		ordered = append(ordered, "AGENTDB_PROJECT_PATH")
	}
	for _, k := range keys {
		if k == "AGENTDB_DB_URL" || k == "AGENTDB_DB_DRIVER" || k == "AGENTDB_PROJECT_PATH" {
			continue
		}
		ordered = append(ordered, k)
	}

	lines := make([]string, 0, len(ordered))
	for _, k := range ordered {
		escaped := strings.ReplaceAll(values[k], "\"", "\\\"")
		lines = append(lines, fmt.Sprintf("%s = \"%s\"", k, escaped))
	}
	payload := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(cfgPath, []byte(payload), 0o644)
}

func parseConfigValues(cfgPath string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return out
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		v = strings.Trim(v, "\"")
		if k != "" {
			out[k] = v
		}
	}

	return out
}

func containsKey(values map[string]string, key string) bool {
	_, ok := values[key]
	return ok
}

func configFilePath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "agentdb", "config.toml")
	}

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}

	return filepath.Join(home, ".config", "agentdb", "config.toml")
}

func DefaultDatabasePath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		return filepath.Join(xdg, "agentdb", "agentdb.db")
	}

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "app.db"
	}

	return filepath.Join(home, ".local", "share", "agentdb", "agentdb.db")
}
