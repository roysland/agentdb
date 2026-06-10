package parse

import (
	"os"
	"path/filepath"
)

// PluginDirectories returns directories to scan for parser plugins.
// It includes the default ~/.agentdb/plugins path and AGENTDB_PLUGIN_DIR when set.
func PluginDirectories() []string {
	var dirs []string

	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".agentdb", "plugins"))
	}

	if envDir := os.Getenv("AGENTDB_PLUGIN_DIR"); envDir != "" {
		dirs = append(dirs, envDir)
	}

	return dirs
}
