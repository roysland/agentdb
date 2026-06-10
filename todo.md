# TODO

- ~~Add a `compare_capabilities` MCP tool~~ ✓

- ~~Extend `--strip-source` to also blank `signature` on both chunks and symbols~~ ✓

- ~~Audit other MCP tools for oversized inline payloads~~ ✓

- ~~Harden MCP tool result serialization so non-finite float scores (`NaN`, `+Inf`, `-Inf`) cannot break editor temp-file materialization~~ ✓ (already in code)

- ~~Add an editor-integration regression check for the transient MCP temp-result path bug~~ ✓

- ~~Evaluate a proprietary-artifact mode for `codebase_context` to optionally suppress sensitive design docs during MCP retrieval~~ ✓

- ~~Investigate db write timeout mismatch (TestBugCondition_WriteTimeout)~~ ✓ — bug was 3s writeTTL; NewConnectionHandle already fixed to 5m; test updated to verify production value

- ~~Investigate `locate_issue_impact_area` ranking bias toward db/sql constants over runtime handler/provider paths~~ ✓ (already in code via `locateIssueConfidenceBonus`)

- ~~Phase in stricter static-analysis gates (errcheck + gosec)~~ — dropped; govet/ineffassign/staticcheck are the right baseline for a local CLI/MCP tool; errcheck noise outweighs signal here

- ~~watch command didn't do proper index and analyze~~ ✓ — fix was already in code (embed provider wired into reindex+runAnalyze, --analyze defaults true when embedding configured); added watcher_test.go to verify both

- ~~Add a "--new" flag to bootstrap, which safely renames the current database and creates a new one.~~ ✓