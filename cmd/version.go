package cmd

import (
	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/db"
)

const version = "0.1.0"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print CLI version",
		RunE: func(_ *cobra.Command, _ []string) error {
			return printJSON(map[string]string{
				"version":        version,
				"schema_version": db.CurrentSchemaVersion,
			})
		},
	}
}
