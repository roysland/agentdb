//go:build treesitter

package parse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// TreeSitterParser is a generic language parser backed by a tree-sitter grammar.
type TreeSitterParser struct {
	lang     *sitter.Language
	langName string
	exts     []string
	parseFn  func(content []byte, root *sitter.Node, filePath, moduleName, fileHash string) ([]Symbol, []Import, []Edge)
}

func (p *TreeSitterParser) Language() string { return p.langName }

func (p *TreeSitterParser) CanParse(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	for _, e := range p.exts {
		if e == ext {
			return true
		}
	}
	return false
}

func (p *TreeSitterParser) Parse(filePath string, content []byte) (FileResult, error) {
	tsParser := sitter.NewParser()
	tsParser.SetLanguage(p.lang)
	tree, err := tsParser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return FileResult{}, fmt.Errorf("tree-sitter parse %s: %w", filePath, err)
	}
	if tree == nil {
		return FileResult{}, fmt.Errorf("tree-sitter returned nil tree for %s", filePath)
	}
	defer tree.Close()

	h := sha256.Sum256(content)
	hash := hex.EncodeToString(h[:])
	moduleName := tsModuleName(filePath)

	symbols, imports, edges := p.parseFn(content, tree.RootNode(), filePath, moduleName, hash)

	return FileResult{
		FilePath:    filePath,
		Language:    p.langName,
		PackageName: moduleName,
		LOC:         bytes.Count(content, []byte("\n")) + 1,
		FileHash:    hash,
		Imports:     imports,
		Symbols:     symbols,
		Edges:       edges,
	}, nil
}

// tsModuleName derives a module/package name from the file path (filename without extension).
func tsModuleName(filePath string) string {
	base := filepath.Base(filePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// tsText returns the source text of a node. Returns "" for nil nodes.
func tsText(content []byte, node *sitter.Node) string {
	if node == nil {
		return ""
	}
	return node.Content(content)
}

// tsSnippet returns the source text of a node, capped at 4000 chars.
func tsSnippet(content []byte, node *sitter.Node) string {
	s := tsText(content, node)
	const maxSnippet = 4000
	if len(s) > maxSnippet {
		s = s[:maxSnippet] + "\n// ... (truncated)"
	}
	return s
}

// tsLine returns the 1-indexed start line of a node.
func tsLine(node *sitter.Node) int64 { return int64(node.StartPoint().Row + 1) }

// tsEndLine returns the 1-indexed end line of a node.
func tsEndLine(node *sitter.Node) int64 { return int64(node.EndPoint().Row + 1) }

// tsWalkChildren calls fn for every direct child of node.
func tsWalkChildren(node *sitter.Node, fn func(child *sitter.Node)) {
	for i := 0; i < int(node.ChildCount()); i++ {
		fn(node.Child(i))
	}
}

// tsFirstNamedChild returns the first named child matching one of the given types.
func tsFirstNamedChild(node *sitter.Node, types ...string) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		if !c.IsNamed() {
			continue
		}
		for _, t := range types {
			if c.Type() == t {
				return c
			}
		}
	}
	return nil
}

// tsHasChild returns true if any direct child has the given type.
func tsHasChild(node *sitter.Node, typ string) bool {
	return tsFirstNamedChild(node, typ) != nil
}

// tsSigUpTo returns the source up to but not including the first child of the given type.
func tsSigUpTo(content []byte, node *sitter.Node, bodyType string) string {
	end := node.EndByte()
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		if c.Type() == bodyType {
			end = c.StartByte()
			break
		}
	}
	if end > uint32(len(content)) {
		end = uint32(len(content))
	}
	return strings.TrimSpace(string(content[node.StartByte():end]))
}
