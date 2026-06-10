package parse

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/roysland/agentdb/internal/observe"
)

// PluginRegistry discovers, loads, and manages external parser plugins.
// It combines plugin parsers with built-in parsers, giving plugins priority
// over builtins when both handle the same language.
type PluginRegistry struct {
	plugins    []*PluginProcess
	builtins   []Parser
	pluginDirs []string
}

// NewPluginRegistry creates a registry that discovers plugins from the given directories.
// It walks each directory looking for subdirectories containing a manifest.json file,
// loads and validates the manifest, and starts the plugin subprocess.
// Non-executable binaries or failed starts are logged as warnings and skipped.
//
// Set AGENTDB_PLUGIN_SAFE_MODE=1 to disable all plugin subprocess execution.
// Set AGENTDB_PLUGIN_ALLOWLIST to a comma-separated list of plugin names to restrict
// which plugins may be loaded (e.g. "my-parser,other-parser").
func NewPluginRegistry(pluginDirs []string, builtins []Parser) (*PluginRegistry, error) {
	r := &PluginRegistry{
		builtins:   builtins,
		pluginDirs: pluginDirs,
	}

	logger := observe.NewLogger(observe.LevelDebug, os.Stderr)

	if os.Getenv("AGENTDB_PLUGIN_SAFE_MODE") == "1" {
		logger.Log(observe.LogEntry{
			Level:     "info",
			Operation: "plugin_discovery",
			Status:    "safe_mode",
			Error:     "AGENTDB_PLUGIN_SAFE_MODE=1: all plugin subprocess execution is disabled",
		})
		return r, nil
	}

	allowlist := parsePluginAllowlist(os.Getenv("AGENTDB_PLUGIN_ALLOWLIST"))

	for _, dir := range pluginDirs {
		plugins, err := discoverPlugins(dir, logger, allowlist)
		if err != nil {
			// Log warning but continue with other directories
			logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "plugin_discovery",
				Status:    "error",
				Error:     fmt.Sprintf("failed to scan plugin directory %s: %v", dir, err),
			})
			continue
		}
		r.plugins = append(r.plugins, plugins...)
	}

	return r, nil
}

// parsePluginAllowlist splits a comma-separated allowlist string into a set.
// Returns nil when the input is empty, meaning all plugins are permitted.
func parsePluginAllowlist(raw string) map[string]struct{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	allowed := make(map[string]struct{})
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			allowed[name] = struct{}{}
		}
	}
	return allowed
}

// discoverPlugins walks a plugin directory and starts all valid plugins found.
// allowlist restricts which plugin names may be loaded; nil means all are permitted.
func discoverPlugins(dir string, logger *observe.Logger, allowlist map[string]struct{}) ([]*PluginProcess, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// Directory doesn't exist — not an error, just nothing to discover
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read plugin directory %s: %w", dir, err)
	}

	var plugins []*PluginProcess

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginDir := filepath.Join(dir, entry.Name())
		manifestPath := filepath.Join(pluginDir, "manifest.json")

		// Check if manifest.json exists
		if _, err := os.Stat(manifestPath); err != nil {
			continue // No manifest — skip this subdirectory
		}

		manifest, err := LoadManifest(pluginDir)
		if err != nil {
			logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "plugin_load",
				Status:    "error",
				Error:     fmt.Sprintf("invalid manifest in %s: %v", pluginDir, err),
			})
			continue
		}

		if allowlist != nil {
			if _, ok := allowlist[manifest.Name]; !ok {
				logger.Log(observe.LogEntry{
					Level:     "info",
					Operation: "plugin_load",
					Status:    "skipped",
					Error:     fmt.Sprintf("plugin %q not in AGENTDB_PLUGIN_ALLOWLIST", manifest.Name),
				})
				continue
			}
		}

		// Check if binary is executable before attempting to start
		binaryPath := manifest.Binary
		if !filepath.IsAbs(binaryPath) {
			binaryPath = filepath.Join(pluginDir, binaryPath)
		}

		fileInfo, err := os.Stat(binaryPath)
		if err != nil {
			logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "plugin_load",
				Status:    "error",
				Error:     fmt.Sprintf("plugin %s: binary not found at %s: %v", manifest.Name, binaryPath, err),
			})
			continue
		}

		// Check if the file is executable (has any execute permission bit set)
		if fileInfo.Mode()&0111 == 0 {
			logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "plugin_load",
				Status:    "error",
				Error:     fmt.Sprintf("plugin %s: binary %s is not executable", manifest.Name, binaryPath),
			})
			continue
		}

		plugin, err := StartPlugin(manifest, pluginDir)
		if err != nil {
			logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "plugin_start",
				Status:    "error",
				Error:     fmt.Sprintf("plugin %s: failed to start: %v", manifest.Name, err),
			})
			continue
		}

		plugins = append(plugins, plugin)
	}

	return plugins, nil
}

// AllParsers returns a combined parser list where plugins take priority over
// builtins for the same language. Plugins appear first in the returned slice,
// followed by builtins whose languages are not already covered by a plugin.
func (r *PluginRegistry) AllParsers() []Parser {
	// Collect languages covered by plugins
	pluginLanguages := make(map[string]bool)
	for _, p := range r.plugins {
		pluginLanguages[p.Language()] = true
	}

	var parsers []Parser

	// Add all plugins first (they have priority)
	for _, p := range r.plugins {
		parsers = append(parsers, p)
	}

	// Add builtins only if their language isn't already covered by a plugin
	for _, b := range r.builtins {
		if !pluginLanguages[b.Language()] {
			parsers = append(parsers, b)
		}
	}

	return parsers
}

// Shutdown sends shutdown notifications to all running plugin subprocesses.
func (r *PluginRegistry) Shutdown() {
	for _, p := range r.plugins {
		p.Shutdown()
	}
}
