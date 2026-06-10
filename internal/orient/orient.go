// Package orient provides orientation document classification and retrieval.
package orient

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/roysland/agentdb/internal/observe"
	"gopkg.in/yaml.v3"
)

// DocType represents the classification of an orientation document.
type DocType string

const (
	DocTypeReadme            DocType = "readme"
	DocTypeDesign            DocType = "design"
	DocTypeArchitecture      DocType = "architecture"
	DocTypeAgentInstructions DocType = "agent-instructions"
	DocTypeTodo              DocType = "todo"
	DocTypeContributing      DocType = "contributing"
	DocTypeFeatureList       DocType = "feature-list"
	DocTypeGeneral           DocType = "general"
)

// PatternSet groups file patterns for a single doc type.
type PatternSet struct {
	Patterns []string `yaml:"patterns"`
	Priority int      `yaml:"priority"`
	Excludes []string `yaml:"exclude"`
	MaxItems int      `yaml:"max_items"`
}

// Config maps doc types to their pattern sets.
type Config map[DocType]PatternSet

// configFile represents the top-level structure of agentdb.yml.
type configFile struct {
	OrientationPatterns map[string]PatternSet `yaml:"orientation_patterns"`
}

// DefaultConfig returns the built-in default orientation patterns.
func DefaultConfig() Config {
	return Config{
		DocTypeReadme: {
			Patterns: []string{"README*", "readme*"},
			Priority: 1,
			Excludes: []string{},
		},
		DocTypeAgentInstructions: {
			Patterns: []string{"CLAUDE.md", "agents.md", ".instructions.md", ".copilot-instructions.md"},
			Priority: 2,
			Excludes: []string{},
		},
		DocTypeFeatureList: {
			Patterns: []string{"FEATURE_LIST*", "FEATURES*", "features.md"},
			Priority: 3,
			Excludes: []string{},
		},
		DocTypeArchitecture: {
			Patterns: []string{"architecture*", "arch.md", "ARCHITECTURE.md"},
			Priority: 4,
			Excludes: []string{},
		},
		DocTypeDesign: {
			Patterns: []string{"design*", "design-doc*", "DESIGN.md"},
			Priority: 5,
			Excludes: []string{},
		},
		DocTypeTodo: {
			Patterns: []string{"todos.md", "TODO.md", "todo.md"},
			Priority: 6,
			Excludes: []string{},
		},
		DocTypeContributing: {
			Patterns: []string{"CONTRIBUTING*", "contributing.md"},
			Priority: 7,
			Excludes: []string{},
		},
		DocTypeGeneral: {
			Patterns: []string{"*.md"},
			Priority: 8,
			Excludes: []string{"CHANGELOG*", "LICENSE*", "HISTORY*", "MAINTENANCE*", "SECURITY*"},
			MaxItems: 5,
		},
	}
}

// Load reads configuration from agentdb.yml or .kiro/agentdb.yml in the codebase root.
// Returns built-in defaults if no config file is found.
// Logs a warning and returns defaults if the config file is malformed YAML.
func Load(codebaseRoot string, logger *observe.Logger) (Config, error) {
	candidates := []string{
		filepath.Join(codebaseRoot, "agentdb.yml"),
		filepath.Join(codebaseRoot, ".kiro", "agentdb.yml"),
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			// Non-existence errors (permission, etc.) — log warning and try next
			if logger != nil {
				logger.Log(observe.LogEntry{
					Level:     "warn",
					Operation: "orient.Load",
					Error:     fmt.Sprintf("failed to read config file %s: %v", path, err),
				})
			}
			continue
		}

		cfg, err := parseConfig(data)
		if err != nil {
			if logger != nil {
				logger.Log(observe.LogEntry{
					Level:     "warn",
					Operation: "orient.Load",
					Error:     fmt.Sprintf("malformed YAML in %s: %v; using defaults", path, err),
				})
			}
			return DefaultConfig(), nil
		}
		return cfg, nil
	}

	return DefaultConfig(), nil
}

// parseConfig parses YAML data into a Config.
func parseConfig(data []byte) (Config, error) {
	var cf configFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, err
	}

	if cf.OrientationPatterns == nil {
		return nil, fmt.Errorf("missing orientation_patterns key")
	}

	config := make(Config)
	for key, ps := range cf.OrientationPatterns {
		docType := yamlKeyToDocType(key)
		if docType == "" {
			continue
		}
		config[docType] = ps
	}

	if len(config) == 0 {
		return nil, fmt.Errorf("no valid orientation patterns found")
	}

	return config, nil
}

// yamlKeyToDocType maps YAML keys (snake_case) to DocType constants.
func yamlKeyToDocType(key string) DocType {
	switch key {
	case "readme":
		return DocTypeReadme
	case "design":
		return DocTypeDesign
	case "architecture":
		return DocTypeArchitecture
	case "agent_instructions":
		return DocTypeAgentInstructions
	case "todo":
		return DocTypeTodo
	case "contributing":
		return DocTypeContributing
	case "feature_list":
		return DocTypeFeatureList
	case "general":
		return DocTypeGeneral
	default:
		return ""
	}
}

// ClassifyResult holds the classification outcome for a file path.
type ClassifyResult struct {
	DocType  DocType
	Priority int
}

// Classify matches a file path against the loaded config patterns and returns
// the DocType and priority. Returns an empty ClassifyResult if no pattern matches.
// The filePath should be relative to the codebase root.
func Classify(filePath string, config Config) ClassifyResult {
	// Use only the base name for pattern matching (orientation docs are identified by filename)
	baseName := filepath.Base(filePath)
	isRoot := !strings.Contains(filepath.ToSlash(filePath), "/")

	var bestResult ClassifyResult
	bestFound := false

	// Iterate in deterministic doc type order so equal-priority ties are stable.
	docTypes := make([]DocType, 0, len(config))
	for docType := range config {
		docTypes = append(docTypes, docType)
	}
	sort.Slice(docTypes, func(i, j int) bool {
		return string(docTypes[i]) < string(docTypes[j])
	})

	for _, docType := range docTypes {
		ps := config[docType]
		if matchesPatternSet(baseName, ps) {
			priority := ps.Priority
			if !isRoot {
				priority += 10
			}
			if !bestFound || priority < bestResult.Priority ||
				(priority == bestResult.Priority && string(docType) < string(bestResult.DocType)) {
				bestResult = ClassifyResult{
					DocType:  docType,
					Priority: priority,
				}
				bestFound = true
			}
		}
	}

	return bestResult
}

// matchesPatternSet checks if a filename matches the patterns in a PatternSet
// and does not match any exclude patterns.
func matchesPatternSet(baseName string, ps PatternSet) bool {
	// Check excludes first
	for _, excl := range ps.Excludes {
		matched, err := filepath.Match(excl, baseName)
		if err == nil && matched {
			return false
		}
	}

	// Check patterns
	for _, pattern := range ps.Patterns {
		matched, err := filepath.Match(pattern, baseName)
		if err == nil && matched {
			return true
		}
	}

	return false
}
