package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/artifact"
	"github.com/roysland/agentdb/internal/db"
)

func newImportCmd(ctx context.Context) *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "import <artifact-path>",
		Short: "Import a codebase from a portable .agentdb artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			artifactPath := args[0]

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			opts := artifact.ImportOptions{
				ArtifactPath: artifactPath,
				NameOverride: name,
			}

			if err := artifact.Import(cmd.Context(), conn, opts); err != nil {
				return err
			}

			// Query the imported codebase name for the success message.
			var codebaseName string
			row := conn.QueryRowContext(cmd.Context(),
				"SELECT name FROM codebases ORDER BY rowid DESC LIMIT 1")
			if err := row.Scan(&codebaseName); err != nil {
				codebaseName = artifactPath
			}

			fmt.Fprintf(os.Stderr, "imported codebase %q from %s\n", codebaseName, artifactPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Override the codebase name from the artifact")

	return cmd
}
