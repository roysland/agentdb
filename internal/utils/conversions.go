package utils

import "strings"

// nullIfEmpty converts an empty string to nil, otherwise returns the string.
func nullIfEmpty(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
