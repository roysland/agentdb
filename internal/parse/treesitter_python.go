//go:build treesitter

package parse

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
)

// NewPythonParser returns a parser for Python source files.
func NewPythonParser() *TreeSitterParser {
	return &TreeSitterParser{
		lang:     python.GetLanguage(),
		langName: "python",
		exts:     []string{".py"},
		parseFn:  parsePythonFile,
	}
}

func parsePythonFile(content []byte, root *sitter.Node, filePath, moduleName, fileHash string) ([]Symbol, []Import, []Edge) {
	var symbols []Symbol
	var imports []Import
	var edges []Edge

	tsWalkChildren(root, func(node *sitter.Node) {
		switch node.Type() {
		case "function_definition":
			symbols = append(symbols, pyExtractFunc(content, node, filePath, moduleName, fileHash, ""))
		case "class_definition":
			cls, methods := pyExtractClass(content, node, filePath, moduleName, fileHash)
			symbols = append(symbols, cls)
			symbols = append(symbols, methods...)
		case "decorated_definition":
			tsWalkChildren(node, func(inner *sitter.Node) {
				switch inner.Type() {
				case "function_definition":
					symbols = append(symbols, pyExtractFunc(content, inner, filePath, moduleName, fileHash, ""))
				case "class_definition":
					cls, methods := pyExtractClass(content, inner, filePath, moduleName, fileHash)
					symbols = append(symbols, cls)
					symbols = append(symbols, methods...)
				}
			})
		case "import_statement":
			imp, edg := pyExtractImport(content, node, filePath)
			imports = append(imports, imp...)
			edges = append(edges, edg...)
		case "import_from_statement":
			imp, edg := pyExtractFromImport(content, node, filePath)
			imports = append(imports, imp...)
			edges = append(edges, edg...)
		}
	})

	return symbols, imports, edges
}

func pyExtractFunc(content []byte, node *sitter.Node, filePath, moduleName, fileHash, className string) Symbol {
	name := tsText(content, node.ChildByFieldName("name"))
	params := tsText(content, node.ChildByFieldName("parameters"))

	kind := "func"
	qualifiedName := moduleName + "." + name
	if className != "" {
		kind = "method"
		qualifiedName = moduleName + "." + className + "." + name
	}

	sig := "def " + name + params
	if ret := node.ChildByFieldName("return_type"); ret != nil {
		sig += " -> " + tsText(content, ret)
	}

	return Symbol{
		Key:           filePath + ":" + qualifiedName,
		FilePath:      filePath,
		Language:      "python",
		Kind:          kind,
		Name:          name,
		QualifiedName: qualifiedName,
		Signature:     sig,
		DocComment:    pyDocstring(content, node),
		Visibility:    pyVisibility(name),
		BodySnippet:   tsSnippet(content, node),
		StartLine:     tsLine(node),
		EndLine:       tsEndLine(node),
		FileHash:      fileHash,
	}
}

func pyExtractClass(content []byte, node *sitter.Node, filePath, moduleName, fileHash string) (Symbol, []Symbol) {
	name := tsText(content, node.ChildByFieldName("name"))
	qualifiedName := moduleName + "." + name

	sig := "class " + name
	if sc := node.ChildByFieldName("superclasses"); sc != nil {
		sig += tsText(content, sc)
	}

	cls := Symbol{
		Key:           filePath + ":" + qualifiedName,
		FilePath:      filePath,
		Language:      "python",
		Kind:          "struct",
		Name:          name,
		QualifiedName: qualifiedName,
		Signature:     sig,
		DocComment:    pyDocstring(content, node),
		Visibility:    pyVisibility(name),
		BodySnippet:   tsSnippet(content, node),
		StartLine:     tsLine(node),
		EndLine:       tsEndLine(node),
		FileHash:      fileHash,
	}

	var methods []Symbol
	if body := node.ChildByFieldName("body"); body != nil {
		tsWalkChildren(body, func(child *sitter.Node) {
			switch child.Type() {
			case "function_definition":
				methods = append(methods, pyExtractFunc(content, child, filePath, moduleName, fileHash, name))
			case "decorated_definition":
				tsWalkChildren(child, func(inner *sitter.Node) {
					if inner.Type() == "function_definition" {
						methods = append(methods, pyExtractFunc(content, inner, filePath, moduleName, fileHash, name))
					}
				})
			}
		})
	}

	return cls, methods
}

func pyDocstring(content []byte, node *sitter.Node) string {
	body := node.ChildByFieldName("body")
	if body == nil {
		return ""
	}
	for i := 0; i < int(body.ChildCount()); i++ {
		child := body.Child(i)
		if !child.IsNamed() {
			continue
		}
		if child.Type() == "expression_statement" {
			for j := 0; j < int(child.ChildCount()); j++ {
				inner := child.Child(j)
				if inner.Type() == "string" {
					s := tsText(content, inner)
					// strip triple and single quotes
					for _, q := range []string{`"""`, `'''`, `"`, `'`} {
						if strings.HasPrefix(s, q) && strings.HasSuffix(s, q) && len(s) > 2*len(q) {
							s = s[len(q) : len(s)-len(q)]
							break
						}
					}
					return strings.TrimSpace(s)
				}
			}
		}
		break // only inspect first statement
	}
	return ""
}

func pyExtractImport(content []byte, node *sitter.Node, filePath string) ([]Import, []Edge) {
	var imps []Import
	var edges []Edge
	line := tsLine(node)

	tsWalkChildren(node, func(child *sitter.Node) {
		switch child.Type() {
		case "dotted_name":
			path := tsText(content, child)
			alias := pyLastSegment(path)
			imps = append(imps, Import{Path: path, Alias: alias})
			edges = append(edges, Edge{
				FromKind: "file", FromRef: filePath,
				ToKind: "file", ToRef: path, EdgeKind: "imports", Line: line,
			})
		case "aliased_import":
			nameNode := child.ChildByFieldName("name")
			aliasNode := child.ChildByFieldName("alias")
			path := tsText(content, nameNode)
			alias := pyLastSegment(path)
			if aliasNode != nil {
				alias = tsText(content, aliasNode)
			}
			imps = append(imps, Import{Path: path, Alias: alias})
			edges = append(edges, Edge{
				FromKind: "file", FromRef: filePath,
				ToKind: "file", ToRef: path, EdgeKind: "imports", Line: line,
			})
		}
	})

	return imps, edges
}

func pyExtractFromImport(content []byte, node *sitter.Node, filePath string) ([]Import, []Edge) {
	// find module_name or relative_import
	var moduleNode *sitter.Node
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		if c.Type() == "dotted_name" || c.Type() == "relative_import" {
			moduleNode = c
			break
		}
	}
	if moduleNode == nil {
		return nil, nil
	}
	path := tsText(content, moduleNode)
	alias := pyLastSegment(path)
	imp := Import{Path: path, Alias: alias}
	edge := Edge{
		FromKind: "file", FromRef: filePath,
		ToKind: "file", ToRef: path, EdgeKind: "imports", Line: tsLine(node),
	}
	return []Import{imp}, []Edge{edge}
}

func pyVisibility(name string) string {
	if strings.HasPrefix(name, "_") {
		return "unexported"
	}
	return "exported"
}

func pyLastSegment(dotted string) string {
	parts := strings.Split(dotted, ".")
	return parts[len(parts)-1]
}
