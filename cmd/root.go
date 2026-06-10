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
	root.PersistentFlags().StringVar(&rootCfg.EmbeddingProvider, "embed-provider", "", "Embedding provider: disabled|ollama (env: AGENTDB_EMBED_PROVIDER)")
	root.PersistentFlags().StringVar(&rootCfg.EmbeddingBaseURL, "embed-base-url", "", "Embedding API base URL (env: AGENTDB_EMBED_BASE_URL)")
	root.PersistentFlags().StringVar(&rootCfg.EmbeddingAPIKey, "embed-api-key", "", "Embedding API key (env: AGENTDB_EMBED_API_KEY)")
	root.PersistentFlags().StringVar(&rootCfg.EmbeddingModel, "embed-model", "", "Embedding model name (env: AGENTDB_EMBED_MODEL)")
	root.PersistentFlags().IntVar(&rootCfg.EmbeddingTimeoutSeconds, "embed-timeout-seconds", 0, "Embedding HTTP timeout seconds (env: AGENTDB_EMBED_TIMEOUT_SECONDS)")

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
