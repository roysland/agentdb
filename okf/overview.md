### What this project is
AgentDB is a codebase analysis and retrieval engine designed to power LLM-based development agents. It provides a structured, searchable index of source code, symbols, and call graphs, enabling agents to perform semantic search, issue localization, and cross-repository impact analysis. It is intended for developers building AI-assisted coding tools that require deep, context-aware understanding of large, multi-repo projects.

### Architecture
The system follows a layered architecture centered around a SQLite database:
*   **Ingestion Layer:** The `cmd` and `parse` modules extract semantic data (symbols, edges, chunks) from source code, utilizing tree-sitter or Go AST parsers.
*   **Storage Layer:** The `store` and `data/gen` modules provide a type-safe, transactional interface to SQLite, managing codebases, chunks, and cross-repo relationships.
*   **Retrieval Layer:** The `search`, `orient`, and `orient` modules perform FTS5-backed lexical search and heuristic-based issue localization.
*   **Control Flow:** The CLI (`cmd`) acts as the primary orchestrator, managing database lifecycles, triggering incremental indexing, and exposing functionality via an MCP (Model Context Protocol) server for external agent consumption.

### Module map
*   `cmd`: CLI entry point, subcommands, and MCP server implementation.
*   `data/gen`: sqlc-generated type-safe database access layer.
*   `internal/artifact`: SQLite-based import/export of codebase analysis data.
*   `internal/chunk`: Strategies for splitting source code into indexed units (AST-aware or text-based).
*   `internal/config`: Configuration resolution from environment, files, and defaults.
*   `internal/db`: Database connection management, schema bootstrapping, and migration handling.
*   `internal/filefilter`: Path-based filtering and `.gitignore` integration for traversal.
*   `internal/index`: Incremental indexing logic and file-hash delta computation.
*   `internal/observe`: Structured logging and in-memory metrics collection.
*   `internal/orient`: Documentation classification and retrieval for agent context.
*   `internal/parse`: Semantic extraction (symbols/edges) using AST parsers and plugin support.
*   `internal/search`: FTS5 lexical search and symbol-level issue localization.
*   `internal/store`: Repository-pattern abstractions for database entities.

### Getting started
1.  **Build:** Run `go build -tags treesitter ./cmd` to compile the CLI with full AST support.
2.  **Bootstrap:** Initialize the database schema by running `./agentdb bootstrap`.
3.  **Register:** Add a codebase to the database: `./agentdb codebase register /path/to/your/repo`.
4.  **Index:** Run the analysis and indexing pipeline: `./agentdb analyze /path/to/your/repo` followed by `./agentdb index`.
5.  **Serve:** Start the MCP server to connect to your agent: `./agentdb mcp`.

### Key design decisions
*   **SQLite-Centric Persistence:** The system uses SQLite as the primary data store, leveraging WAL mode for concurrency and `ATTACH DATABASE` for efficient, bulk-copying of codebase artifacts without serialization overhead.
*   **Incremental Indexing:** The system computes file-hash deltas to ensure that only changed or new files are processed during re-indexing, significantly reducing overhead for large codebases.
*   **Resilient Parsing:** The AST parser uses a threshold-based error tolerance, falling back to text-based chunking if the tree-sitter parse quality is too low, ensuring the index remains functional even with broken code.
*   **Plugin-First Extensibility:** The parser registry prioritizes external binary plugins over built-in parsers, allowing users to add support for new languages without modifying the core codebase.
*   **Heuristic-Driven Localization:** Issue localization uses a combination of FTS5 BM25 scores and hand-tuned confidence bonuses (e.g., favoring runtime entry points like `handlers` over constants) to rank search results for LLM consumption.