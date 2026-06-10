//go:build !treesitter

package parse

// DefaultParsers returns the built-in set of parsers for the default (no-CGo) build.
// Add -tags treesitter to the build to include Python, TypeScript, and Rust support.
func DefaultParsers() []Parser {
	return []Parser{&GoParser{}}
}
