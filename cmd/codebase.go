package cmd

import (
	"context"
	"errors"
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

	codebase.AddCommand(register, list)
	return codebase
}
