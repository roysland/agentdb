//go:build treesitter

package parse

// DefaultParsers returns all built-in parsers, including tree-sitter language parsers.
// Requires building with -tags treesitter (implies CGo and a C compiler).
func DefaultParsers() []Parser {
	return []Parser{
		&GoParser{},
		NewPythonParser(),
		NewTypeScriptParser(),
		NewTSXParser(),
		NewJavaScriptParser(),
		NewRustParser(),
	}
}
