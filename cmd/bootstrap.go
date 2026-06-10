package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/config"
	"github.com/roysland/agentdb/internal/db"
)

func newBootstrapCmd(ctx context.Context) *cobra.Command {
	var newDB bool

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Apply schema and ensure required tables exist",
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved := config.Resolve(rootCfg)

			if newDB {
				dbPath := resolved.DatabaseURL
				if strings.Contains(dbPath, "://") {
					return fmt.Errorf("--new only works with local file databases, not %s", dbPath)
				}
				if _, err := os.Stat(dbPath); err == nil {
					backupPath := fmt.Sprintf("%s.bak.%d", dbPath, time.Now().Unix())
					if err := os.Rename(dbPath, backupPath); err != nil {
						return fmt.Errorf("rename existing database: %w", err)
					}
					_, _ = fmt.Fprintf(os.Stderr, "agentdb: existing database backed up to %s\n", backupPath)
				}
			}

			conn, err := db.Open(ctx, resolved)
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

	cmd.Flags().BoolVar(&newDB, "new", false, "Rename the existing database to a timestamped backup and create a fresh one")

	return cmd
}
