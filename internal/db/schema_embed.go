package db

import (
	"fmt"
	"sync"
)

// schemaContent holds the embedded schema, lazily loaded from the root package
var (
	schemaContentOnce sync.Once
	schemaContent     string
	schemaLoadErr     error
)

// GetEmbeddedSchema returns the schema SQL content.
// It first tries to get it from the root package's embedded FS.
func GetEmbeddedSchema() (string, error) {
	schemaContentOnce.Do(func() {
		// Try to load from root-level embed (will be set by init function)
		schemaContent = getRootEmbeddedSchema()
		if schemaContent == "" {
			schemaLoadErr = fmt.Errorf("no embedded schema available")
		}
	})
	if schemaLoadErr != nil {
		return "", schemaLoadErr
	}
	return schemaContent, nil
}

// getRootEmbeddedSchema is meant to be called from root package via build-time setting
func getRootEmbeddedSchema() string {
	return rootSchemaFS
}

// rootSchemaFS will be set by init() in the root package (via embed)
var rootSchemaFS string

// SetEmbeddedSchema is called by the root package to set the embedded schema
func SetEmbeddedSchema(schema string) {
	rootSchemaFS = schema
}
