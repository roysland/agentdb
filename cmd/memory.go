package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/config"
	"github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/embed"
	"github.com/roysland/agentdb/internal/search"
	"github.com/roysland/agentdb/internal/store"
)

var (
	embedProviderMu     sync.Mutex
	embedProviderKey    string
	embedProviderCached embed.Provider
)

func newMemoryCmd(ctx context.Context) *cobra.Command {
	mem := &cobra.Command{
		Use:   "memory",
		Short: "Manage long-term agent memories",
	}

	mem.AddCommand(newMemoryAddCmd(ctx))
	mem.AddCommand(newMemoryGetCmd(ctx))
	mem.AddCommand(newMemoryListCmd(ctx))
	mem.AddCommand(newMemoryUpdateCmd(ctx))
	mem.AddCommand(newMemorySearchCmd(ctx))
	mem.AddCommand(newMemoryDeleteCmd(ctx))
	mem.AddCommand(newMemoryRetrieveCmd(ctx))

	return mem
}

func newMemoryAddCmd(ctx context.Context) *cobra.Command {
	var id string
	var content string
	var category string
	var sourceTask string
	var embeddingCSV string
	var workspaceID int64
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a memory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(content) == "" {
				return errors.New("--content is required")
			}
			if strings.TrimSpace(category) == "" {
				return errors.New("--category is required")
			}
			if id == "" {
				id = uuid.NewString()
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewMemoryRepo(conn)
			now := time.Now().Unix()

			item := store.Memory{
				ID:         id,
				Content:    content,
				Category:   category,
				CreatedAt:  now,
				SourceTask: sourceTask,
			}
			if workspaceID > 0 {
				item.WorkspaceID = &workspaceID
			}
			if codebaseID > 0 {
				item.CodebaseID = &codebaseID
			}
			if strings.TrimSpace(embeddingCSV) != "" {
				emb, err := parseEmbeddingCSV(embeddingCSV)
				if err != nil {
					return err
				}
				item.Embedding = emb
			}
			if err := repo.Create(cmd.Context(), item); err != nil {
				return err
			}

			return printJSON(item)
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "Memory ID (default: generated UUID)")
	cmd.Flags().StringVar(&content, "content", "", "Memory content")
	cmd.Flags().StringVar(&category, "category", "", "Memory category")
	cmd.Flags().Int64Var(&workspaceID, "workspace-id", 0, "Optional workspace scope")
	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Optional codebase scope")
	cmd.Flags().StringVar(&sourceTask, "source-task", "", "Optional source task ID")
	cmd.Flags().StringVar(&embeddingCSV, "embedding", "", "Optional embedding as comma-separated float values")

	return cmd
}

func newMemoryUpdateCmd(ctx context.Context) *cobra.Command {
	var content string
	var category string
	var sourceTask string
	var clearSourceTask bool
	var embeddingCSV string

	cmd := &cobra.Command{
		Use:   "update [id]",
		Short: "Update memory fields by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if clearSourceTask && cmd.Flags().Changed("source-task") {
				return errors.New("use either --source-task or --clear-source-task")
			}
			if !cmd.Flags().Changed("content") &&
				!cmd.Flags().Changed("category") &&
				!cmd.Flags().Changed("source-task") &&
				!clearSourceTask &&
				!cmd.Flags().Changed("embedding") {
				return errors.New("at least one field must be provided")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewMemoryRepo(conn)
			params := store.UpdateMemoryParams{}

			if cmd.Flags().Changed("content") {
				params.Content = &content
			}
			if cmd.Flags().Changed("category") {
				params.Category = &category
			}
			if cmd.Flags().Changed("source-task") {
				params.SourceTask = &sourceTask
			}
			if clearSourceTask {
				empty := ""
				params.SourceTask = &empty
			}
			if cmd.Flags().Changed("embedding") {
				emb, err := parseEmbeddingCSV(embeddingCSV)
				if err != nil {
					return err
				}
				params.Embedding = &emb
			}

			item, err := repo.UpdateByID(cmd.Context(), args[0], params)
			if err != nil {
				return err
			}
			return printJSON(item)
		},
	}

	cmd.Flags().StringVar(&content, "content", "", "New content")
	cmd.Flags().StringVar(&category, "category", "", "New category")
	cmd.Flags().StringVar(&sourceTask, "source-task", "", "New source task (empty string clears)")
	cmd.Flags().BoolVar(&clearSourceTask, "clear-source-task", false, "Clear source task")
	cmd.Flags().StringVar(&embeddingCSV, "embedding", "", "New embedding as comma-separated float values")

	return cmd
}

func newMemorySearchCmd(ctx context.Context) *cobra.Command {
	var query string
	var mode string
	var category string
	var limit int
	var queryEmbeddingCSV string
	var workspaceID int64
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search memories by lexical or vector similarity",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(query) == "" {
				return errors.New("--query is required")
			}

			mode = strings.ToLower(strings.TrimSpace(mode))
			if mode == "" {
				mode = "lexical"
			}
			if mode != "lexical" && mode != "vector" && mode != "hybrid" {
				return errors.New("--mode must be lexical, vector, or hybrid")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewMemoryRepo(conn)

			lexical, err := repo.SearchLexical(cmd.Context(), query, category, limit, workspaceID, codebaseID)
			if err != nil {
				return err
			}

			if mode == "lexical" {
				return printJSON(map[string]any{"mode": "lexical", "results": lexical})
			}

			queryEmbedding, err := resolveQueryEmbedding(cmd.Context(), query, queryEmbeddingCSV)
			if err != nil {
				return printJSON(map[string]any{
					"mode_requested": mode,
					"mode_used":      "lexical",
					"results":        lexical,
					"warning":        err.Error(),
				})
			}

			if mode == "vector" {
				candidates, err := repo.ListWithEmbeddings(cmd.Context(), category, max(limit*20, 200), workspaceID, codebaseID)
				if err != nil {
					return err
				}
				if len(candidates) == 0 {
					return printJSON(map[string]any{
						"mode_requested": "vector",
						"mode_used":      "lexical",
						"results":        lexical,
						"warning":        "no stored memory embeddings found; using lexical fallback",
					})
				}
				hits := search.RankMemoriesByCosine(candidates, queryEmbedding, limit)
				if len(hits) == 0 {
					return printJSON(map[string]any{
						"mode_requested": "vector",
						"mode_used":      "lexical",
						"results":        lexical,
						"warning":        "vector ranking produced no hits; using lexical fallback",
					})
				}
				return printJSON(map[string]any{"mode": "vector", "results": hits})
			}

			vectorHits := search.RankMemoriesByCosine(lexical, queryEmbedding, limit)
			if len(vectorHits) == 0 {
				return printJSON(map[string]any{
					"mode_requested": "hybrid",
					"mode_used":      "lexical",
					"results":        lexical,
					"warning":        "no embeddings found in lexical result set",
				})
			}

			return printJSON(map[string]any{"mode": "hybrid", "results": vectorHits})
		},
	}

	cmd.Flags().StringVar(&query, "query", "", "Search query text")
	cmd.Flags().StringVar(&mode, "mode", "lexical", "Search mode: lexical|vector|hybrid")
	cmd.Flags().StringVar(&category, "category", "", "Optional category filter")
	cmd.Flags().Int64Var(&workspaceID, "workspace-id", 0, "Optional workspace scope")
	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Optional codebase scope")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results")
	cmd.Flags().StringVar(&queryEmbeddingCSV, "query-embedding", "", "Optional query embedding as comma-separated float values")

	return cmd
}

func resolveQueryEmbedding(ctx context.Context, query, queryEmbeddingCSV string) ([]float32, error) {
	if strings.TrimSpace(queryEmbeddingCSV) != "" {
		return parseEmbeddingCSV(queryEmbeddingCSV)
	}

	provider, err := sessionEmbeddingProvider(rootCfg)
	if err != nil {
		return nil, err
	}

	vec, err := provider.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query with provider %q: %w", provider.ModelName(), err)
	}
	return vec, nil
}

func sessionEmbeddingProvider(cfg config.Runtime) (embed.Provider, error) {
	key := embeddingProviderSessionKey(cfg)

	embedProviderMu.Lock()
	defer embedProviderMu.Unlock()

	if embedProviderCached != nil && embedProviderKey == key {
		return embedProviderCached, nil
	}

	base, err := embed.NewProviderFromRuntime(cfg)
	if err != nil {
		return nil, err
	}

	embedProviderCached = embed.NewCachedProvider(base)
	embedProviderKey = key
	return embedProviderCached, nil
}

func embeddingProviderSessionKey(cfg config.Runtime) string {
	keyHash := sha256.Sum256([]byte(cfg.EmbeddingAPIKey))
	return strings.Join([]string{
		cfg.EmbeddingProvider,
		cfg.EmbeddingBaseURL,
		cfg.EmbeddingModel,
		strconv.Itoa(cfg.EmbeddingTimeoutSeconds),
		hex.EncodeToString(keyHash[:]),
	}, "|")
}

func parseEmbeddingCSV(raw string) ([]float32, error) {
	parts := strings.Split(raw, ",")
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		f, err := strconv.ParseFloat(v, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid embedding value %q: %w", v, err)
		}
		out = append(out, float32(f))
	}
	if len(out) == 0 {
		return nil, errors.New("embedding cannot be empty")
	}
	return out, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func newMemoryGetCmd(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [id]",
		Short: "Get memory by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewMemoryRepo(conn)
			item, err := repo.GetByID(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			return printJSON(item)
		},
	}

	return cmd
}

func newMemoryListCmd(ctx context.Context) *cobra.Command {
	var category string
	var limit int
	var workspaceID int64
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List memories",
		RunE: func(cmd *cobra.Command, _ []string) error {
			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewMemoryRepo(conn)
			items, err := repo.List(cmd.Context(), store.ListMemoryParams{
				Category:    category,
				Limit:       limit,
				WorkspaceID: workspaceID,
				CodebaseID:  codebaseID,
			})
			if err != nil {
				return err
			}

			return printJSON(items)
		},
	}

	cmd.Flags().StringVar(&category, "category", "", "Optional category filter")
	cmd.Flags().Int64Var(&workspaceID, "workspace-id", 0, "Optional workspace scope")
	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Optional codebase scope")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum memories to return")

	return cmd
}

func newMemoryDeleteCmd(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete memory by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewMemoryRepo(conn)
			deleted, err := repo.DeleteByID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !deleted {
				return fmt.Errorf("memory not found: %s", args[0])
			}

			return printJSON(map[string]any{"deleted": true, "id": args[0]})
		},
	}

	return cmd
}

func newMemoryRetrieveCmd(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retrieve [id]",
		Short: "Mark memory as retrieved and return it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewMemoryRepo(conn)
			item, err := repo.MarkRetrieved(cmd.Context(), args[0], time.Now().Unix())
			if err != nil {
				return err
			}

			return printJSON(item)
		},
	}

	return cmd
}
