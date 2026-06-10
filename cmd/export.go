package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/artifact"
	"github.com/roysland/agentdb/internal/db"
)

func newExportCmd(ctx context.Context) *cobra.Command {
	var codebaseID int64
	var output string
	var includeEmbeddings bool
	var stripSource bool

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export a codebase to a portable .agentdb artifact",
		RunE: func(cmd *cobra.Command, _ []string) error {
			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			opts := artifact.ExportOptions{
				CodebaseID:        codebaseID,
				OutputPath:        output,
				IncludeEmbeddings: includeEmbeddings,
				StripSource:       stripSource,
			}

			if err := artifact.Export(cmd.Context(), conn, opts); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "exported codebase %d to %s\n", codebaseID, output)
			return nil
		},
	}

	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "ID of the codebase to export (required)")
	cmd.Flags().StringVar(&output, "output", "", "Output path for the artifact file (required)")
	cmd.Flags().BoolVar(&includeEmbeddings, "include-embeddings", false, "Include symbol and chunk embeddings in the exported artifact")
	cmd.Flags().BoolVar(&stripSource, "strip-source", false, "Strip source-bearing text from exported chunks and symbols (snippet/doc_comment/body_snippet)")
	_ = cmd.MarkFlagRequired("codebase-id")
	_ = cmd.MarkFlagRequired("output")

	return cmd
}
