# How to Use agentdb

agentdb pre-indexes your codebase into a local SQLite database — symbols, call graphs, imports, file metadata — so AI agents can query project structure without re-parsing source on every session.

## Installation

```bash
go install github.com/roysland/agentdb@latest
```

Requires Go 1.24+. Pure Go, no CGo required.

**For Python, TypeScript, JavaScript, and Rust support**, install the parsers plugin (requires CGo and a C compiler):

```bash
go install -tags treesitter github.com/roysland/agentdb/plugins/parsers@latest
```

Then place the binary and a `manifest.json` in `~/.agentdb/plugins/agentdb-parsers/`. A template manifest is at `plugins/parsers/manifest.json` in the repo.

## Building Locally

Main binary (pure Go, no CGo):
```bash
go build -o ~/.local/bin/agentdb .
```

Parser plugin:
```bash
CGO_ENABLED=1 go build -tags treesitter -o ~/.local/bin/agentdb-parsers ./plugins/parsers/
```

## Configuration

agentdb reads config from `~/.config/agentdb/config.toml` (or `$XDG_CONFIG_HOME/agentdb/config.toml`).

Precedence: **flags > environment variables > config.toml > built-in defaults**

All config keys use the `AGENTDB_*` convention in both env vars and config.toml.

**Minimal config.toml:**

```toml
AGENTDB_DB_URL = "~/.local/share/agentdb/agentdb.db"
AGENTDB_DB_DRIVER = "auto"
```

## First-Time Setup

Run this once to initialize the database schema:

```bash
agentdb bootstrap
```

Then register and index your project:

```bash
# Register the project root
agentdb codebase register --path /path/to/your/project --name my-project

# Note the codebase ID printed (usually 1 for a fresh database)
agentdb codebase list

# Index source files into chunks
agentdb index --codebase-id 1

# Extract symbols, call graphs, and relationships
agentdb analyze --codebase-id 1
```

Full indexing on a large codebase takes a few minutes. Subsequent runs use `--incremental` (see below) and complete in seconds.

## Keeping the Index Current: Watch Mode

For day-to-day development, run `watch` in the background. It monitors the directory for file changes and triggers incremental re-indexing automatically:

```bash
# Re-index chunks only (fast, no symbol extraction)
agentdb watch --codebase-id 1 --codebase-path /path/to/your/project --analyze=false

# Re-index and re-analyze (chunks + symbols + call graph)
agentdb watch --codebase-id 1 --codebase-path /path/to/your/project
```

Run this in a terminal tab or as a background service while you code. SIGINT/SIGTERM waits for any in-progress re-index to finish before exiting cleanly.

For occasional manual updates instead:

```bash
agentdb index --incremental --codebase-id 1
agentdb analyze --incremental --codebase-id 1
```

Incremental mode uses SHA-256 file hashing to skip unchanged files.

## MCP Server Setup

`agentdb mcp` starts a JSON-RPC stdio server that exposes all capabilities to AI agents via the MCP protocol.

### Claude Code (Claude's CLI)

Add agentdb to your project's MCP config. Create or edit `.claude/mcp.json` in your project root:

```json
{
  "mcpServers": {
    "agentdb": {
      "command": "agentdb",
      "args": ["mcp"]
    }
  }
}
```

Or add it to your global Claude Code config (`~/.claude/mcp.json`) to make it available in all projects.

Restart Claude Code after saving the config. You should see agentdb tools available (find_symbol, search, get_callers, etc.).

### Other MCP Clients

Any MCP-compatible client can use agentdb. Configure the server as:

- **Command:** `agentdb mcp`
- **Transport:** stdio

### Verifying the Connection

Once the MCP server is running, you can ask the agent to call `project_overview` or `list_codebases` to confirm it can reach the database and see your indexed codebase.

## Working Across Git Branches

agentdb is not git-aware — it indexes the files as they exist on disk. Switching branches changes what's on disk, which means the index can go stale.

**The recommended solution: git worktrees.**

Instead of switching branches in a single directory, keep each branch checked out in its own directory. Register each worktree as a separate agentdb codebase. Both stay indexed and up-to-date simultaneously — no re-indexing on branch switch, no stale data.

```bash
# Add a worktree for your feature branch
git worktree add ../my-project-feature my-feature-branch

# Register and index it as its own codebase
agentdb codebase register --path ../my-project-feature --name my-project-feature
agentdb index --codebase-id 2
agentdb analyze --codebase-id 2

# Watch both (in separate terminals or as background jobs)
agentdb watch --codebase-id 1 --codebase-path /path/to/my-project --analyze
agentdb watch --codebase-id 2 --codebase-path ../my-project-feature --analyze
```

Now you can query either codebase by ID, and both indices reflect live file state. When the feature branch is merged and the worktree removed, unregister or simply stop watching it.

```bash
# When done with the feature branch
git worktree remove ../my-project-feature
```

This approach also enables cross-codebase queries via workspaces (see readme for `workspace` commands), which is useful for understanding how a change in one branch would affect callers in main.

## What the MCP Tools Give You

Once the server is running and an agent is connected, the main tools you'll use:

| Tool | What it does |
|------|-------------|
| `find_symbol` | Look up a function, type, or method by name |
| `find_usages` | Find all call sites that reference a symbol |
| `get_callers` / `get_callees` | Traverse the call graph up or down |
| `get_file_symbols` | List all symbols defined in a specific file |
| `search` | Lexical search across code chunks (FTS5/BM25) |
| `locate_issue_impact_area` | Triage a bug description into ranked impact candidates |
| `project_overview` | High-level summary: languages, LOC, packages, top files |
| `codebase_context` | Load README/design docs for session bootstrapping |
| `server_stats` | Runtime metrics: latency, call counts, error rates |

## Troubleshooting

**`agentdb` not found after `go install`**
Ensure `$(go env GOPATH)/bin` is in your `$PATH`.

**MCP tools not appearing in Claude Code**
Check that `agentdb mcp` runs without error in your terminal first. The binary must be on `$PATH` for the MCP client to launch it.

**Index feels stale after large changes**
Run a full (non-incremental) reindex to clear out deleted-file residue:
```bash
agentdb index --codebase-id 1
agentdb analyze --codebase-id 1
```
