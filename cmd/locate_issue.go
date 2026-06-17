package cmd

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/observe"
	"github.com/roysland/agentdb/internal/search"
)

type locateIssueCmd struct {
	issueText   string
	codebaseID  int64
	workspaceID int64
	limit       int
}

func newLocateIssueCmd(ctx context.Context) *cobra.Command {
	lc := &locateIssueCmd{}

	cmd := &cobra.Command{
		Use:   "locate-issue",
		Short: "Locate likely impact area for a natural-language issue report",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return lc.run(ctx)
		},
	}

	cmd.Flags().StringVar(&lc.issueText, "issue-text", "", "Issue description in natural language")
	cmd.Flags().Int64Var(&lc.codebaseID, "codebase-id", 0, "Optional codebase scope")
	cmd.Flags().Int64Var(&lc.workspaceID, "workspace-id", 0, "Optional workspace scope")
	cmd.Flags().IntVar(&lc.limit, "limit", 10, "Maximum ranked candidates")

	return cmd
}

func (lc *locateIssueCmd) run(ctx context.Context) error {
	conn, err := db.Open(ctx, rootCfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	result, err := locateIssueImpactArea(ctx, conn, lc.issueText, lc.codebaseID, lc.workspaceID, lc.limit, nil)
	if err != nil {
		return err
	}

	return printJSON(result)
}

func locateIssueImpactArea(ctx context.Context, conn *sql.DB, issueText string, codebaseID, workspaceID int64, limit int, logger *observe.Logger) (map[string]any, error) {
	issueText = strings.TrimSpace(issueText)
	if issueText == "" {
		return nil, errors.New("issue_text is required")
	}

	if limit <= 0 {
		limit = 10
	}

	codebaseIDs, err := resolveScopedCodebaseIDs(ctx, conn, codebaseID, workspaceID)
	if err != nil {
		return nil, err
	}

	cfg := search.LocateIssueConfig{
		IssueText:   issueText,
		CodebaseIDs: codebaseIDs,
		Limit:       limit,
	}

	results, warning, err := search.LocateIssue(ctx, conn, cfg, logger)
	if err != nil {
		return nil, err
	}

	response := map[string]any{
		"candidates": results,
	}
	if warning != "" {
		response["warning"] = warning
	}
	if len(results) == 0 {
		response["message"] = "no strong matches found"
	}

	return response, nil
}
