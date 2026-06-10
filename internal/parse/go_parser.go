package parse

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"unicode"
)

// GoParser parses Go source files using the stdlib go/ast package.
type GoParser struct{}

func (p *GoParser) Language() string { return "go" }

func (p *GoParser) CanParse(filePath string) bool {
	return strings.ToLower(filepath.Ext(filePath)) == ".go"
}

func (p *GoParser) Parse(filePath string, content []byte) (FileResult, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, content, parser.ParseComments)
	if f == nil {
		return FileResult{}, fmt.Errorf("parse %s: %w", filePath, err)
	}
	// partial parse errors are ok — we work with whatever we got

	h := sha256.Sum256(content)
	hash := hex.EncodeToString(h[:])
	result := FileResult{
		FilePath:    filePath,
		Language:    "go",
		PackageName: f.Name.Name,
		LOC:         bytes.Count(content, []byte("\n")) + 1,
		FileHash:    hash,
	}

	// Build import-alias → path map for resolving call targets
	importAliases := make(map[string]string) // alias → import path
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		} else {
			parts := strings.Split(path, "/")
			alias = parts[len(parts)-1]
		}
		result.Imports = append(result.Imports, Import{Path: path, Alias: alias})
		importAliases[alias] = path
		result.Edges = append(result.Edges, Edge{
			FromKind: "file",
			FromRef:  filePath,
			ToKind:   "file",
			ToRef:    path,
			EdgeKind: "imports",
			Line:     int64(fset.Position(imp.Pos()).Line),
		})
	}

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sym := extractGoFunc(fset, d, filePath, f.Name.Name, content, hash)
			result.Symbols = append(result.Symbols, sym)
			if d.Body != nil {
				edges := extractCallEdges(fset, d.Body, sym.QualifiedName, f.Name.Name, importAliases)
				result.Edges = append(result.Edges, edges...)
			}
		case *ast.GenDecl:
			syms := extractGoGenDecl(fset, d, filePath, f.Name.Name, content, hash)
			result.Symbols = append(result.Symbols, syms...)
		}
	}

	return result, nil
}

func extractGoFunc(fset *token.FileSet, d *ast.FuncDecl, filePath, pkgName string, content []byte, fileHash string) Symbol {
	name := d.Name.Name
	kind := "func"
	receiver := ""
	qualifiedName := pkgName + "." + name

	if d.Recv != nil && len(d.Recv.List) > 0 {
		kind = "method"
		receiverType := exprToString(d.Recv.List[0].Type)
		receiver = receiverType
		// strip pointer for qualified name
		clean := strings.TrimLeft(receiverType, "*")
		qualifiedName = pkgName + "." + clean + "." + name
	}

	startPos := fset.Position(d.Pos())
	endPos := fset.Position(d.End())

	// Signature: everything up to the opening brace
	sigEnd := endPos.Offset
	if d.Body != nil {
		sigEnd = fset.Position(d.Body.Lbrace).Offset
	}
	signature := ""
	if startPos.Offset < sigEnd && sigEnd <= len(content) {
		signature = strings.TrimSpace(string(content[startPos.Offset:sigEnd]))
	}

	bodySnippet := snippetFromContent(content, startPos.Offset, endPos.Offset)
	doc := docText(d.Doc)

	return Symbol{
		Key:           filePath + ":" + qualifiedName,
		FilePath:      filePath,
		Language:      "go",
		Kind:          kind,
		Name:          name,
		QualifiedName: qualifiedName,
		Receiver:      receiver,
		Signature:     signature,
		DocComment:    doc,
		Visibility:    visibility(name),
		BodySnippet:   bodySnippet,
		StartLine:     int64(startPos.Line),
		EndLine:       int64(endPos.Line),
		FileHash:      fileHash,
	}
}

func extractGoGenDecl(fset *token.FileSet, d *ast.GenDecl, filePath, pkgName string, content []byte, fileHash string) []Symbol {
	var out []Symbol

	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			kind := "type"
			switch s.Type.(type) {
			case *ast.StructType:
				kind = "struct"
			case *ast.InterfaceType:
				kind = "interface"
			}
			name := s.Name.Name
			startPos := fset.Position(d.Pos())
			endPos := fset.Position(d.End())
			bodySnippet := snippetFromContent(content, startPos.Offset, endPos.Offset)
			doc := docText(d.Doc)
			if doc == "" {
				doc = docText(s.Comment)
			}
			qualifiedName := pkgName + "." + name

			// Signature: "type Name struct" or "type Name interface"
			sig := ""
			specStart := fset.Position(s.Pos())
			specEnd := fset.Position(s.Type.(ast.Node).Pos())
			if specStart.Offset < specEnd.Offset && specEnd.Offset <= len(content) {
				sig = "type " + strings.TrimSpace(string(content[specStart.Offset:specEnd.Offset]))
			}

			out = append(out, Symbol{
				Key:           filePath + ":" + qualifiedName,
				FilePath:      filePath,
				Language:      "go",
				Kind:          kind,
				Name:          name,
				QualifiedName: qualifiedName,
				Signature:     sig,
				DocComment:    doc,
				Visibility:    visibility(name),
				BodySnippet:   bodySnippet,
				StartLine:     int64(startPos.Line),
				EndLine:       int64(endPos.Line),
				FileHash:      fileHash,
			})

		case *ast.ValueSpec:
			kind := "var"
			if d.Tok == token.CONST {
				kind = "const"
			}
			for _, nameIdent := range s.Names {
				name := nameIdent.Name
				qualifiedName := pkgName + "." + name
				startPos := fset.Position(s.Pos())
				endPos := fset.Position(s.End())
				sig := snippetFromContent(content, startPos.Offset, endPos.Offset)

				out = append(out, Symbol{
					Key:           filePath + ":" + qualifiedName,
					FilePath:      filePath,
					Language:      "go",
					Kind:          kind,
					Name:          name,
					QualifiedName: qualifiedName,
					Signature:     strings.TrimSpace(sig),
					DocComment:    docText(s.Comment),
					Visibility:    visibility(name),
					BodySnippet:   strings.TrimSpace(sig),
					StartLine:     int64(startPos.Line),
					EndLine:       int64(endPos.Line),
					FileHash:      fileHash,
				})
			}
		}
	}
	return out
}

func extractCallEdges(fset *token.FileSet, body *ast.BlockStmt, fromRef, pkgName string, importAliases map[string]string) []Edge {
	var edges []Edge
	seen := make(map[string]bool) // deduplicate per function
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		target := resolveCallTarget(call.Fun, pkgName, importAliases)
		if target == "" {
			return true
		}
		key := fromRef + "→" + target
		if seen[key] {
			return true
		}
		seen[key] = true
		edges = append(edges, Edge{
			FromKind: "symbol",
			FromRef:  fromRef,
			ToKind:   "symbol",
			ToRef:    target,
			EdgeKind: "calls",
			Line:     int64(fset.Position(call.Pos()).Line),
		})
		return true
	})
	return edges
}

func resolveCallTarget(expr ast.Expr, pkgName string, imports map[string]string) string {
	switch e := expr.(type) {
	case *ast.Ident:
		if e.Name == "" {
			return ""
		}
		return pkgName + "." + e.Name
	case *ast.SelectorExpr:
		ident, ok := e.X.(*ast.Ident)
		if !ok {
			return e.Sel.Name
		}
		if importPath, isImport := imports[ident.Name]; isImport {
			parts := strings.Split(importPath, "/")
			pkg := parts[len(parts)-1]
			return pkg + "." + e.Sel.Name
		}
		// method call on local variable — record method name only
		return e.Sel.Name
	case *ast.ParenExpr:
		return resolveCallTarget(e.X, pkgName, imports)
	}
	return ""
}

func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprToString(e.Elt)
	}
	return ""
}

func visibility(name string) string {
	if name == "" {
		return "unexported"
	}
	if unicode.IsUpper(rune(name[0])) {
		return "exported"
	}
	return "unexported"
}

func docText(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	lines := make([]string, 0, len(cg.List))
	for _, c := range cg.List {
		text := strings.TrimPrefix(c.Text, "//")
		text = strings.TrimPrefix(text, "/*")
		text = strings.TrimSuffix(text, "*/")
		lines = append(lines, strings.TrimSpace(text))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func snippetFromContent(content []byte, startOffset, endOffset int) string {
	if startOffset < 0 {
		startOffset = 0
	}
	if endOffset > len(content) {
		endOffset = len(content)
	}
	if startOffset >= endOffset {
		return ""
	}
	s := string(content[startOffset:endOffset])
	const maxSnippet = 4000
	if len(s) > maxSnippet {
		s = s[:maxSnippet] + "\n// ... (truncated)"
	}
	return s
}
