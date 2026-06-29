# Architecture Decision Records

Directory of design and architecture choices.

* [Automatic codebase indexing and watching daemon](./automatic-codebase-indexing-and-watching.md) - Introduce a lightweight daemon and systemd user service to automate codebase discovery, registration, indexing, and live watching.
* [Defer memory_upsert MCP tool](./defer-memory-upsert-mcp-tool.md) - memory_upsert is implemented in the schema and store layer but not exposed as an MCP tool until a concrete workflow exists
* [Drop embedding search support](./drop-embedding-search-support.md) - TODO — review commit 95703f86 and fill in
* [Drop embedding/semantic search support](./drop-embedding-semantic-search.md) - Removed all embedding infrastructure and semantic search in favour of FTS5-only lexical search
