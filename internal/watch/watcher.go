package watch

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/roysland/agentdb/internal/chunk"
	"github.com/roysland/agentdb/internal/embed"
	"github.com/roysland/agentdb/internal/filefilter"
	"github.com/roysland/agentdb/internal/index"
	"github.com/roysland/agentdb/internal/observe"
	"github.com/roysland/agentdb/internal/parse"
	"github.com/roysland/agentdb/internal/store"
)

// watcherState represents the current state of the watcher state machine.
type watcherState int

const (
	stateIdle watcherState = iota
	stateAccumulating
	stateIndexing
	stateIndexingQueued
)

// Config holds watcher configuration.
type Config struct {
	CodebaseID    int64
	CodebasePath  string
	DebounceMs    int
	Analyze       bool
	EmbedProvider embed.Provider
}

// Watcher monitors a codebase directory for file changes and triggers re-indexing.
type Watcher struct {
	codebaseID    int64
	codebasePath  string
	debounce      time.Duration
	analyze       bool
	embedProvider embed.Provider
	db            *sql.DB
	logger        *observe.Logger
}

// New creates a Watcher from the given config and database connection.
// It validates that the codebase path exists and is a directory.
func New(cfg Config, db *sql.DB, logger *observe.Logger) (*Watcher, error) {
	info, err := os.Stat(cfg.CodebasePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("codebase path does not exist: %s", cfg.CodebasePath)
		}
		return nil, fmt.Errorf("cannot access codebase path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("codebase path is not a directory: %s", cfg.CodebasePath)
	}

	debounceMs := cfg.DebounceMs
	if debounceMs <= 0 {
		debounceMs = 500
	}

	return &Watcher{
		codebaseID:    cfg.CodebaseID,
		codebasePath:  cfg.CodebasePath,
		debounce:      time.Duration(debounceMs) * time.Millisecond,
		analyze:       cfg.Analyze,
		embedProvider: cfg.EmbedProvider,
		db:            db,
		logger:        logger,
	}, nil
}

// Run starts the filesystem watcher and blocks until ctx is cancelled.
// It returns nil on graceful shutdown (context cancellation).
func (w *Watcher) Run(ctx context.Context) error {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fsnotify watcher: %w", err)
	}
	defer fsWatcher.Close()

	// Recursively add directories to the watcher.
	if err := w.addDirectories(fsWatcher); err != nil {
		return fmt.Errorf("add directories to watcher: %w", err)
	}

	w.logger.Log(observe.LogEntry{
		Level:     "info",
		Operation: "watch_start",
		Status:    "started",
	})

	deb := newDebouncer(w.debounce)
	state := stateIdle

	for {
		select {
		case <-ctx.Done():
			w.logger.Log(observe.LogEntry{
				Level:     "info",
				Operation: "watch_stop",
				Status:    "shutdown",
			})
			return nil

		case event, ok := <-fsWatcher.Events:
			if !ok {
				return nil
			}

			// If a new directory is created, add it to the watcher.
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if filefilter.ShouldSkipDirName(filepath.Base(event.Name)) {
						continue
					}
					_ = fsWatcher.Add(event.Name)
					continue
				}
			}

			// Filter: only process code files.
			if !chunk.IsCodeFile(event.Name) {
				continue
			}

			switch state {
			case stateIdle, stateAccumulating:
				state = stateAccumulating
				relPath, err := filepath.Rel(w.codebasePath, event.Name)
				if err != nil {
					continue
				}
				deb.Add(filepath.ToSlash(relPath), event.Op)

			case stateIndexing:
				state = stateIndexingQueued
				relPath, err := filepath.Rel(w.codebasePath, event.Name)
				if err != nil {
					continue
				}
				deb.Add(filepath.ToSlash(relPath), event.Op)

			case stateIndexingQueued:
				// Already queued; just accumulate more events.
				relPath, err := filepath.Rel(w.codebasePath, event.Name)
				if err != nil {
					continue
				}
				deb.Add(filepath.ToSlash(relPath), event.Op)
			}

		case err, ok := <-fsWatcher.Errors:
			if !ok {
				return nil
			}
			w.logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "watch_error",
				Error:     err.Error(),
			})

		case <-deb.Ready():
			if state != stateAccumulating {
				continue
			}

			batch := deb.Drain()
			if len(batch) == 0 {
				state = stateIdle
				continue
			}

			w.logger.Log(observe.LogEntry{
				Level:     "info",
				Operation: "watch_trigger",
				Status:    fmt.Sprintf("detected %d file change(s), starting re-index", len(batch)),
			})

			state = stateIndexing
			err := w.reindex(ctx)
			if err != nil {
				w.logger.Log(observe.LogEntry{
					Level:     "error",
					Operation: "watch_reindex",
					Error:     err.Error(),
				})
			}

			// Transition based on whether events arrived during indexing.
			if state == stateIndexingQueued {
				state = stateAccumulating
				// The debouncer already has queued events; its timer will fire again.
			} else {
				state = stateIdle
			}
		}
	}
}

// reindex performs an incremental re-index of the codebase.
func (w *Watcher) reindex(ctx context.Context) error {
	startTime := time.Now()

	indexedFileRepo := store.NewIndexedFileRepo(w.db)
	chunkRepo := store.NewChunkRepo(w.db)

	// Load stored hashes.
	storedHashes, err := indexedFileRepo.GetHashesByCodebase(ctx, w.codebaseID)
	if err != nil {
		return fmt.Errorf("load stored hashes: %w", err)
	}

	// Compute delta.
	delta, err := index.ComputeDelta(ctx, w.codebaseID, w.codebasePath, storedHashes)
	if err != nil {
		return fmt.Errorf("compute delta: %w", err)
	}

	cfg := chunk.DefaultConfig()
	indexedAt := time.Now().Unix()
	totalChunks := 0

	// Process Changed + Added files.
	filesToProcess := index.FilesToProcess(delta)
	for _, relPath := range filesToProcess {
		// Delete existing chunks for this file.
		if err := chunkRepo.DeleteByFile(ctx, w.codebaseID, relPath); err != nil {
			return fmt.Errorf("delete chunks for file %s: %w", relPath, err)
		}

		absPath := filepath.Join(w.codebasePath, filepath.FromSlash(relPath))
		fileChunks, err := chunk.ChunkFile(absPath, cfg)
		if err != nil {
			w.logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "watch_chunk",
				Error:     fmt.Sprintf("failed to chunk file %s: %v", relPath, err),
			})
			continue
		}

		var fileHash string
		fileChunkCount := 0
		for _, c := range fileChunks {
			chunkData := store.ChunkData{
				FilePath:  relPath,
				ChunkKey:  c.Key,
				Language:  c.Language,
				Kind:      c.Kind,
				Name:      c.Name,
				Signature: c.Signature,
				Snippet:   c.Snippet,
				StartLine: c.StartLine,
				EndLine:   c.EndLine,
				FileHash:  c.FileHash,
				IndexedAt: indexedAt,
			}

			if w.embedProvider != nil {
				if emb, embErr := w.embedProvider.Embed(ctx, c.Snippet); embErr == nil {
					chunkData.Embedding = emb
					chunkData.EmbeddingModel = w.embedProvider.ModelName()
				} else {
					w.logger.Log(observe.LogEntry{
						Level:     "warn",
						Operation: "watch_embed_chunk",
						Error:     fmt.Sprintf("embed chunk %s: %v", c.Key, embErr),
					})
				}
			}

			if err := chunkRepo.Create(ctx, w.codebaseID, chunkData); err != nil {
				return fmt.Errorf("create chunk %s: %w", c.Key, err)
			}

			fileHash = c.FileHash
			fileChunkCount++
			totalChunks++
		}

		if fileHash != "" {
			if err := indexedFileRepo.Upsert(ctx, w.codebaseID, relPath, fileHash, int64(fileChunkCount), indexedAt); err != nil {
				return fmt.Errorf("update indexed file %s: %w", relPath, err)
			}
		}
	}

	// Delete chunks for Removed files.
	for _, relPath := range delta.Removed {
		if err := chunkRepo.DeleteByFile(ctx, w.codebaseID, relPath); err != nil {
			return fmt.Errorf("delete chunks for removed file %s: %w", relPath, err)
		}
		if err := indexedFileRepo.DeleteByFile(ctx, w.codebaseID, relPath); err != nil {
			return fmt.Errorf("delete indexed file record %s: %w", relPath, err)
		}
	}

	duration := time.Since(startTime)
	filesIndexed := len(delta.Changed) + len(delta.Added)

	w.logger.Log(observe.LogEntry{
		Level:      "info",
		Operation:  "watch_reindex",
		DurationMs: duration.Milliseconds(),
		Status:     fmt.Sprintf("indexed %d files, %d chunks, removed %d files", filesIndexed, totalChunks, len(delta.Removed)),
	})

	// Optionally run symbol extraction (analyze step).
	if w.analyze {
		if err := w.runAnalyze(ctx, delta); err != nil {
			w.logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "watch_analyze",
				Error:     fmt.Sprintf("analyze step failed: %v", err),
			})
		}
	}

	return nil
}

// runAnalyze performs symbol/relationship extraction for changed files.
// This is a simplified version that re-parses changed/added files.
func (w *Watcher) runAnalyze(ctx context.Context, delta index.DeltaResult) error {
	symbolRepo := store.NewSymbolRepo(w.db)
	edgeRepo := store.NewEdgeRepo(w.db)
	sfRepo := store.NewSourceFileRepo(w.db)

	parsers := parse.DefaultParsers()
	registry, regErr := parse.NewPluginRegistry(parse.PluginDirectories(), parsers)
	if regErr != nil {
		w.logger.Log(observe.LogEntry{
			Level:     "warn",
			Operation: "watch_analyze",
			Error:     fmt.Sprintf("plugin registry unavailable, using builtins only: %v", regErr),
		})
	} else {
		defer registry.Shutdown()
		parsers = registry.AllParsers()
	}

	filesToProcess := index.FilesToProcess(delta)
	indexedAt := time.Now().Unix()
	totalSymbols := 0
	totalEdges := 0

	for _, relPath := range filesToProcess {
		// Delete existing symbols and edges for this file.
		if err := symbolRepo.DeleteByFile(ctx, w.codebaseID, relPath); err != nil {
			return fmt.Errorf("delete symbols for file %s: %w", relPath, err)
		}
		if err := edgeRepo.DeleteByFile(ctx, w.codebaseID, relPath); err != nil {
			return fmt.Errorf("delete edges for file %s: %w", relPath, err)
		}

		absPath := filepath.Join(w.codebasePath, filepath.FromSlash(relPath))
		content, err := os.ReadFile(absPath)
		if err != nil {
			w.logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "watch_analyze",
				Error:     fmt.Sprintf("failed to read file %s: %v", relPath, err),
			})
			continue
		}

		parser := findParserForFile(relPath, parsers)
		fileHash, hashErr := index.HashFile(absPath)
		if hashErr != nil {
			w.logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "watch_analyze",
				Error:     fmt.Sprintf("failed to hash file %s: %v", relPath, hashErr),
			})
			continue
		}

		if parser == nil {
			sfData := store.SourceFileData{
				FilePath:    relPath,
				Language:    chunk.DetectLanguage(relPath),
				PackageName: "",
				LOC:         countLines(content),
				SymbolCount: 0,
				FileHash:    fileHash,
				IndexedAt:   indexedAt,
			}
			if err := sfRepo.Upsert(ctx, w.codebaseID, sfData); err != nil {
				return fmt.Errorf("store source file %s: %w", relPath, err)
			}
			continue
		}

		result, parseErr := parser.Parse(relPath, content)
		if parseErr != nil {
			w.logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "watch_analyze",
				Error:     fmt.Sprintf("failed to parse file %s: %v", relPath, parseErr),
			})

			sfData := store.SourceFileData{
				FilePath:    relPath,
				Language:    chunk.DetectLanguage(relPath),
				PackageName: "",
				LOC:         countLines(content),
				SymbolCount: 0,
				FileHash:    fileHash,
				IndexedAt:   indexedAt,
			}
			if err := sfRepo.Upsert(ctx, w.codebaseID, sfData); err != nil {
				return fmt.Errorf("store source file %s: %w", relPath, err)
			}
			continue
		}

		if result.FileHash == "" {
			result.FileHash = fileHash
		}

		sfData := store.SourceFileData{
			FilePath:    result.FilePath,
			Language:    result.Language,
			PackageName: result.PackageName,
			LOC:         result.LOC,
			SymbolCount: len(result.Symbols),
			FileHash:    result.FileHash,
			IndexedAt:   indexedAt,
		}
		if err := sfRepo.Upsert(ctx, w.codebaseID, sfData); err != nil {
			return fmt.Errorf("store source file %s: %w", relPath, err)
		}

		for _, sym := range result.Symbols {
			if sym.FileHash == "" {
				sym.FileHash = result.FileHash
			}
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
			if w.embedProvider != nil {
				text := sym.Signature
				if sym.DocComment != "" {
					text += "\n" + sym.DocComment
				}
				if emb, embErr := w.embedProvider.Embed(ctx, text); embErr == nil {
					sd.Embedding = emb
					sd.EmbeddingModel = w.embedProvider.ModelName()
				} else {
					w.logger.Log(observe.LogEntry{
						Level:     "warn",
						Operation: "watch_embed_symbol",
						Error:     fmt.Sprintf("embed symbol %s: %v", sym.QualifiedName, embErr),
					})
				}
			}
			if err := symbolRepo.Create(ctx, w.codebaseID, sd); err != nil {
				return fmt.Errorf("store symbol %s: %w", sym.QualifiedName, err)
			}
			totalSymbols++
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
			if err := edgeRepo.Create(ctx, w.codebaseID, ed); err != nil {
				return fmt.Errorf("store edge %s->%s: %w", edge.FromRef, edge.ToRef, err)
			}
			totalEdges++
		}
	}

	// Delete source_file records for removed files.
	for _, relPath := range delta.Removed {
		if err := symbolRepo.DeleteByFile(ctx, w.codebaseID, relPath); err != nil {
			return fmt.Errorf("delete symbols for removed file %s: %w", relPath, err)
		}
		if err := edgeRepo.DeleteByFile(ctx, w.codebaseID, relPath); err != nil {
			return fmt.Errorf("delete edges for removed file %s: %w", relPath, err)
		}
		if err := sfRepo.DeleteByFile(ctx, w.codebaseID, relPath); err != nil {
			return fmt.Errorf("delete source file record %s: %w", relPath, err)
		}
	}

	w.logger.Log(observe.LogEntry{
		Level:     "info",
		Operation: "watch_analyze",
		Status:    fmt.Sprintf("analyzed %d files, %d symbols, %d edges", len(filesToProcess), totalSymbols, totalEdges),
	})

	return nil
}

// addDirectories recursively walks the codebase path and adds all directories
// to the fsnotify watcher. Permission errors on subdirectories are logged as
// warnings and do not stop the walk.
func (w *Watcher) addDirectories(fsWatcher *fsnotify.Watcher) error {
	return filepath.Walk(w.codebasePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Permission error on subdirectory: log warning and skip.
			w.logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "watch_add_dir",
				Error:     fmt.Sprintf("cannot access %s: %v", path, err),
			})
			return filepath.SkipDir
		}

		if !info.IsDir() {
			return nil
		}

		if filefilter.ShouldSkipDirName(filepath.Base(path)) {
			return filepath.SkipDir
		}

		if err := fsWatcher.Add(path); err != nil {
			w.logger.Log(observe.LogEntry{
				Level:     "warn",
				Operation: "watch_add_dir",
				Error:     fmt.Sprintf("failed to watch %s: %v", path, err),
			})
		}
		return nil
	})
}

func findParserForFile(filePath string, parsers []parse.Parser) parse.Parser {
	for _, p := range parsers {
		if p.CanParse(filePath) {
			return p
		}
	}
	return nil
}

// countLines returns the number of lines in the given content.
func countLines(content []byte) int {
	count := 0
	for _, b := range content {
		if b == '\n' {
			count++
		}
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		count++
	}
	return count
}
