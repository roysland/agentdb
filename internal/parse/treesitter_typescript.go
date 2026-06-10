//go:build treesitter

package parse

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	tsts "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// NewTypeScriptParser returns a parser for .ts files.
func NewTypeScriptParser() *TreeSitterParser {
	return &TreeSitterParser{
		lang:     tsts.GetLanguage(),
		langName: "typescript",
		exts:     []string{".ts"},
		parseFn:  parseTSFile,
	}
}

// NewTSXParser returns a parser for .tsx files.
func NewTSXParser() *TreeSitterParser {
	return &TreeSitterParser{
		lang:     tstsx.GetLanguage(),
		langName: "typescript",
		exts:     []string{".tsx"},
		parseFn:  parseTSFile,
	}
}

// NewJavaScriptParser returns a parser for .js and .jsx files.
func NewJavaScriptParser() *TreeSitterParser {
	return &TreeSitterParser{
		lang:     javascript.GetLanguage(),
		langName: "javascript",
		exts:     []string{".js", ".jsx", ".mjs"},
		parseFn:  parseTSFile, // same structure as TypeScript
	}
}

func parseTSFile(content []byte, root *sitter.Node, filePath, moduleName, fileHash string) ([]Symbol, []Import, []Edge) {
	var symbols []Symbol
	var imports []Import
	var edges []Edge

	tsWalkChildren(root, func(node *sitter.Node) {
		syms, imps, edgs := tsSingleNode(content, node, filePath, moduleName, fileHash)
		symbols = append(symbols, syms...)
		imports = append(imports, imps...)
		edges = append(edges, edgs...)
	})

	return symbols, imports, edges
}

// tsSingleNode processes one top-level node, handling export wrappers transparently.
func tsSingleNode(content []byte, node *sitter.Node, filePath, moduleName, fileHash string) ([]Symbol, []Import, []Edge) {
	switch node.Type() {
	case "function_declaration":
		return []Symbol{tsExtractFunc(content, node, filePath, moduleName, fileHash, "")}, nil, nil

	case "class_declaration":
		cls, methods := tsExtractClass(content, node, filePath, moduleName, fileHash)
		syms := append([]Symbol{cls}, methods...)
		return syms, nil, nil

	case "interface_declaration":
		return []Symbol{tsExtractInterface(content, node, filePath, moduleName, fileHash)}, nil, nil

	case "type_alias_declaration":
		return []Symbol{tsExtractTypeAlias(content, node, filePath, moduleName, fileHash)}, nil, nil

	case "lexical_declaration", "variable_declaration":
		// const/let/var myFunc = () => {} or const MyClass = class {}
		return tsExtractVarDecl(content, node, filePath, moduleName, fileHash), nil, nil

	case "export_statement":
		// unwrap and re-dispatch the declaration
		decl := node.ChildByFieldName("declaration")
		if decl != nil {
			return tsSingleNode(content, decl, filePath, moduleName, fileHash)
		}
		// export default function / export default class
		for i := 0; i < int(node.ChildCount()); i++ {
			c := node.Child(i)
			switch c.Type() {
			case "function_declaration", "class_declaration", "interface_declaration":
				return tsSingleNode(content, c, filePath, moduleName, fileHash)
			}
		}
		return nil, nil, nil

	case "import_statement":
		imp, edg := tsExtractImport(content, node, filePath)
		return nil, imp, edg

	default:
		return nil, nil, nil
	}
}

func tsExtractFunc(content []byte, node *sitter.Node, filePath, moduleName, fileHash, className string) Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		// anonymous function — skip
		return Symbol{}
	}
	name := tsText(content, nameNode)
	if name == "" {
		return Symbol{}
	}

	kind := "func"
	qualifiedName := moduleName + "." + name
	if className != "" {
		kind = "method"
		qualifiedName = moduleName + "." + className + "." + name
	}

	sig := tsSigUpTo(content, node, "statement_block")

	return Symbol{
		Key:           filePath + ":" + qualifiedName,
		FilePath:      filePath,
		Language:      "typescript",
		Kind:          kind,
		Name:          name,
		QualifiedName: qualifiedName,
		Signature:     sig,
		DocComment:    tsLeadingComment(content, node),
		Visibility:    tsExportVisibility(node),
		BodySnippet:   tsSnippet(content, node),
		StartLine:     tsLine(node),
		EndLine:       tsEndLine(node),
		FileHash:      fileHash,
	}
}

func tsExtractClass(content []byte, node *sitter.Node, filePath, moduleName, fileHash string) (Symbol, []Symbol) {
	nameNode := node.ChildByFieldName("name")
	name := tsText(content, nameNode)
	qualifiedName := moduleName + "." + name

	sig := tsSigUpTo(content, node, "class_body")

	cls := Symbol{
		Key:           filePath + ":" + qualifiedName,
		FilePath:      filePath,
		Language:      "typescript",
		Kind:          "struct",
		Name:          name,
		QualifiedName: qualifiedName,
		Signature:     sig,
		DocComment:    tsLeadingComment(content, node),
		Visibility:    tsExportVisibility(node),
		BodySnippet:   tsSnippet(content, node),
		StartLine:     tsLine(node),
		EndLine:       tsEndLine(node),
		FileHash:      fileHash,
	}

	var methods []Symbol
	if body := node.ChildByFieldName("body"); body != nil {
		tsWalkChildren(body, func(child *sitter.Node) {
			switch child.Type() {
			case "method_definition":
				m := tsExtractMethod(content, child, filePath, moduleName, fileHash, name)
				if m.Name != "" {
					methods = append(methods, m)
				}
			}
		})
	}

	return cls, methods
}

func tsExtractMethod(content []byte, node *sitter.Node, filePath, moduleName, fileHash, className string) Symbol {
	nameNode := node.ChildByFieldName("name")
	name := tsText(content, nameNode)
	qualifiedName := moduleName + "." + className + "." + name

	sig := tsSigUpTo(content, node, "statement_block")

	return Symbol{
		Key:           filePath + ":" + qualifiedName,
		FilePath:      filePath,
		Language:      "typescript",
		Kind:          "method",
		Name:          name,
		QualifiedName: qualifiedName,
		Signature:     sig,
		DocComment:    tsLeadingComment(content, node),
		Visibility:    tsMethodVisibility(node, content),
		BodySnippet:   tsSnippet(content, node),
		StartLine:     tsLine(node),
		EndLine:       tsEndLine(node),
		FileHash:      fileHash,
	}
}

func tsExtractInterface(content []byte, node *sitter.Node, filePath, moduleName, fileHash string) Symbol {
	name := tsText(content, node.ChildByFieldName("name"))
	qualifiedName := moduleName + "." + name
	sig := tsSigUpTo(content, node, "interface_body")

	return Symbol{
		Key:           filePath + ":" + qualifiedName,
		FilePath:      filePath,
		Language:      "typescript",
		Kind:          "interface",
		Name:          name,
		QualifiedName: qualifiedName,
		Signature:     sig,
		Visibility:    tsExportVisibility(node),
		BodySnippet:   tsSnippet(content, node),
		StartLine:     tsLine(node),
		EndLine:       tsEndLine(node),
		FileHash:      fileHash,
	}
}

func tsExtractTypeAlias(content []byte, node *sitter.Node, filePath, moduleName, fileHash string) Symbol {
	name := tsText(content, node.ChildByFieldName("name"))
	qualifiedName := moduleName + "." + name

	return Symbol{
		Key:           filePath + ":" + qualifiedName,
		FilePath:      filePath,
		Language:      "typescript",
		Kind:          "type",
		Name:          name,
		QualifiedName: qualifiedName,
		Signature:     strings.TrimSpace(tsText(content, node)),
		Visibility:    tsExportVisibility(node),
		BodySnippet:   tsText(content, node),
		StartLine:     tsLine(node),
		EndLine:       tsEndLine(node),
		FileHash:      fileHash,
	}
}

// tsExtractVarDecl handles `const foo = () => {}` and `const Foo = class {}` patterns.
func tsExtractVarDecl(content []byte, node *sitter.Node, filePath, moduleName, fileHash string) []Symbol {
	var out []Symbol
	tsWalkChildren(node, func(declarator *sitter.Node) {
		if declarator.Type() != "variable_declarator" {
			return
		}
		nameNode := declarator.ChildByFieldName("name")
		valueNode := declarator.ChildByFieldName("value")
		if nameNode == nil || valueNode == nil {
			return
		}
		name := tsText(content, nameNode)
		if name == "" {
			return
		}
		qualifiedName := moduleName + "." + name

		switch valueNode.Type() {
		case "arrow_function", "function_expression":
			sig := "const " + name + " = " + tsSigUpTo(content, valueNode, "statement_block")
			out = append(out, Symbol{
				Key:           filePath + ":" + qualifiedName,
				FilePath:      filePath,
				Language:      "typescript",
				Kind:          "func",
				Name:          name,
				QualifiedName: qualifiedName,
				Signature:     sig,
				Visibility:    "exported",
				BodySnippet:   tsSnippet(content, declarator),
				StartLine:     tsLine(declarator),
				EndLine:       tsEndLine(declarator),
				FileHash:      fileHash,
			})
		case "class_expression":
			sig := "const " + name + " = class"
			out = append(out, Symbol{
				Key:           filePath + ":" + qualifiedName,
				FilePath:      filePath,
				Language:      "typescript",
				Kind:          "struct",
				Name:          name,
				QualifiedName: qualifiedName,
				Signature:     sig,
				Visibility:    "exported",
				BodySnippet:   tsSnippet(content, declarator),
				StartLine:     tsLine(declarator),
				EndLine:       tsEndLine(declarator),
				FileHash:      fileHash,
			})
		}
	})
	return out
}

func tsExtractImport(content []byte, node *sitter.Node, filePath string) ([]Import, []Edge) {
	// import ... from "source"
	source := node.ChildByFieldName("source")
	if source == nil {
		return nil, nil
	}
	path := strings.Trim(tsText(content, source), `"'`)
	alias := pyLastSegment(strings.ReplaceAll(path, "/", "."))

	imp := Import{Path: path, Alias: alias}
	edge := Edge{
		FromKind: "file", FromRef: filePath,
		ToKind: "file", ToRef: path, EdgeKind: "imports", Line: tsLine(node),
	}
	return []Import{imp}, []Edge{edge}
}

// tsExportVisibility returns "exported" when the node's parent is an export_statement.
func tsExportVisibility(node *sitter.Node) string {
	if p := node.Parent(); p != nil && p.Type() == "export_statement" {
		return "exported"
	}
	return "unexported"
}

// tsMethodVisibility checks for TypeScript access modifiers (public/private/protected).
func tsMethodVisibility(node *sitter.Node, content []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		switch tsText(content, c) {
		case "private", "protected", "#":
			return "unexported"
		case "public":
			return "exported"
		}
	}
	return "exported" // default in TypeScript
}

// tsLeadingComment looks for a block comment immediately before the node.
func tsLeadingComment(content []byte, node *sitter.Node) string {
	prev := node.PrevNamedSibling()
	if prev == nil {
		return ""
	}
	if prev.Type() == "comment" {
		s := tsText(content, prev)
		s = strings.TrimPrefix(s, "/**")
		s = strings.TrimPrefix(s, "/*")
		s = strings.TrimSuffix(s, "*/")
		s = strings.TrimPrefix(s, "//")
		return strings.TrimSpace(s)
	}
	return ""
}
