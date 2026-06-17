package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/store"
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
	var workspaceID int64
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a memory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if content == "" {
				return errors.New("--content is required")
			}
			if category == "" {
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

	return cmd
}

func newMemoryUpdateCmd(ctx context.Context) *cobra.Command {
	var content string
	var category string
	var sourceTask string
	var clearSourceTask bool

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
				!clearSourceTask {
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

	return cmd
}

func newMemorySearchCmd(ctx context.Context) *cobra.Command {
	var query string
	var category string
	var limit int
	var workspaceID int64
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search memories by lexical similarity",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if query == "" {
				return errors.New("--query is required")
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

			return printJSON(map[string]any{"mode": "lexical", "results": lexical})
		},
	}

	cmd.Flags().StringVar(&query, "query", "", "Search query text")
	cmd.Flags().StringVar(&category, "category", "", "Optional category filter")
	cmd.Flags().Int64Var(&workspaceID, "workspace-id", 0, "Optional workspace scope")
	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Optional codebase scope")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results")

	return cmd
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
