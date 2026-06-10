package parse

// Symbol is a named, addressable entity extracted from source code.
type Symbol struct {
	Key           string // file_path:qualified_name
	FilePath      string
	Language      string
	Kind          string // func | method | type | struct | interface | const | var
	Name          string // simple: "ParseConfig"
	QualifiedName string // pkg-qualified: "config.ParseConfig", "config.Manager.Run"
	Receiver      string // Go methods only: "*Manager"
	Signature     string // declaration up to body open brace
	DocComment    string
	Visibility    string // exported | unexported
	BodySnippet   string // full declaration source, capped at 4000 chars
	StartLine     int64
	EndLine       int64
	FileHash      string
}

// Import is a single import declaration within a source file.
type Import struct {
	Path  string // "net/http"
	Alias string // local name; empty means last segment of Path
}

// Edge is a directed relationship between two nodes (files or symbols).
type Edge struct {
	FromKind string // "file" | "symbol"
	FromRef  string // file_path or qualified_name
	ToKind   string // "file" | "symbol"
	ToRef    string // may be unresolved
	EdgeKind string // imports | calls | uses_type | implements | references
	Line     int64
	Resolved bool // true if ToRef matches a known symbol in the codebase
}

// FileResult holds everything extracted from a single source file.
type FileResult struct {
	FilePath    string
	Language    string
	PackageName string
	Imports     []Import
	LOC         int
	FileHash    string
	Symbols     []Symbol
	Edges       []Edge
}

// Parser is implemented per language.
type Parser interface {
	Language() string
	CanParse(filePath string) bool
	Parse(filePath string, content []byte) (FileResult, error)
}
