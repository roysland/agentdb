package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/config"
	"github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/embed"
	"github.com/roysland/agentdb/internal/filefilter"
	"github.com/roysland/agentdb/internal/index"
	"github.com/roysland/agentdb/internal/parse"
	"github.com/roysland/agentdb/internal/store"
)

type analyzeCmd struct {
	codebaseID   int64
	codebasePath string
	embed        bool
	incremental  bool
}

func newAnalyzeCmd(ctx context.Context) *cobra.Command {
	ac := &analyzeCmd{}

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Parse a codebase into symbols and relationships (project database)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return ac.run(ctx)
		},
	}

	cmd.Flags().Int64Var(&ac.codebaseID, "codebase-id", 0, "Codebase ID to analyze")
	cmd.Flags().StringVar(&ac.codebasePath, "codebase-path", "", "Path to codebase root")
	cmd.Flags().BoolVar(&ac.embed, "embed", false, "Generate embeddings for symbols (requires embedding provider)")
	cmd.Flags().BoolVar(&ac.incremental, "incremental", false, "Only re-analyze changed files (uses stored file hashes)")

	return cmd
}

func (ac *analyzeCmd) run(ctx context.Context) error {
	startTime := time.Now()

	resolved := config.Resolve(rootCfg)

	conn, err := db.Open(ctx, resolved)
	if err != nil {
		return err
	}
	defer conn.Close()
	catalogRepo := store.NewCatalogRepo(conn)
	codebaseID, codebasePath, err := resolveCodebaseTarget(ctx, catalogRepo, resolved, ac.codebaseID, "", ac.codebasePath, false)
	if err != nil {
		return err
	}
	ac.codebaseID = codebaseID
	ac.codebasePath = codebasePath

	symbolRepo := store.NewSymbolRepo(conn)
	edgeRepo := store.NewEdgeRepo(conn)
	sfRepo := store.NewSourceFileRepo(conn)
	wsRepo := store.NewWorkspaceRepo(conn)

	// Setup optional embedding provider
	var provider embed.Provider
	embedRequested := ac.embed || resolved.EmbeddingProvider != "disabled"
	if embedRequested {
		provider, err = embed.NewProviderFromRuntime(resolved)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: embedding provider unavailable (%s), analyzing without embeddings\n", err)
			provider = nil
		}
	}

	// Construct plugin registry with default and env-configured plugin directories
	pluginDirs := parse.PluginDirectories()
	registry, err := parse.NewPluginRegistry(pluginDirs, parse.DefaultParsers())
	if err != nil {
		return fmt.Errorf("init plugin registry: %w", err)
	}
	defer registry.Shutdown()

	parsers := registry.AllParsers()

	// Determine if we should do incremental analysis
	var runErr error
	if ac.incremental {
		runErr = ac.runIncremental(ctx, parsers, symbolRepo, edgeRepo, sfRepo, provider, startTime)
	} else {
		runErr = ac.runFull(ctx, parsers, symbolRepo, edgeRepo, sfRepo, provider, startTime)
	}
	if runErr != nil {
		return runErr
	}

	// Cross-repo link resolution: resolve unresolved imports against workspace members
	ac.resolveCrossRepoLinks(ctx, wsRepo, edgeRepo, symbolRepo)

	return nil
}

func (ac *analyzeCmd) runFull(ctx context.Context, parsers []parse.Parser, symbolRepo *store.SymbolRepo, edgeRepo *store.EdgeRepo, sfRepo *store.SourceFileRepo, provider embed.Provider, startTime time.Time) error {
	// Clear existing analysis for this codebase
	if err := symbolRepo.DeleteByCodebase(ctx, ac.codebaseID); err != nil {
		return fmt.Errorf("clear symbols: %w", err)
	}
	if err := edgeRepo.DeleteByCodebase(ctx, ac.codebaseID); err != nil {
		return fmt.Errorf("clear edges: %w", err)
	}
	if err := sfRepo.DeleteByCodebase(ctx, ac.codebaseID); err != nil {
		return fmt.Errorf("clear source files: %w", err)
	}

	// Parse the codebase using all parsers (plugins + builtins)
	results, err := parse.ParseDirectory(ac.codebasePath, parsers)
	if err != nil {
		return fmt.Errorf("parse directory: %w", err)
	}

	indexedAt := time.Now().Unix()
	totalSymbols, totalEdges, totalFiles := 0, 0, 0

	for _, result := range results {
		if err := ac.storeFileResult(ctx, result, symbolRepo, edgeRepo, sfRepo, provider, indexedAt); err != nil {
			return err
		}
		totalFiles++
		totalSymbols += len(result.Symbols)
		totalEdges += len(result.Edges)
	}

	duration := time.Since(startTime)
	durationMs := duration.Milliseconds()
	filesPerSecond := float64(0)
	if durationMs > 0 {
		filesPerSecond = float64(totalFiles) / duration.Seconds()
	}
	embeddingEnabled := provider != nil
	embeddingModel := ""
	if embeddingEnabled {
		embeddingModel = provider.ModelName()
	}

	return printJSON(map[string]any{
		"codebase_id":       ac.codebaseID,
		"total_files":       totalFiles,
		"files_analyzed":    totalFiles,
		"files_skipped":     0,
		"files_changed":     0,
		"files_added":       totalFiles,
		"files_removed":     0,
		"symbols_extracted": totalSymbols,
		"edges_extracted":   totalEdges,
		"duration_ms":       durationMs,
		"files_per_second":  filesPerSecond,
		"embedding_enabled": embeddingEnabled,
		"embedding_model":   embeddingModel,
		"incremental":       false,
	})
}

func (ac *analyzeCmd) runIncremental(ctx context.Context, parsers []parse.Parser, symbolRepo *store.SymbolRepo, edgeRepo *store.EdgeRepo, sfRepo *store.SourceFileRepo, provider embed.Provider, startTime time.Time) error {
	// Load stored hashes from source_files table
	storedHashes, err := sfRepo.GetHashesByCodebase(ctx, ac.codebaseID)
	if err != nil {
		return fmt.Errorf("load stored hashes: %w", err)
	}

	// If no source_files exist, fall back to full analyze
	if len(storedHashes) == 0 {
		return ac.runFull(ctx, parsers, symbolRepo, edgeRepo, sfRepo, provider, startTime)
	}

	// Compute delta
	delta, err := index.ComputeDelta(ctx, ac.codebaseID, ac.codebasePath, storedHashes)
	if err != nil {
		return fmt.Errorf("compute delta: %w", err)
	}

	indexedAt := time.Now().Unix()
	totalSymbols, totalEdges := 0, 0

	// Process Changed + Added files: delete old data and re-parse
	filesToProcess := index.FilesToProcess(delta)
	for _, relPath := range filesToProcess {
		// Delete existing symbols and edges for this file
		if err := symbolRepo.DeleteByFile(ctx, ac.codebaseID, relPath); err != nil {
			return fmt.Errorf("delete symbols for file %s: %w", relPath, err)
		}
		if err := edgeRepo.DeleteByFile(ctx, ac.codebaseID, relPath); err != nil {
			return fmt.Errorf("delete edges for file %s: %w", relPath, err)
		}

		// Parse the single file
		absPath := filepath.Join(ac.codebasePath, filepath.FromSlash(relPath))
		if !filefilter.IsConfinedRegularFile(ac.codebasePath, absPath, nil) {
			_, _ = fmt.Fprintf(os.Stderr, "warning: skipping non-confined or non-regular file %s\n", relPath)
			continue
		}
		content, readErr := os.ReadFile(absPath)
		if readErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to read file %s: %v\n", relPath, readErr)
			continue
		}

		p := findParserForFile(relPath, parsers)
		if p == nil {
			continue
		}

		result, parseErr := p.Parse(relPath, content)
		if parseErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: parse %s: %v\n", relPath, parseErr)
			continue
		}

		if err := ac.storeFileResult(ctx, result, symbolRepo, edgeRepo, sfRepo, provider, indexedAt); err != nil {
			return err
		}
		totalSymbols += len(result.Symbols)
		totalEdges += len(result.Edges)
	}

	// Delete data for Removed files
	for _, relPath := range delta.Removed {
		if err := symbolRepo.DeleteByFile(ctx, ac.codebaseID, relPath); err != nil {
			return fmt.Errorf("delete symbols for removed file %s: %w", relPath, err)
		}
		if err := edgeRepo.DeleteByFile(ctx, ac.codebaseID, relPath); err != nil {
			return fmt.Errorf("delete edges for removed file %s: %w", relPath, err)
		}
		if err := sfRepo.DeleteByFile(ctx, ac.codebaseID, relPath); err != nil {
			return fmt.Errorf("delete source file record %s: %w", relPath, err)
		}
	}

	// Compute metrics
	totalFiles := len(delta.Changed) + len(delta.Added) + len(delta.Removed) + len(delta.Unchanged)
	filesAnalyzed := len(delta.Changed) + len(delta.Added)
	duration := time.Since(startTime)
	durationMs := duration.Milliseconds()
	filesPerSecond := float64(0)
	if durationMs > 0 {
		filesPerSecond = float64(filesAnalyzed) / duration.Seconds()
	}
	embeddingEnabled := provider != nil
	embeddingModel := ""
	if embeddingEnabled {
		embeddingModel = provider.ModelName()
	}

	return printJSON(map[string]any{
		"codebase_id":       ac.codebaseID,
		"total_files":       totalFiles,
		"files_analyzed":    filesAnalyzed,
		"files_skipped":     len(delta.Unchanged),
		"files_changed":     len(delta.Changed),
		"files_added":       len(delta.Added),
		"files_removed":     len(delta.Removed),
		"symbols_extracted": totalSymbols,
		"edges_extracted":   totalEdges,
		"duration_ms":       durationMs,
		"files_per_second":  filesPerSecond,
		"embedding_enabled": embeddingEnabled,
		"embedding_model":   embeddingModel,
		"incremental":       true,
	})
}

// storeFileResult persists a parsed file's symbols, edges, and source_file record.
func (ac *analyzeCmd) storeFileResult(ctx context.Context, result parse.FileResult, symbolRepo *store.SymbolRepo, edgeRepo *store.EdgeRepo, sfRepo *store.SourceFileRepo, provider embed.Provider, indexedAt int64) error {
	sfData := store.SourceFileData{
		FilePath:    result.FilePath,
		Language:    result.Language,
		PackageName: result.PackageName,
		LOC:         result.LOC,
		SymbolCount: len(result.Symbols),
		FileHash:    result.FileHash,
		IndexedAt:   indexedAt,
	}
	if err := sfRepo.Upsert(ctx, ac.codebaseID, sfData); err != nil {
		return fmt.Errorf("store source file %s: %w", result.FilePath, err)
	}

	for _, sym := range result.Symbols {
		sd := store.SymbolData{
			FilePath:      sym.FilePath,
			Language:      sym.Language,
			Kind:          sym.Kind,
			Name:          sym.Name,
			QualifiedName: sym.QualifiedName,
			Receiver:      sym.Receiver,
			Signature:     sym.Signature,
			DocComment:    sym.DocComment,
			Visibility:    sym.Visibility,
			BodySnippet:   sym.BodySnippet,
			StartLine:     sym.StartLine,
			EndLine:       sym.EndLine,
			FileHash:      sym.FileHash,
			IndexedAt:     indexedAt,
		}
		if provider != nil {
			// Embed signature + doc comment for semantic symbol lookup
			text := sym.Signature
			if sym.DocComment != "" {
				text += "\n" + sym.DocComment
			}
			if emb, embErr := provider.Embed(ctx, text); embErr == nil {
				sd.Embedding = emb
				sd.EmbeddingModel = provider.ModelName()
			}
		}
		if err := symbolRepo.Create(ctx, ac.codebaseID, sd); err != nil {
			return fmt.Errorf("store symbol %s: %w", sym.QualifiedName, err)
		}
	}

	for _, edge := range result.Edges {
		ed := store.EdgeData{
			FromKind: edge.FromKind,
			FromRef:  edge.FromRef,
			ToKind:   edge.ToKind,
			ToRef:    edge.ToRef,
			EdgeKind: edge.EdgeKind,
			Line:     edge.Line,
			Resolved: edge.Resolved,
		}
		if err := edgeRepo.Create(ctx, ac.codebaseID, ed); err != nil {
			return fmt.Errorf("store edge %s→%s: %w", edge.FromRef, edge.ToRef, err)
		}
	}

	return nil
}

// findParserForFile returns the first parser that can handle the given file path.
func findParserForFile(filePath string, parsers []parse.Parser) parse.Parser {
	for _, p := range parsers {
		if p.CanParse(filePath) {
			return p
		}
	}
	return nil
}

// resolveCrossRepoLinks checks if the current codebase is part of a workspace,
// and if so, attempts to resolve unresolved import edges by matching them against
// symbols in other workspace member codebases.
func (ac *analyzeCmd) resolveCrossRepoLinks(ctx context.Context, wsRepo *store.WorkspaceRepo, edgeRepo *store.EdgeRepo, symbolRepo *store.SymbolRepo) {
	// Check if this codebase belongs to any workspace
	workspaces, err := wsRepo.GetWorkspacesForCodebase(ctx, ac.codebaseID)
	if err != nil || len(workspaces) == 0 {
		return
	}

	// Get unresolved import edges for this codebase
	unresolvedImports, err := edgeRepo.GetUnresolvedImports(ctx, ac.codebaseID)
	if err != nil || len(unresolvedImports) == 0 {
		return
	}

	// For each workspace this codebase belongs to, collect other member IDs
	for _, ws := range workspaces {
		memberIDs, err := wsRepo.GetMemberIDs(ctx, ws.ID)
		if err != nil {
			continue
		}

		// Filter out the current codebase from the member list
		otherIDs := make([]int64, 0, len(memberIDs))
		for _, id := range memberIDs {
			if id != ac.codebaseID {
				otherIDs = append(otherIDs, id)
			}
		}
		if len(otherIDs) == 0 {
			continue
		}

		// Try to resolve each unresolved import against other workspace members
		for _, edge := range unresolvedImports {
			symbols, err := symbolRepo.FindByNameMulti(ctx, otherIDs, edge.ToRef)
			if err != nil || len(symbols) == 0 {
				continue
			}

			// Look for an exact qualified_name match
			for _, sym := range symbols {
				if sym.QualifiedName == edge.ToRef || sym.Name == edge.ToRef {
					_ = edgeRepo.ResolveCrossRepoEdge(ctx, edge.ID, sym.CodebaseID)
					break
				}
			}
		}
	}
}
