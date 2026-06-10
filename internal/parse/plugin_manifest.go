package parse

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// PluginManifest is the manifest.json schema for a parser plugin.
type PluginManifest struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Languages []string `json:"languages"`
	Binary    string   `json:"binary"`
}

// LoadManifest reads and validates a manifest.json from the given plugin directory.
// It returns a validated PluginManifest or a descriptive error if the manifest
// is missing, unreadable, or contains invalid fields.
func LoadManifest(dir string) (PluginManifest, error) {
	manifestPath := filepath.Join(dir, "manifest.json")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return PluginManifest{}, fmt.Errorf("read plugin manifest %s: %w", manifestPath, err)
	}

	var manifest PluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return PluginManifest{}, fmt.Errorf("parse plugin manifest %s: %w", manifestPath, err)
	}

	if err := validateManifest(manifest); err != nil {
		return PluginManifest{}, fmt.Errorf("invalid plugin manifest %s: %w", manifestPath, err)
	}

	return manifest, nil
}

// validateManifest checks that all required fields are present and valid.
func validateManifest(m PluginManifest) error {
	if m.Name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if m.Version == "" {
		return fmt.Errorf("version must not be empty")
	}
	if len(m.Languages) == 0 {
		return fmt.Errorf("languages must contain at least one entry")
	}
	if m.Binary == "" {
		return fmt.Errorf("binary must not be empty")
	}
	return nil
}
