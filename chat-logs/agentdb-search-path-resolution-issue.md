
I’m rerunning the exact MCP search reproduction against the rebuilt binary to see whether the response still spills into an unreadable session-resource file.

I could not reproduce it after the rebuild and restart.

The same mcp_agentdb_search call for codebase_id: 1 and query todo now returns inline structured results directly in the tool response. It no longer returns a workspaceStorage/.../content.json path, so there is no unresolved temp file to chase through read_file.

The response shape also matches the mitigation in cmd/mcp.go: snippets are truncated inline with ... (truncated), which keeps the payload small enough to stay in-band. That means the original bug appears fixed in the rebuilt MCP server.

The bug is not reproducible now with the current AgentDB behavior.
Good job
The bug is no longer reproducible with the rebuilt AgentDB MCP server. The search response now stays inline and no longer returns an unresolved content.json path.

The fix is holding in the live workflow, so this is in good shape.