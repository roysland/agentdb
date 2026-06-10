//go:build treesitter

package parse

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/rust"
)

// NewRustParser returns a parser for Rust source files.
func NewRustParser() *TreeSitterParser {
	return &TreeSitterParser{
		lang:     rust.GetLanguage(),
		langName: "rust",
		exts:     []string{".rs"},
		parseFn:  parseRustFile,
	}
}

func parseRustFile(content []byte, root *sitter.Node, filePath, moduleName, fileHash string) ([]Symbol, []Import, []Edge) {
	var symbols []Symbol
	var imports []Import
	var edges []Edge

	tsWalkChildren(root, func(node *sitter.Node) {
		switch node.Type() {
		case "function_item":
			symbols = append(symbols, rsExtractFunc(content, node, filePath, moduleName, fileHash, ""))
		case "struct_item":
			symbols = append(symbols, rsExtractNamed(content, node, "struct", filePath, moduleName, fileHash))
		case "enum_item":
			symbols = append(symbols, rsExtractNamed(content, node, "type", filePath, moduleName, fileHash))
		case "trait_item":
			symbols = append(symbols, rsExtractNamed(content, node, "interface", filePath, moduleName, fileHash))
		case "type_item":
			symbols = append(symbols, rsExtractNamed(content, node, "type", filePath, moduleName, fileHash))
		case "const_item":
			symbols = append(symbols, rsExtractConst(content, node, filePath, moduleName, fileHash))
		case "static_item":
			symbols = append(symbols, rsExtractConst(content, node, filePath, moduleName, fileHash))
		case "impl_item":
			methods := rsExtractImpl(content, node, filePath, moduleName, fileHash)
			symbols = append(symbols, methods...)
		case "use_declaration":
			imp, edg := rsExtractUse(content, node, filePath)
			imports = append(imports, imp...)
			edges = append(edges, edg...)
		case "mod_item":
			// Inline module: extract functions from its body
			if body := node.ChildByFieldName("body"); body != nil {
				inner, _, _ := parseRustFile(content, body, filePath, moduleName, fileHash)
				symbols = append(symbols, inner...)
			}
		}
	})

	return symbols, imports, edges
}

func rsExtractFunc(content []byte, node *sitter.Node, filePath, moduleName, fileHash, typeName string) Symbol {
	name := tsText(content, node.ChildByFieldName("name"))
	if name == "" {
		return Symbol{}
	}

	kind := "func"
	qualifiedName := moduleName + "." + name
	if typeName != "" {
		kind = "method"
		qualifiedName = moduleName + "." + typeName + "." + name
	}

	sig := tsSigUpTo(content, node, "block")

	return Symbol{
		Key:           filePath + ":" + qualifiedName,
		FilePath:      filePath,
		Language:      "rust",
		Kind:          kind,
		Name:          name,
		QualifiedName: qualifiedName,
		Signature:     sig,
		DocComment:    rsDocComment(content, node),
		Visibility:    rsVisibility(content, node),
		BodySnippet:   tsSnippet(content, node),
		StartLine:     tsLine(node),
		EndLine:       tsEndLine(node),
		FileHash:      fileHash,
	}
}

func rsExtractNamed(content []byte, node *sitter.Node, kind, filePath, moduleName, fileHash string) Symbol {
	name := tsText(content, node.ChildByFieldName("name"))
	if name == "" {
		return Symbol{}
	}
	qualifiedName := moduleName + "." + name
	sig := tsSigUpTo(content, node, "field_declaration_list")
	if sig == "" || sig == tsText(content, node) {
		sig = tsSigUpTo(content, node, "enum_variant_list")
	}
	if sig == "" || sig == tsText(content, node) {
		sig = tsSigUpTo(content, node, "declaration_list")
	}

	return Symbol{
		Key:           filePath + ":" + qualifiedName,
		FilePath:      filePath,
		Language:      "rust",
		Kind:          kind,
		Name:          name,
		QualifiedName: qualifiedName,
		Signature:     sig,
		DocComment:    rsDocComment(content, node),
		Visibility:    rsVisibility(content, node),
		BodySnippet:   tsSnippet(content, node),
		StartLine:     tsLine(node),
		EndLine:       tsEndLine(node),
		FileHash:      fileHash,
	}
}

func rsExtractConst(content []byte, node *sitter.Node, filePath, moduleName, fileHash string) Symbol {
	name := tsText(content, node.ChildByFieldName("name"))
	if name == "" {
		return Symbol{}
	}
	qualifiedName := moduleName + "." + name
	kind := "const"
	if node.Type() == "static_item" {
		kind = "var"
	}

	return Symbol{
		Key:           filePath + ":" + qualifiedName,
		FilePath:      filePath,
		Language:      "rust",
		Kind:          kind,
		Name:          name,
		QualifiedName: qualifiedName,
		Signature:     strings.TrimSpace(tsText(content, node)),
		DocComment:    rsDocComment(content, node),
		Visibility:    rsVisibility(content, node),
		BodySnippet:   tsText(content, node),
		StartLine:     tsLine(node),
		EndLine:       tsEndLine(node),
		FileHash:      fileHash,
	}
}

// rsExtractImpl extracts methods from an impl block.
// Handles both `impl Type` and `impl Trait for Type`.
func rsExtractImpl(content []byte, node *sitter.Node, filePath, moduleName, fileHash string) []Symbol {
	// Determine the implementing type name
	typeNode := node.ChildByFieldName("type")
	typeName := ""
	if typeNode != nil {
		typeName = tsText(content, typeNode)
		// strip generic params: Vec<T> → Vec
		if idx := strings.IndexByte(typeName, '<'); idx >= 0 {
			typeName = typeName[:idx]
		}
	}

	var out []Symbol
	body := node.ChildByFieldName("body")
	if body == nil {
		return nil
	}

	tsWalkChildren(body, func(child *sitter.Node) {
		if child.Type() == "function_item" {
			sym := rsExtractFunc(content, child, filePath, moduleName, fileHash, typeName)
			if sym.Name != "" {
				out = append(out, sym)
			}
		}
	})

	return out
}

func rsExtractUse(content []byte, node *sitter.Node, filePath string) ([]Import, []Edge) {
	// `use std::collections::HashMap;`
	// The path is the full use tree text
	path := strings.TrimSpace(tsText(content, node))
	path = strings.TrimPrefix(path, "use ")
	path = strings.TrimSuffix(path, ";")
	path = strings.TrimSpace(path)

	if path == "" {
		return nil, nil
	}

	// Last segment as alias
	parts := strings.Split(path, "::")
	alias := parts[len(parts)-1]
	alias = strings.TrimSuffix(strings.TrimPrefix(alias, "{"), "}")

	imp := Import{Path: path, Alias: alias}
	edge := Edge{
		FromKind: "file", FromRef: filePath,
		ToKind: "file", ToRef: path, EdgeKind: "imports", Line: tsLine(node),
	}
	return []Import{imp}, []Edge{edge}
}

func rsVisibility(content []byte, node *sitter.Node) string {
	if vis := node.ChildByFieldName("visibility_modifier"); vis != nil {
		t := tsText(content, vis)
		if strings.HasPrefix(t, "pub") {
			return "exported"
		}
	}
	return "unexported"
}

// rsDocComment collects `///` doc comments immediately preceding the node.
func rsDocComment(content []byte, node *sitter.Node) string {
	var lines []string
	prev := node.PrevNamedSibling()
	for prev != nil && prev.Type() == "line_comment" {
		text := tsText(content, prev)
		text = strings.TrimPrefix(text, "///")
		text = strings.TrimPrefix(text, "//")
		lines = append([]string{strings.TrimSpace(text)}, lines...)
		prev = prev.PrevNamedSibling()
	}
	return strings.Join(lines, "\n")
}
