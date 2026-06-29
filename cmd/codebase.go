package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/store"
)

func newCodebaseCmd(ctx context.Context) *cobra.Command {
	var name string
	var path string

	codebase := &cobra.Command{Use: "codebase", Short: "Manage registered codebases"}
	register := &cobra.Command{
		Use:   "register",
		Short: "Register a codebase root path",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(path) == "" {
				return errors.New("--path is required")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewCatalogRepo(conn)
			id, err := repo.RegisterCodebase(cmd.Context(), path, name)
			if err != nil {
				return err
			}

			return printJSON(map[string]any{"id": id, "path": path, "name": name})
		},
	}
	register.Flags().StringVar(&path, "path", "", "Root path to codebase")
	register.Flags().StringVar(&name, "name", "", "Optional codebase name")

	list := &cobra.Command{
		Use:   "list",
		Short: "List codebases",
		RunE: func(cmd *cobra.Command, _ []string) error {
			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewCatalogRepo(conn)
			items, err := repo.ListCodebases(cmd.Context())
			if err != nil {
				return err
			}
			return printJSON(items)
		},
	}

	prune := &cobra.Command{
		Use:   "prune",
		Short: "Remove codebases whose root path no longer exists on disk",
		RunE: func(cmd *cobra.Command, _ []string) error {
			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewCatalogRepo(conn)
			items, err := repo.ListCodebases(cmd.Context())
			if err != nil {
				return err
			}

			type result struct {
				ID       int64  `json:"id"`
				Name     string `json:"name"`
				RootPath string `json:"root_path"`
			}
			var pruned []result

			for _, cb := range items {
				if _, statErr := os.Stat(cb.RootPath); os.IsNotExist(statErr) {
					if err := repo.DeleteCodebase(cmd.Context(), cb.ID); err != nil {
						return fmt.Errorf("delete codebase %d (%s): %w", cb.ID, cb.RootPath, err)
					}
					pruned = append(pruned, result{ID: cb.ID, Name: cb.Name, RootPath: cb.RootPath})
				}
			}

			return printJSON(map[string]any{
				"pruned": pruned,
				"count":  len(pruned),
			})
		},
	}

	codebase.AddCommand(register, list, prune)
	return codebase
}
