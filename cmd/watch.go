package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/config"
	"github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/observe"
	"github.com/roysland/agentdb/internal/store"
	"github.com/roysland/agentdb/internal/watch"
)

func newWatchCmd(ctx context.Context) *cobra.Command {
	var (
		codebaseID   int64
		codebasePath string
		debounceMs   int
		analyze      bool
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch a codebase for file changes and trigger incremental re-indexing",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve config and open database.
			resolved := config.Resolve(rootCfg)
			dbConn, err := db.Open(ctx, resolved)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer dbConn.Close()

			catalogRepo := store.NewCatalogRepo(dbConn)
			resolvedID, resolvedPath, err := resolveCodebaseTarget(ctx, catalogRepo, resolved, codebaseID, "", codebasePath, false)
			if err != nil {
				return err
			}

			logger := observe.NewLogger(observe.LevelInfo, os.Stderr)

			cfg := watch.Config{
				CodebaseID:   resolvedID,
				CodebasePath: resolvedPath,
				DebounceMs:   debounceMs,
				Analyze:      analyze,
			}

			watcher, err := watch.New(cfg, dbConn, logger)
			if err != nil {
				return fmt.Errorf("create watcher: %w", err)
			}

			// Set up signal handling: intercept SIGINT/SIGTERM, cancel context,
			// and wait for in-progress re-index to complete.
			watchCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			// Run watcher in a goroutine so we can listen for signals.
			errCh := make(chan error, 1)
			go func() {
				errCh <- watcher.Run(watchCtx)
			}()
			fmt.Println("Watching for file changes... (press Ctrl+C to stop)")

			select {
			case sig := <-sigCh:
				logger.Log(observe.LogEntry{
					Level:     "info",
					Operation: "watch_signal",
					Status:    fmt.Sprintf("received %s, shutting down gracefully", sig),
				})
				cancel()
				// Wait for watcher to finish (completes in-progress re-index).
				return <-errCh
			case err := <-errCh:
				return err
			}
		},
	}

	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID to watch")
	cmd.Flags().StringVar(&codebasePath, "codebase-path", "", "Path to codebase root directory")
	cmd.Flags().IntVar(&debounceMs, "debounce", 500, "Debounce window in milliseconds")
	cmd.Flags().BoolVar(&analyze, "analyze", false, "Run symbol/relationship extraction after chunk indexing")

	return cmd
}
