package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/config"
)

var rootCfg = config.Runtime{}

func Execute(ctx context.Context) error {
	cmd := newRootCmd(ctx)
	return cmd.Execute()
}

func newRootCmd(ctx context.Context) *cobra.Command {
	root := &cobra.Command{
		Use:   "agentdb",
		Short: "CLI for agent memory and codebase metadata",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			resolved := config.Resolve(rootCfg)
			rootCfg = resolved
			_ = config.SaveDefaultDatabaseURL(rootCfg.DatabaseURL)
			return nil
		},
	}

	root.PersistentFlags().StringVar(&rootCfg.DatabaseURL, "db-url", "", "Database URL or file path (env: AGENTDB_DB_URL)")
	root.PersistentFlags().StringVar(&rootCfg.DatabaseDriver, "db-driver", "", "Database driver mode: auto|sqlite3 (env: AGENTDB_DB_DRIVER)")
	root.PersistentFlags().StringVar(&rootCfg.ProjectPath, "project-path", "", "Default project path context (env: AGENTDB_PROJECT_PATH)")

	root.AddCommand(newBootstrapCmd(ctx))
	root.AddCommand(newMemoryCmd(ctx))
	root.AddCommand(newCodebaseCmd(ctx))
	root.AddCommand(newIndexCmd(ctx))
	root.AddCommand(newAnalyzeCmd(ctx))
	root.AddCommand(newLocateIssueCmd(ctx))
	root.AddCommand(newMCPCmd(ctx))
	root.AddCommand(newExportCmd(ctx))
	root.AddCommand(newImportCmd(ctx))
	root.AddCommand(newWatchCmd(ctx))
	root.AddCommand(newWorkspaceCmd(ctx))
	root.AddCommand(newVersionCmd())
	root.AddCommand(newSearchCmd(ctx))
	root.AddCommand(newFindSymbolCmd(ctx))
	root.AddCommand(newFindUsagesCmd(ctx))
	root.AddCommand(newGetCallersCmd(ctx))
	root.AddCommand(newGetCalleesCmd(ctx))
	root.AddCommand(newGetFileSymbolsCmd(ctx))
	root.AddCommand(newGetImportsCmd(ctx))
	root.AddCommand(newProjectOverviewCmd(ctx))
	root.AddCommand(newIndexStatusCmd(ctx))

	return root
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func exitWithError(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err.Error())
}
