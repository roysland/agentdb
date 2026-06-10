//go:build treesitter

package chunk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/roysland/agentdb/internal/parse"
)

// ASTChunkerConfig configures AST-aware chunking behavior.
type ASTChunkerConfig struct {
	MaxChunkLines int // Maximum lines per chunk before subdivision (default: 100)
}

// DefaultASTChunkerConfig returns the default AST chunker configuration.
func DefaultASTChunkerConfig() ASTChunkerConfig {
	return ASTChunkerConfig{
		MaxChunkLines: 100,
	}
}

// ASTChunker splits source files at tree-sitter AST node boundaries.
type ASTChunker struct {
	config  ASTChunkerConfig
	parsers []parse.Parser
}

// NewASTChunker creates an AST chunker with the given parsers and config.
func NewASTChunker(parsers []parse.Parser, config ASTChunkerConfig) *ASTChunker {
	if config.MaxChunkLines <= 0 {
		config.MaxChunkLines = 100
	}
	return &ASTChunker{
		config:  config,
		parsers: parsers,
	}
}

// ChunkFile splits a file at AST boundaries. Returns chunks with kind/name/signature metadata.
// Falls back to fixed-line chunking if no parser is available or parsing fails.
func (ac *ASTChunker) ChunkFile(filePath string, content []byte, language string) ([]Chunk, error) {
	// Compute file hash (SHA-256)
	h := sha256.Sum256(content)
	fileHash := hex.EncodeToString(h[:])

	// Find a parser for this language
	var parser parse.Parser
	for _, p := range ac.parsers {
		if p.CanParse(filePath) {
			parser = p
			break
		}
	}

	// Fall back to fixed-line chunking if no parser available
	if parser == nil {
		return ac.fallbackChunk(filePath, content, language, fileHash)
	}

	// Parse with tree-sitter to get the AST
	tree, err := ac.parseTree(content, parser)
	if err != nil || tree == nil {
		// Parsing failed — fall back to fixed-line chunking
		return ac.fallbackChunk(filePath, content, language, fileHash)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		return ac.fallbackChunk(filePath, content, language, fileHash)
	}

	// Walk top-level AST nodes and create chunks at node boundaries
	chunks := ac.walkTopLevel(content, root, filePath, language, fileHash)

	// If no chunks were produced (e.g., empty file or only whitespace), fall back
	if len(chunks) == 0 {
		return ac.fallbackChunk(filePath, content, language, fileHash)
	}

	return chunks, nil
}

// walkTopLevel walks the top-level children of the root node and creates chunks.
// It ensures the round-trip property: concatenating all snippets reproduces the original file.
func (ac *ASTChunker) walkTopLevel(content []byte, root *sitter.Node, filePath, language, fileHash string) []Chunk {
	var chunks []Chunk
	childCount := int(root.ChildCount())

	// Track the last byte position we've covered to ensure no gaps
	var lastEnd uint32

	for i := 0; i < childCount; i++ {
		child := root.Child(i)
		if child == nil {
			continue
		}

		nodeStart := child.StartByte()
		nodeEnd := child.EndByte()

		// Capture any gap between the last node and this one (whitespace, comments between nodes)
		if nodeStart > lastEnd {
			gapSnippet := string(content[lastEnd:nodeStart])
			if len(chunks) > 0 {
				// Append gap to the previous chunk's snippet
				prevIdx := len(chunks) - 1
				chunks[prevIdx].Snippet += gapSnippet
				// Update end line of previous chunk
				gapLines := strings.Count(gapSnippet, "\n")
				chunks[prevIdx].EndLine += int64(gapLines)
			} else {
				// Gap before first node — create a leading chunk
				if strings.TrimSpace(gapSnippet) != "" {
					startLine := int64(1)
					endLine := startLine + int64(strings.Count(gapSnippet, "\n"))
					chunks = append(chunks, Chunk{
						Key:       fmt.Sprintf("%s:%d-%d", filePath, startLine, endLine),
						FilePath:  filePath,
						Language:  language,
						Kind:      "module",
						Name:      filepath.Base(filePath),
						Snippet:   gapSnippet,
						StartLine: startLine,
						EndLine:   endLine,
						FileHash:  fileHash,
					})
				}
			}
		}

		// Chunk this node (may subdivide if too large)
		nodeChunks := ac.chunkNode(content, child, "", filePath, language, fileHash)
		chunks = append(chunks, nodeChunks...)

		if nodeEnd > lastEnd {
			lastEnd = nodeEnd
		}
	}

	// Capture any trailing content after the last node
	if lastEnd < uint32(len(content)) {
		trailingSnippet := string(content[lastEnd:])
		if len(chunks) > 0 {
			prevIdx := len(chunks) - 1
			chunks[prevIdx].Snippet += trailingSnippet
			trailingLines := strings.Count(trailingSnippet, "\n")
			chunks[prevIdx].EndLine += int64(trailingLines)
		} else if strings.TrimSpace(trailingSnippet) != "" {
			startLine := int64(countNewlines(content[:lastEnd])) + 1
			endLine := startLine + int64(strings.Count(trailingSnippet, "\n"))
			chunks = append(chunks, Chunk{
				Key:       fmt.Sprintf("%s:%d-%d", filePath, startLine, endLine),
				FilePath:  filePath,
				Language:  language,
				Kind:      "module",
				Name:      filepath.Base(filePath),
				Snippet:   trailingSnippet,
				StartLine: startLine,
				EndLine:   endLine,
				FileHash:  fileHash,
			})
		}
	}

	return chunks
}

// chunkNode recursively chunks an AST node, subdividing if it exceeds MaxChunkLines.
func (ac *ASTChunker) chunkNode(content []byte, node *sitter.Node, parentSig string, filePath, language, fileHash string) []Chunk {
	startLine := int64(node.StartPoint().Row + 1)
	endLine := int64(node.EndPoint().Row + 1)
	nodeLines := int(endLine - startLine + 1)

	// Extract metadata from the node
	kind := ac.nodeKind(node)
	name := ac.nodeName(content, node)
	signature := ac.nodeSignature(content, node)

	// If parent signature is set, prepend it
	if parentSig != "" && signature == "" {
		signature = parentSig
	} else if parentSig != "" && signature != "" {
		signature = parentSig + " > " + signature
	}

	// If the node fits within MaxChunkLines, emit it as a single chunk
	if nodeLines <= ac.config.MaxChunkLines {
		snippet := string(content[node.StartByte():node.EndByte()])
		return []Chunk{{
			Key:       fmt.Sprintf("%s:%d-%d", filePath, startLine, endLine),
			FilePath:  filePath,
			Language:  language,
			Kind:      kind,
			Name:      name,
			Signature: signature,
			Snippet:   snippet,
			StartLine: startLine,
			EndLine:   endLine,
			FileHash:  fileHash,
		}}
	}

	// Node exceeds MaxChunkLines — subdivide at nested block boundaries
	return ac.subdivideNode(content, node, signature, filePath, language, fileHash, kind, name)
}

// subdivideNode splits a large node into sub-chunks at child boundaries.
func (ac *ASTChunker) subdivideNode(content []byte, node *sitter.Node, parentSig string, filePath, language, fileHash string, parentKind, parentName string) []Chunk {
	var chunks []Chunk
	childCount := int(node.ChildCount())

	// If the node has no children or only unnamed children, emit as a single large chunk
	hasNamedChildren := false
	for i := 0; i < childCount; i++ {
		if node.Child(i).IsNamed() {
			hasNamedChildren = true
			break
		}
	}

	if !hasNamedChildren || childCount == 0 {
		// Can't subdivide further — emit as one chunk
		startLine := int64(node.StartPoint().Row + 1)
		endLine := int64(node.EndPoint().Row + 1)
		snippet := string(content[node.StartByte():node.EndByte()])
		return []Chunk{{
			Key:       fmt.Sprintf("%s:%d-%d", filePath, startLine, endLine),
			FilePath:  filePath,
			Language:  language,
			Kind:      parentKind,
			Name:      parentName,
			Signature: parentSig,
			Snippet:   snippet,
			StartLine: startLine,
			EndLine:   endLine,
			FileHash:  fileHash,
		}}
	}

	// Group consecutive children into sub-chunks that fit within MaxChunkLines
	var currentSnippet strings.Builder
	var currentStartLine int64
	var currentEndLine int64
	var lastEnd uint32 = node.StartByte()
	started := false

	flushChunk := func() {
		if !started || currentSnippet.Len() == 0 {
			return
		}
		chunks = append(chunks, Chunk{
			Key:       fmt.Sprintf("%s:%d-%d", filePath, currentStartLine, currentEndLine),
			FilePath:  filePath,
			Language:  language,
			Kind:      parentKind,
			Name:      parentName,
			Signature: parentSig,
			Snippet:   currentSnippet.String(),
			StartLine: currentStartLine,
			EndLine:   currentEndLine,
			FileHash:  fileHash,
		})
		currentSnippet.Reset()
		started = false
	}

	for i := 0; i < childCount; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}

		childStartByte := child.StartByte()
		childEndByte := child.EndByte()
		childStartLine := int64(child.StartPoint().Row + 1)
		childEndLine := int64(child.EndPoint().Row + 1)

		// Include any gap between last position and this child
		gap := ""
		if childStartByte > lastEnd {
			gap = string(content[lastEnd:childStartByte])
		}

		childText := gap + string(content[childStartByte:childEndByte])
		childLines := strings.Count(childText, "\n") + 1

		if !started {
			// Start a new sub-chunk
			currentSnippet.WriteString(childText)
			if gap != "" {
				// Adjust start line to account for gap
				gapStartLine := int64(countNewlines(content[:lastEnd])) + 1
				currentStartLine = gapStartLine
			} else {
				currentStartLine = childStartLine
			}
			currentEndLine = childEndLine
			started = true
		} else {
			// Check if adding this child would exceed MaxChunkLines
			currentLines := int(currentEndLine - currentStartLine + 1)
			if currentLines+childLines > ac.config.MaxChunkLines {
				// Flush current chunk and start a new one
				flushChunk()
				currentSnippet.WriteString(childText)
				if gap != "" {
					gapStartLine := int64(countNewlines(content[:lastEnd])) + 1
					currentStartLine = gapStartLine
				} else {
					currentStartLine = childStartLine
				}
				currentEndLine = childEndLine
				started = true
			} else {
				// Add to current chunk
				currentSnippet.WriteString(childText)
				currentEndLine = childEndLine
			}
		}

		lastEnd = childEndByte
	}

	// Include any trailing content within the node after the last child
	if lastEnd < node.EndByte() {
		trailing := string(content[lastEnd:node.EndByte()])
		if started {
			currentSnippet.WriteString(trailing)
			trailingLines := strings.Count(trailing, "\n")
			currentEndLine += int64(trailingLines)
		} else {
			currentSnippet.WriteString(trailing)
			currentStartLine = int64(countNewlines(content[:lastEnd])) + 1
			currentEndLine = currentStartLine + int64(strings.Count(trailing, "\n"))
			started = true
		}
	}

	// Flush remaining
	flushChunk()

	return chunks
}

// parseTree uses tree-sitter to parse the content. It finds the appropriate
// tree-sitter language from the parser and creates a parse tree.
func (ac *ASTChunker) parseTree(content []byte, parser parse.Parser) (*sitter.Tree, error) {
	// We need to get the tree-sitter language from the parser.
	// The parsers in this project use TreeSitterParser which has a lang field.
	// We'll parse the file using the parser's Parse method to validate it works,
	// then re-parse with tree-sitter directly for AST walking.
	// However, we don't have direct access to the sitter.Language from the Parser interface.
	// Instead, we'll use the parser to parse and get a FileResult, then re-parse with
	// a fresh tree-sitter parser using the language detection.

	// Since we can't access the internal tree-sitter language from the Parser interface,
	// we'll use a different approach: parse the content using the available tree-sitter
	// languages based on what the parser reports.
	lang := parser.Language()
	tsLang := getTreeSitterLanguage(lang)
	if tsLang == nil {
		return nil, fmt.Errorf("no tree-sitter language for %s", lang)
	}

	tsParser := sitter.NewParser()
	tsParser.SetLanguage(tsLang)
	tree, err := tsParser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse: %w", err)
	}
	return tree, nil
}

// nodeKind maps a tree-sitter node type to a chunk kind.
func (ac *ASTChunker) nodeKind(node *sitter.Node) string {
	switch node.Type() {
	// Go
	case "function_declaration", "function_definition":
		return "function"
	case "method_declaration", "method_definition":
		return "method"
	case "type_declaration", "type_spec", "class_declaration", "class_definition":
		return "class"
	case "interface_declaration":
		return "interface"

	// Python
	case "decorated_definition":
		// Look inside for the actual definition
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			switch child.Type() {
			case "function_definition":
				return "function"
			case "class_definition":
				return "class"
			}
		}
		return "function"

	// TypeScript/JavaScript
	case "export_statement":
		// Look inside for the actual declaration
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			switch child.Type() {
			case "function_declaration":
				return "function"
			case "class_declaration":
				return "class"
			case "interface_declaration":
				return "interface"
			case "lexical_declaration", "variable_declaration":
				return "function" // likely arrow function
			case "type_alias_declaration":
				return "type"
			}
		}
		return "module"

	case "lexical_declaration", "variable_declaration":
		return "function" // const foo = () => {} pattern

	case "type_alias_declaration":
		return "type"

	// Rust
	case "function_item", "impl_item":
		return "function"
	case "struct_item":
		return "class"
	case "enum_item":
		return "type"
	case "trait_item":
		return "interface"

	// Generic
	case "import_statement", "import_from_statement", "use_declaration":
		return "import"
	case "comment", "line_comment", "block_comment":
		return "comment"

	default:
		return "module"
	}
}

// nodeName extracts the symbol name from an AST node.
func (ac *ASTChunker) nodeName(content []byte, node *sitter.Node) string {
	// Try common field names for the symbol name
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return nameNode.Content(content)
	}

	// For decorated definitions, look inside
	if node.Type() == "decorated_definition" {
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "function_definition" || child.Type() == "class_definition" {
				if nameNode := child.ChildByFieldName("name"); nameNode != nil {
					return nameNode.Content(content)
				}
			}
		}
	}

	// For export statements, look inside for the declaration
	if node.Type() == "export_statement" {
		if decl := node.ChildByFieldName("declaration"); decl != nil {
			if nameNode := decl.ChildByFieldName("name"); nameNode != nil {
				return nameNode.Content(content)
			}
		}
		// Check direct children
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.IsNamed() {
				if nameNode := child.ChildByFieldName("name"); nameNode != nil {
					return nameNode.Content(content)
				}
			}
		}
	}

	// For Go type declarations (type_declaration wraps type_spec)
	if node.Type() == "type_declaration" {
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "type_spec" {
				if nameNode := child.ChildByFieldName("name"); nameNode != nil {
					return nameNode.Content(content)
				}
			}
		}
	}

	// For variable declarations, get the first declarator name
	if node.Type() == "lexical_declaration" || node.Type() == "variable_declaration" {
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "variable_declarator" {
				if nameNode := child.ChildByFieldName("name"); nameNode != nil {
					return nameNode.Content(content)
				}
			}
		}
	}

	return ""
}

// nodeSignature extracts the declaration signature from an AST node.
// Returns the text up to (but not including) the body block.
func (ac *ASTChunker) nodeSignature(content []byte, node *sitter.Node) string {
	// Body field names vary by language/node type
	bodyTypes := []string{"body", "block", "statement_block", "class_body", "interface_body"}

	// For export statements, look inside
	targetNode := node
	if node.Type() == "export_statement" {
		if decl := node.ChildByFieldName("declaration"); decl != nil {
			targetNode = decl
		} else {
			// Check direct children for declarations
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				switch child.Type() {
				case "function_declaration", "class_declaration", "interface_declaration",
					"function_definition", "class_definition":
					targetNode = child
				}
			}
		}
	}

	// For decorated definitions, find the inner definition
	if targetNode.Type() == "decorated_definition" {
		for i := 0; i < int(targetNode.ChildCount()); i++ {
			child := targetNode.Child(i)
			if child.Type() == "function_definition" || child.Type() == "class_definition" {
				targetNode = child
				break
			}
		}
	}

	// Find the body node and return text up to it
	for i := 0; i < int(targetNode.ChildCount()); i++ {
		child := targetNode.Child(i)
		for _, bt := range bodyTypes {
			if child.Type() == bt {
				// Return text from node start to body start
				sigEnd := child.StartByte()
				sigStart := targetNode.StartByte()
				if sigEnd > sigStart && sigEnd <= uint32(len(content)) {
					sig := strings.TrimSpace(string(content[sigStart:sigEnd]))
					// Cap signature length
					if len(sig) > 200 {
						sig = sig[:200]
					}
					return sig
				}
			}
		}
	}

	// For nodes without a body (imports, type aliases, etc.), use the first line
	nodeText := string(content[targetNode.StartByte():targetNode.EndByte()])
	if idx := strings.Index(nodeText, "\n"); idx > 0 {
		sig := strings.TrimSpace(nodeText[:idx])
		if len(sig) > 200 {
			sig = sig[:200]
		}
		return sig
	}

	// Short node — use the whole text as signature
	sig := strings.TrimSpace(nodeText)
	if len(sig) > 200 {
		sig = sig[:200]
	}
	return sig
}

// fallbackChunk uses fixed-line chunking when no parser is available.
// This is the same strategy as the existing ChunkFile function but operates on
// in-memory content rather than reading from disk.
func (ac *ASTChunker) fallbackChunk(filePath string, content []byte, language, fileHash string) ([]Chunk, error) {
	lines := strings.Split(string(content), "\n")
	var chunks []Chunk

	linesPerChunk := ac.config.MaxChunkLines
	if linesPerChunk <= 0 {
		linesPerChunk = 100
	}

	for i := 0; i < len(lines); i += linesPerChunk {
		startLine := int64(i + 1)
		endLine := int64(i + linesPerChunk)
		if endLine > int64(len(lines)) {
			endLine = int64(len(lines))
		}

		snippet := strings.Join(lines[i:minInt(i+linesPerChunk, len(lines))], "\n")
		if strings.TrimSpace(snippet) == "" {
			continue
		}

		chunk := Chunk{
			FilePath:  filePath,
			Language:  language,
			Kind:      "code",
			Name:      fmt.Sprintf("%s:%d-%d", filepath.Base(filePath), startLine, endLine),
			Snippet:   snippet,
			StartLine: startLine,
			EndLine:   endLine,
			FileHash:  fileHash,
		}
		chunk.Key = fmt.Sprintf("%s:%d-%d", filePath, startLine, endLine)
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// countNewlines counts the number of newline characters in a byte slice.
func countNewlines(data []byte) int {
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	return count
}

// minInt returns the smaller of two integers.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
