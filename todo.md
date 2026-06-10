# TODO

- Add a `compare_capabilities` MCP tool: given two codebase IDs, diff their symbol inventories by file-path domain (e.g., handlers/, models/) and return implemented/partial/missing capability groups. Foundation for feature-parity analysis between a legacy codebase and a current one. **Safety constraint:** output must be limited to name, kind, file-path domain, and presence/absence — no `signature` field — so it stays a capability-presence indicator and does not become a bulk API-interface enumerator for closed-source artifacts.

- Extend `--strip-source` to also blank `signature` on both chunks and symbols. Currently `snippet`, `doc_comment`, and `body_snippet` are stripped, but `signature` is retained. Function signatures can reveal business logic, data model design, and parameter semantics (e.g., fraud scoring thresholds, internal type names) without any source body. This is the primary remaining leakage surface for proprietary artifacts distributed without source.

- Audit other MCP tools for oversized inline payloads that trigger Copilot Chat session-resource spill files; compact verbose text fields before transport so results stay editor-readable.

- Harden MCP tool result serialization so non-finite float scores (`NaN`, `+Inf`, `-Inf`) cannot break editor temp-file materialization for large search responses.

- Add an editor-integration regression check for the transient MCP temp-result path bug: when `search` produces a large response, the editor-reported `content.json` path should resolve and remain readable end-to-end after MCP restart/build changes.

- Evaluate a proprietary-artifact mode for `codebase_context` to optionally suppress sensitive design docs during MCP retrieval.

- Investigate db write timeout mismatch surfaced by TestBugCondition_WriteTimeout in internal/db (observed ~3s effective deadline vs expected long-running write budget).

- Investigate `locate_issue_impact_area` ranking bias toward db/sql constants over runtime handler/provider paths when lexical hits dominate.

- Phase in stricter static-analysis gates: re-enable errcheck and gosec after triaging current repository-wide findings and adding targeted suppressions where appropriate.
