package cmd

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/orient"
	"github.com/roysland/agentdb/internal/search"
	"github.com/roysland/agentdb/internal/store"
)

func newSearchCmd(ctx context.Context) *cobra.Command {
	var query string
	var codebaseID int64
	var source string
	var limit int

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search indexed code chunks using BM25 lexical ranking",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(query) == "" {
				return errors.New("--query is required")
			}
			if source != "memories" && source != "chunks" && source != "both" {
				return errors.New("--source must be one of memories|chunks|both")
			}
			if (source == "chunks" || source == "both") && codebaseID <= 0 {
				return errors.New("--codebase-id is required when source includes chunks")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			return runSearch(ctx, conn, query, source, codebaseID, limit)
		},
	}

	cmd.Flags().StringVar(&query, "query", "", "Search query")
	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID to search")
	cmd.Flags().StringVar(&source, "source", "chunks", "Source to search: memories|chunks|both")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of results")

	return cmd
}

func runSearch(ctx context.Context, conn *sql.DB, query, source string, codebaseID int64, limit int) error {
	hits := make([]map[string]any, 0)
	var warning string

	if source == "memories" || source == "both" {
		memRepo := store.NewMemoryRepo(conn)
		memHits, err := memRepo.SearchLexical(ctx, query, "", limit, 0, codebaseID)
		if err != nil {
			return err
		}
		for _, m := range memHits {
			hits = append(hits, map[string]any{
				"source":   "memory",
				"id":       m.ID,
				"content":  m.Content,
				"category": m.Category,
			})
		}
	}

	if source == "chunks" || source == "both" {
		usedFallback := false

		fts, ftsErr := search.NewFTS5Search(conn, nil)
		if ftsErr == nil {
			if err := fts.EnsureIndex(ctx); err != nil {
				ftsErr = err
			}
		}

		if ftsErr == nil && fts.IsAvailable(ctx) {
			ftsResults, err := fts.SearchLexical(ctx, query, codebaseID, limit)
			if err != nil {
				ftsErr = err
			} else {
				for _, r := range ftsResults {
					hits = append(hits, map[string]any{
						"source":      "chunk",
						"id":          r.ChunkID,
						"file_path":   r.FilePath,
						"name":        r.Name,
						"kind":        r.Kind,
						"start_line":  r.StartLine,
						"end_line":    r.EndLine,
						"snippet":     r.Snippet,
						"codebase_id": codebaseID,
						"bm25_score":  r.BM25Score,
					})
				}
			}
		}

		if ftsErr != nil {
			usedFallback = true
			warning = "FTS5 index unavailable; using in-memory fallback"
			chunkRepo := store.NewChunkRepo(conn)
			chunks, err := chunkRepo.GetByCodebase(ctx, codebaseID)
			if err != nil {
				return err
			}
			queryLower := strings.ToLower(query)
			for _, c := range chunks {
				if strings.Contains(strings.ToLower(c.Snippet), queryLower) ||
					strings.Contains(strings.ToLower(c.Name), queryLower) {
					hits = append(hits, map[string]any{
						"source":      "chunk",
						"id":          c.ID,
						"codebase_id": c.CodebaseID,
						"file_path":   c.FilePath,
						"name":        c.Name,
						"kind":        c.Kind,
						"start_line":  c.StartLine,
						"end_line":    c.EndLine,
						"snippet":     c.Snippet,
					})
				}
			}
		}

		_ = usedFallback
	}

	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}

	result := map[string]any{
		"query":   query,
		"source":  source,
		"count":   len(hits),
		"results": hits,
	}
	if warning != "" {
		result["warning"] = warning
	}

	return printJSON(result)
}

func newFindSymbolCmd(ctx context.Context) *cobra.Command {
	var name string
	var codebaseID int64
	var kind string

	cmd := &cobra.Command{
		Use:   "find-symbol",
		Short: "Find a symbol by name in the indexed codebase",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return errors.New("--name is required")
			}
			if codebaseID <= 0 {
				return errors.New("--codebase-id is required")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewSymbolRepo(conn)
			symbols, err := repo.FindByName(ctx, codebaseID, name)
			if err != nil {
				return err
			}

			if kind != "" {
				filtered := symbols[:0]
				for _, s := range symbols {
					if s.Kind == kind {
						filtered = append(filtered, s)
					}
				}
				symbols = filtered
			}

			return printJSON(map[string]any{
				"name":    name,
				"count":   len(symbols),
				"symbols": symbols,
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Symbol name or qualified name to search for")
	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID")
	cmd.Flags().StringVar(&kind, "kind", "", "Filter by kind: func|method|struct|interface|type|const|var")

	return cmd
}

func newFindUsagesCmd(ctx context.Context) *cobra.Command {
	var name string
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "find-usages",
		Short: "Find all references to a symbol across the codebase",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return errors.New("--name is required")
			}
			if codebaseID <= 0 {
				return errors.New("--codebase-id is required")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewEdgeRepo(conn)
			edges, err := repo.FindUsages(ctx, codebaseID, name)
			if err != nil {
				return err
			}

			return printJSON(map[string]any{
				"name":   name,
				"count":  len(edges),
				"usages": edges,
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Symbol name to find usages for")
	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID")

	return cmd
}

func newGetCallersCmd(ctx context.Context) *cobra.Command {
	var name string
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "get-callers",
		Short: "List all functions that call a given symbol",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return errors.New("--name is required")
			}
			if codebaseID <= 0 {
				return errors.New("--codebase-id is required")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewEdgeRepo(conn)
			edges, err := repo.GetCallers(ctx, codebaseID, name)
			if err != nil {
				return err
			}

			return printJSON(map[string]any{
				"target":  name,
				"count":   len(edges),
				"callers": edges,
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Symbol name to find callers for")
	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID")

	return cmd
}

func newGetCalleesCmd(ctx context.Context) *cobra.Command {
	var qualifiedName string
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "get-callees",
		Short: "List all symbols called by a given function",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(qualifiedName) == "" {
				return errors.New("--qualified-name is required")
			}
			if codebaseID <= 0 {
				return errors.New("--codebase-id is required")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewEdgeRepo(conn)
			edges, err := repo.GetCallees(ctx, codebaseID, qualifiedName)
			if err != nil {
				return err
			}

			return printJSON(map[string]any{
				"from":     qualifiedName,
				"count":    len(edges),
				"callees":  edges,
			})
		},
	}

	cmd.Flags().StringVar(&qualifiedName, "qualified-name", "", "Fully qualified function name, e.g. config.ParseConfig")
	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID")

	return cmd
}

func newGetFileSymbolsCmd(ctx context.Context) *cobra.Command {
	var filePath string
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "get-file-symbols",
		Short: "List all symbols defined in a file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(filePath) == "" {
				return errors.New("--file-path is required")
			}
			if codebaseID <= 0 {
				return errors.New("--codebase-id is required")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewSymbolRepo(conn)
			symbols, err := repo.GetByFile(ctx, codebaseID, filePath)
			if err != nil {
				return err
			}

			return printJSON(map[string]any{
				"file_path": filePath,
				"count":     len(symbols),
				"symbols":   symbols,
			})
		},
	}

	cmd.Flags().StringVar(&filePath, "file-path", "", "Relative file path within the codebase")
	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID")

	return cmd
}

func newGetImportsCmd(ctx context.Context) *cobra.Command {
	var filePath string
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "get-imports",
		Short: "List all imports and dependencies for a file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(filePath) == "" {
				return errors.New("--file-path is required")
			}
			if codebaseID <= 0 {
				return errors.New("--codebase-id is required")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewEdgeRepo(conn)
			edges, err := repo.GetImports(ctx, codebaseID, filePath)
			if err != nil {
				return err
			}

			imports := make([]string, 0, len(edges))
			for _, e := range edges {
				imports = append(imports, e.ToRef)
			}

			return printJSON(map[string]any{
				"file_path": filePath,
				"count":     len(imports),
				"imports":   imports,
			})
		},
	}

	cmd.Flags().StringVar(&filePath, "file-path", "", "Relative file path within the codebase")
	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID")

	return cmd
}

func newProjectOverviewCmd(ctx context.Context) *cobra.Command {
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "project-overview",
		Short: "Show a structural summary of a codebase (files, symbols, packages)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if codebaseID <= 0 {
				return errors.New("--codebase-id is required")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			overview, err := buildProjectOverview(ctx, conn, codebaseID)
			if err != nil {
				return err
			}

			return printJSON(overview)
		},
	}

	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID")

	return cmd
}

// buildProjectOverview loads the structural summary for a codebase (files,
// symbols, packages). Shared by `project-overview` and the no-docs fallback
// path in `codebase-context`.
func buildProjectOverview(ctx context.Context, conn *sql.DB, codebaseID int64) (map[string]any, error) {
	sfRepo := store.NewSourceFileRepo(conn)
	symRepo := store.NewSymbolRepo(conn)

	fileStats, err := sfRepo.Stats(ctx, codebaseID)
	if err != nil {
		return nil, err
	}
	symbolStats, err := symRepo.Stats(ctx, codebaseID)
	if err != nil {
		return nil, err
	}
	packages, err := sfRepo.PackageList(ctx, codebaseID)
	if err != nil {
		return nil, err
	}
	topFiles, err := symRepo.TopFilesBySymbolCount(ctx, codebaseID, 10)
	if err != nil {
		return nil, err
	}
	entryPoints, err := symRepo.ExportedFuncs(ctx, codebaseID, 20)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"codebase_id":    codebaseID,
		"files":          fileStats,
		"symbols":        symbolStats,
		"packages":       packages,
		"top_files":      topFiles,
		"exported_funcs": entryPoints,
	}, nil
}

func newCodebaseContextCmd(ctx context.Context) *cobra.Command {
	var codebaseID int64
	var workspaceID int64

	cmd := &cobra.Command{
		Use:   "codebase-context",
		Short: "Show README/design/agent-guidance docs for a codebase (session orientation)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if codebaseID <= 0 && workspaceID <= 0 {
				return errors.New("one of --codebase-id or --workspace-id is required")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			codebaseIDs, err := resolveScopedCodebaseIDs(ctx, conn, codebaseID, workspaceID)
			if err != nil {
				return err
			}

			catalogRepo := store.NewCatalogRepo(conn)
			orientCfg := orient.DefaultConfig()
			if len(codebaseIDs) > 0 {
				cb, err := catalogRepo.GetByID(ctx, codebaseIDs[0])
				if err == nil && cb.RootPath != "" {
					if loaded, loadErr := orient.Load(cb.RootPath, nil); loadErr == nil {
						orientCfg = loaded
					}
				}
			}

			docs, err := orient.Retrieve(ctx, conn, orient.RetrieveConfig{
				CodebaseIDs: codebaseIDs,
				Config:      orientCfg,
			})
			if err != nil {
				return err
			}

			if len(codebaseIDs) > 0 {
				if policy, ok := loadCodebasePolicyMetadata(ctx, conn, codebaseIDs[0]); ok {
					if closedSource, _ := policy["closed_source"].(bool); closedSource {
						safeTypes := map[orient.DocType]bool{
							orient.DocTypeReadme:            true,
							orient.DocTypeContributing:      true,
							orient.DocTypeAgentInstructions: true,
						}
						filtered := docs[:0]
						for _, doc := range docs {
							if safeTypes[doc.DocType] {
								filtered = append(filtered, doc)
							}
						}
						docs = filtered
					}
				}
			}

			if len(docs) == 0 {
				overview, err := buildProjectOverview(ctx, conn, codebaseIDs[0])
				if err != nil {
					return err
				}
				return printJSON(map[string]any{
					"fallback":    true,
					"codebase_id": codebaseIDs[0],
					"overview":    overview,
				})
			}

			docResults := make([]map[string]any, 0, len(docs))
			for _, doc := range docs {
				docResults = append(docResults, map[string]any{
					"file_path": doc.FilePath,
					"content":   compactMCPText(doc.Content, mcpDocContentLimit),
					"doc_type":  string(doc.DocType),
				})
			}

			return printJSON(map[string]any{
				"documents": docResults,
				"count":     len(docResults),
			})
		},
	}

	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID")
	cmd.Flags().Int64Var(&workspaceID, "workspace-id", 0, "Search across all codebases in this workspace")

	return cmd
}

func newCompareCapabilitiesCmd(ctx context.Context) *cobra.Command {
	var aID, bID int64

	cmd := &cobra.Command{
		Use:   "compare-capabilities",
		Short: "Compare symbol coverage between two codebases (e.g. legacy vs. current)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if aID <= 0 || bID <= 0 {
				return errors.New("--codebase-a-id and --codebase-b-id are required")
			}
			if aID == bID {
				return errors.New("--codebase-a-id and --codebase-b-id must be different")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			result, err := compareCapabilities(ctx, conn, aID, bID)
			if err != nil {
				return err
			}

			return printJSON(result)
		},
	}

	cmd.Flags().Int64Var(&aID, "codebase-a-id", 0, "Reference codebase ID (e.g. legacy)")
	cmd.Flags().Int64Var(&bID, "codebase-b-id", 0, "Target codebase ID to compare against the reference")

	return cmd
}

// compareCapabilities groups symbols from two codebases by file-path domain
// (first path segment) and classifies each domain as implemented/partial/
// missing/extra based on symbol-name+kind overlap.
func compareCapabilities(ctx context.Context, conn *sql.DB, aID, bID int64) (map[string]any, error) {
	repo := store.NewSymbolRepo(conn)
	aSymbols, err := repo.ListCapabilities(ctx, aID)
	if err != nil {
		return nil, err
	}
	bSymbols, err := repo.ListCapabilities(ctx, bID)
	if err != nil {
		return nil, err
	}

	type capKey struct{ name, kind string }

	domain := func(filePath string) string {
		if i := strings.Index(filePath, "/"); i > 0 {
			return filePath[:i+1]
		}
		return "(root)"
	}

	type domainEntry struct {
		a map[capKey]struct{}
		b map[capKey]struct{}
	}
	domains := map[string]*domainEntry{}

	ensure := func(d string) *domainEntry {
		if domains[d] == nil {
			domains[d] = &domainEntry{a: map[capKey]struct{}{}, b: map[capKey]struct{}{}}
		}
		return domains[d]
	}

	for _, s := range aSymbols {
		ensure(domain(s.FilePath)).a[capKey{s.Name, s.Kind}] = struct{}{}
	}
	for _, s := range bSymbols {
		ensure(domain(s.FilePath)).b[capKey{s.Name, s.Kind}] = struct{}{}
	}

	type symbolRef struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	type domainResult struct {
		Domain  string      `json:"domain"`
		Status  string      `json:"status"`
		ACount  int         `json:"codebase_a_symbols"`
		BCount  int         `json:"codebase_b_symbols"`
		InBoth  int         `json:"in_both"`
		OnlyInA []symbolRef `json:"only_in_a"`
		OnlyInB []symbolRef `json:"only_in_b"`
	}

	summary := map[string]int{"implemented": 0, "partial": 0, "missing": 0, "extra": 0}

	domainNames := make([]string, 0, len(domains))
	for d := range domains {
		domainNames = append(domainNames, d)
	}
	sort.Strings(domainNames)

	results := make([]domainResult, 0, len(domainNames))
	for _, d := range domainNames {
		e := domains[d]

		var onlyInA, onlyInB []symbolRef
		inBoth := 0
		for k := range e.a {
			if _, ok := e.b[k]; ok {
				inBoth++
			} else {
				onlyInA = append(onlyInA, symbolRef{k.name, k.kind})
			}
		}
		for k := range e.b {
			if _, ok := e.a[k]; !ok {
				onlyInB = append(onlyInB, symbolRef{k.name, k.kind})
			}
		}

		var status string
		switch {
		case len(e.a) == 0:
			status = "extra"
		case len(e.b) == 0:
			status = "missing"
		case inBoth*10 >= len(e.a)*7:
			status = "implemented"
		default:
			status = "partial"
		}
		summary[status]++

		results = append(results, domainResult{
			Domain: d, Status: status, ACount: len(e.a), BCount: len(e.b),
			InBoth: inBoth, OnlyInA: onlyInA, OnlyInB: onlyInB,
		})
	}

	return map[string]any{
		"codebase_a_id": aID,
		"codebase_b_id": bID,
		"domains":       results,
		"summary":       summary,
	}, nil
}

func newServerStatsCmd(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server-stats",
		Short: "Show persisted tool-call, indexing, and analyze metrics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			stats, err := buildServerStats(ctx, conn)
			if err != nil {
				return err
			}

			return printJSON(stats)
		},
	}

	return cmd
}

// buildServerStats aggregates the metric_tool_calls, metric_index_runs, and
// metric_analyze_runs tables (persisted by both the CLI and the MCP server)
// into a single point-in-time summary. Unlike the MCP server_stats tool,
// which reports an in-memory, session-scoped counter, this reads durable
// history that spans every process invocation.
func buildServerStats(ctx context.Context, conn *sql.DB) (map[string]any, error) {
	var codebaseCount int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM codebases").Scan(&codebaseCount); err != nil {
		return nil, err
	}

	rows, err := conn.QueryContext(ctx,
		"SELECT tool, duration_ms, is_error FROM metric_tool_calls ORDER BY tool")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type toolAgg struct {
		durations []int64
		errors    int64
	}
	byTool := map[string]*toolAgg{}
	var totalCalls, totalErrors int64

	for rows.Next() {
		var tool string
		var durationMs int64
		var isError int
		if err := rows.Scan(&tool, &durationMs, &isError); err != nil {
			return nil, err
		}
		agg := byTool[tool]
		if agg == nil {
			agg = &toolAgg{}
			byTool[tool] = agg
		}
		agg.durations = append(agg.durations, durationMs)
		totalCalls++
		if isError != 0 {
			agg.errors++
			totalErrors++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tools := make(map[string]any, len(byTool))
	for tool, agg := range byTool {
		slices.Sort(agg.durations)
		var sum int64
		for _, d := range agg.durations {
			sum += d
		}
		count := int64(len(agg.durations))
		var avg, p95 int64
		if count > 0 {
			avg = sum / count
			idx := max(int(float64(count)*0.95)-1, 0)
			if idx >= len(agg.durations) {
				idx = len(agg.durations) - 1
			}
			p95 = agg.durations[idx]
		}
		tools[tool] = map[string]any{
			"count":           count,
			"avg_duration_ms": avg,
			"p95_duration_ms": p95,
			"error_count":     agg.errors,
		}
	}

	var indexRuns, indexFiles, indexChunks, indexEmbedFailures, indexAvgMs sql.NullInt64
	if err := conn.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(files_indexed), 0), COALESCE(SUM(chunks_indexed), 0),
		       COALESCE(SUM(embedding_failures), 0), COALESCE(AVG(duration_ms), 0)
		FROM metric_index_runs`,
	).Scan(&indexRuns, &indexFiles, &indexChunks, &indexEmbedFailures, &indexAvgMs); err != nil {
		return nil, err
	}

	var analyzeRuns, analyzeFiles, analyzeComplete, analyzeTextFallbacks, analyzePartial, analyzePanics, analyzeZeroSymbols, analyzeSymbols, analyzeEdges, analyzeAvgMs sql.NullInt64
	if err := conn.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(total_files), 0), COALESCE(SUM(complete), 0),
		       COALESCE(SUM(text_fallbacks), 0), COALESCE(SUM(partial), 0), COALESCE(SUM(panics), 0),
		       COALESCE(SUM(zero_symbols), 0), COALESCE(SUM(total_symbols), 0), COALESCE(SUM(total_edges), 0),
		       COALESCE(AVG(duration_ms), 0)
		FROM metric_analyze_runs`,
	).Scan(&analyzeRuns, &analyzeFiles, &analyzeComplete, &analyzeTextFallbacks, &analyzePartial,
		&analyzePanics, &analyzeZeroSymbols, &analyzeSymbols, &analyzeEdges, &analyzeAvgMs,
	); err != nil {
		return nil, err
	}

	return map[string]any{
		"active_codebases": codebaseCount,
		"tool_calls": map[string]any{
			"total":   totalCalls,
			"errors":  totalErrors,
			"by_tool": tools,
		},
		"index_runs": map[string]any{
			"run_count":          indexRuns.Int64,
			"files_indexed":      indexFiles.Int64,
			"chunks_indexed":     indexChunks.Int64,
			"embedding_failures": indexEmbedFailures.Int64,
			"avg_duration_ms":    indexAvgMs.Int64,
		},
		"analyze_runs": map[string]any{
			"run_count":       analyzeRuns.Int64,
			"total_files":     analyzeFiles.Int64,
			"complete":        analyzeComplete.Int64,
			"text_fallback":   analyzeTextFallbacks.Int64,
			"partial":         analyzePartial.Int64,
			"panics":          analyzePanics.Int64,
			"zero_symbols":    analyzeZeroSymbols.Int64,
			"total_symbols":   analyzeSymbols.Int64,
			"total_edges":     analyzeEdges.Int64,
			"avg_duration_ms": analyzeAvgMs.Int64,
		},
	}, nil
}

func newIndexStatusCmd(ctx context.Context) *cobra.Command {
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "index-status",
		Short: "Show chunk index readiness for a codebase",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if codebaseID <= 0 {
				return errors.New("--codebase-id is required")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			var chunkCount int64
			var latestIndexedAt sql.NullInt64
			err = conn.QueryRowContext(ctx,
				"SELECT COUNT(*), MAX(indexed_at) FROM chunks WHERE codebase_id = ?",
				codebaseID,
			).Scan(&chunkCount, &latestIndexedAt)
			if err != nil {
				return err
			}

			return printJSON(map[string]any{
				"codebase_id": codebaseID,
				"chunk_count": chunkCount,
				"indexed_at":  latestIndexedAt.Int64,
				"indexed":     chunkCount > 0,
			})
		},
	}

	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID")

	return cmd
}
