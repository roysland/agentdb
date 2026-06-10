package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/db"
)

func newBootstrapCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap",
		Short: "Apply schema and ensure required tables exist",
		RunE: func(cmd *cobra.Command, _ []string) error {
			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			stats, err := db.BootstrapSchema(cmd.Context(), conn, "data/schema.sql")
			if err != nil {
				return err
			}

			return printJSON(map[string]any{
				"status":             "ok",
				"statements_applied": stats.StatementsApplied,
				"source":             "data/schema.sql",
			})
		},
	}
}
