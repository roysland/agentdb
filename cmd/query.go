package cmd

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/db"
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

			sfRepo := store.NewSourceFileRepo(conn)
			symRepo := store.NewSymbolRepo(conn)

			fileStats, err := sfRepo.Stats(ctx, codebaseID)
			if err != nil {
				return err
			}
			symbolStats, err := symRepo.Stats(ctx, codebaseID)
			if err != nil {
				return err
			}
			packages, err := sfRepo.PackageList(ctx, codebaseID)
			if err != nil {
				return err
			}
			topFiles, err := symRepo.TopFilesBySymbolCount(ctx, codebaseID, 10)
			if err != nil {
				return err
			}
			entryPoints, err := symRepo.ExportedFuncs(ctx, codebaseID, 20)
			if err != nil {
				return err
			}

			return printJSON(map[string]any{
				"codebase_id":    codebaseID,
				"files":          fileStats,
				"symbols":        symbolStats,
				"packages":       packages,
				"top_files":      topFiles,
				"exported_funcs": entryPoints,
			})
		},
	}

	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID")

	return cmd
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
