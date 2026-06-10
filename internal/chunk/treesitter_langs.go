//go:build treesitter

package chunk

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	tsts "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// getTreeSitterLanguage returns the tree-sitter language for the given language name.
// Returns nil if the language is not supported by tree-sitter.
// Note: Go uses stdlib go/ast and does not have a tree-sitter grammar in this project.
func getTreeSitterLanguage(lang string) *sitter.Language {
	switch lang {
	case "python":
		return python.GetLanguage()
	case "typescript":
		return tsts.GetLanguage()
	case "tsx":
		return tstsx.GetLanguage()
	case "javascript":
		return javascript.GetLanguage()
	case "rust":
		return rust.GetLanguage()
	default:
		return nil
	}
}
