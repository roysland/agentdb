package cmd

import (
	"context"
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/roysland/agentdb/internal/db"
	"github.com/roysland/agentdb/internal/store"
)

func newWorkspaceCmd(ctx context.Context) *cobra.Command {
	workspace := &cobra.Command{Use: "workspace", Short: "Manage cross-repository workspaces"}

	workspace.AddCommand(
		newWorkspaceCreateCmd(ctx),
		newWorkspaceAddCmd(ctx),
		newWorkspaceRemoveCmd(ctx),
		newWorkspaceListCmd(ctx),
	)
	return workspace
}

func newWorkspaceCreateCmd(ctx context.Context) *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return errors.New("--name is required")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewWorkspaceRepo(conn)
			id, err := repo.Create(cmd.Context(), name)
			if err != nil {
				return err
			}

			return printJSON(map[string]any{"id": id, "name": name})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Workspace name")
	return cmd
}

func newWorkspaceAddCmd(ctx context.Context) *cobra.Command {
	var workspaceName string
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a codebase to a workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(workspaceName) == "" {
				return errors.New("--workspace is required")
			}
			if codebaseID <= 0 {
				return errors.New("--codebase-id is required and must be positive")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewWorkspaceRepo(conn)
			ws, err := repo.GetByName(cmd.Context(), workspaceName)
			if err != nil {
				return err
			}

			if err := repo.AddMember(cmd.Context(), ws.ID, codebaseID); err != nil {
				return err
			}

			return printJSON(map[string]any{
				"workspace_id": ws.ID,
				"workspace":    workspaceName,
				"codebase_id":  codebaseID,
				"action":       "added",
			})
		},
	}
	cmd.Flags().StringVar(&workspaceName, "workspace", "", "Workspace name")
	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID to add")
	return cmd
}

func newWorkspaceRemoveCmd(ctx context.Context) *cobra.Command {
	var workspaceName string
	var codebaseID int64

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a codebase from a workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(workspaceName) == "" {
				return errors.New("--workspace is required")
			}
			if codebaseID <= 0 {
				return errors.New("--codebase-id is required and must be positive")
			}

			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewWorkspaceRepo(conn)
			ws, err := repo.GetByName(cmd.Context(), workspaceName)
			if err != nil {
				return err
			}

			if err := repo.RemoveMember(cmd.Context(), ws.ID, codebaseID); err != nil {
				return err
			}

			return printJSON(map[string]any{
				"workspace_id": ws.ID,
				"workspace":    workspaceName,
				"codebase_id":  codebaseID,
				"action":       "removed",
			})
		},
	}
	cmd.Flags().StringVar(&workspaceName, "workspace", "", "Workspace name")
	cmd.Flags().Int64Var(&codebaseID, "codebase-id", 0, "Codebase ID to remove")
	return cmd
}

func newWorkspaceListCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all workspaces and their members",
		RunE: func(cmd *cobra.Command, _ []string) error {
			conn, err := db.Open(ctx, rootCfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			repo := store.NewWorkspaceRepo(conn)
			workspaces, err := repo.List(cmd.Context())
			if err != nil {
				return err
			}

			type workspaceOutput struct {
				ID        int64            `json:"id"`
				Name      string           `json:"name"`
				CreatedAt int64            `json:"created_at"`
				Members   []store.Codebase `json:"members"`
			}

			out := make([]workspaceOutput, 0, len(workspaces))
			for _, ws := range workspaces {
				members, err := repo.GetMembers(cmd.Context(), ws.ID)
				if err != nil {
					return err
				}
				out = append(out, workspaceOutput{
					ID:        ws.ID,
					Name:      ws.Name,
					CreatedAt: ws.CreatedAt,
					Members:   members,
				})
			}

			return printJSON(out)
		},
	}
}
