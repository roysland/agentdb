package db

import (
	"os"
	"regexp"
	"testing"
)

func TestSchemaSQLVersionMatchesExecutableVersion(t *testing.T) {
	content, err := os.ReadFile("../../data/schema.sql")
	if err != nil {
		t.Fatalf("read schema.sql: %v", err)
	}

	re := regexp.MustCompile(`schema_version'\s*,\s*'([^']+)'`)
	matches := re.FindStringSubmatch(string(content))
	if len(matches) < 2 {
		t.Fatal("schema.sql does not declare meta schema_version")
	}

	schemaSQLVersion := matches[1]
	if schemaSQLVersion != CurrentSchemaVersion {
		t.Fatalf("schema version mismatch: data/schema.sql=%q, executable=%q; bump both together", schemaSQLVersion, CurrentSchemaVersion)
	}
}
