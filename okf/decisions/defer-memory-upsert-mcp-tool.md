---
type: Decision
title: Defer memory_upsert MCP tool
description: memory_upsert is implemented in the schema and store layer but not exposed as an MCP tool until a concrete workflow exists
tags: [architecture, decision]
timestamp: 2026-06-28T00:00:00+02:00
---

## Context

agentdb has a `memories` table in the schema and a corresponding store layer for upserting keyed memory facts (content, category, workspace/codebase scope). The original design exposed this as an MCP tool so agents could persist confirmed facts for later retrieval.

The problem is that the workflow for consuming these memories is unspecified. There is no MCP read path that surfaces stored memories to agents in a useful, structured way, and no design for how agents should decide what qualifies as a memory worth persisting. Exposing a write tool without a coherent read/query path creates a footgun: agents may store data with no way to retrieve it, and the storage grows without feedback.

## Decision

`memory_upsert` is commented out of the MCP tool list and the call-site handler. The underlying schema table and store implementation are retained. The tool will be uncommented when:

1. A concrete use case is identified (e.g. annotating vendor artifacts, persisting cross-session agent conclusions).
2. A corresponding read/query MCP tool exists so stored memories can actually be retrieved.
3. The decision criteria for what an agent should persist is specified.

## Alternatives Considered

- **Expose memory_upsert now, design the read path later** — risks agents storing data into a black hole and creates a false impression that memory persistence is functional end-to-end.
- **Delete the schema table and store layer** — premature; the use case is plausible, just not yet concrete enough to commit to a specific interface.
- **Expose both upsert and a naive read-all tool** — punts the design problem to the caller; does not solve the "what should be persisted" question.

## Consequences

- Agents connected via MCP cannot persist memory facts. This is intentional until the workflow is designed.
- The `memories` table exists in the database schema, so databases bootstrapped from the current schema will have the table even though it is unused.
- When the tool is re-enabled, the existing store implementation (`internal/store/memory_repo.go`) can be reused, but the MCP tool description and schema should be revisited based on the concrete use case.
