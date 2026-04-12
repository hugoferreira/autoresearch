package cli

import (
	"fmt"

	"github.com/bytter/autoresearch/internal/integration"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/spf13/cobra"
)

func codexCommands() []*cobra.Command {
	c := &cobra.Command{
		Use:   "codex",
		Short: "Manage Codex integration (AGENTS.md block, .codex docs, role briefs)",
	}
	c.AddCommand(codexInstallCmd(), codexAgentsCommand())
	return []*cobra.Command{c}
}

func codexAgentsCommand() *cobra.Command {
	a := &cobra.Command{
		Use:   "agents",
		Short: "Manage autoresearch's Codex role briefs",
	}
	a.AddCommand(codexAgentsInstallCmd())
	return a
}

func codexAgentsInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Write the six research-* role briefs into .codex/agents/",
		Long: `Write the generator, designer, implementer, observer, analyst, and
critic role briefs into the target project's .codex/agents/ directory.
Codex does not auto-discover these, but the main session can read the
matching brief before calling spawn_agent.

This command never touches non-research files in .codex/agents/. The
six research-*.md files are fully managed — re-running this command
overwrites them with the current bundled version, so any hand edits you
made will be lost. If you want custom behavior, create a sibling file
with a different name.

This is idempotent; run it after upgrading autoresearch to pull in
updated role briefs.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			if globalDryRun {
				preview, err := integration.PreviewCodexAgents(globalProjectDir)
				if err != nil {
					return err
				}
				return w.Emit(
					fmt.Sprintf("[dry-run] would write %d role brief(s) to %s", preview.Count, preview.Dir),
					map[string]any{
						"status": "dry-run",
						"dir":    preview.Dir,
						"files":  preview.Written,
					},
				)
			}
			res, err := integration.InstallCodexAgents(globalProjectDir)
			if err != nil {
				return fmt.Errorf("install codex agents: %w", err)
			}
			return w.Emit(
				fmt.Sprintf("wrote %d role brief(s) to %s", res.Count, res.Dir),
				map[string]any{
					"status": "ok",
					"dir":    res.Dir,
					"files":  res.Written,
					"count":  res.Count,
				},
			)
		},
	}
}

func codexInstallCmd() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "install",
		Short: "Write .codex/autoresearch.md and maintain the AGENTS.md autoresearch block",
		Long: `Write a Codex-facing reference document to
.codex/autoresearch.md in the target project, and ensure that the root
AGENTS.md contains a managed autoresearch block pointing Codex at that
reference.

If AGENTS.md already exists, only the managed autoresearch block is
added or refreshed; user-owned AGENTS.md content outside that block is
preserved. If AGENTS.md does not exist, it is created with the managed
block as its initial content.

The .codex/autoresearch.md file is fully managed: running this command
or ` + "`autoresearch init`" + ` again overwrites it with the current version.
Do not edit it by hand — your edits will be lost on the next refresh.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			wrote, err := installCodexDoc(globalProjectDir, force, globalDryRun)
			if err != nil {
				return err
			}

			var agentsRes integration.CodexInstructionsResult
			if globalDryRun {
				agentsRes, err = integration.PreviewCodexInstructions(globalProjectDir)
			} else {
				agentsRes, err = integration.EnsureCodexInstructions(globalProjectDir)
			}
			if err != nil {
				return fmt.Errorf("update AGENTS.md: %w", err)
			}

			payload := map[string]any{
				"status":       "ok",
				"path":         wrote,
				"instructions": codexInstructionsResultToMap(agentsRes),
			}
			if globalDryRun {
				payload["status"] = "dry-run"
				return w.Emit(
					fmt.Sprintf("[dry-run] would write %s\n[dry-run] AGENTS.md: %s",
						wrote, describeCodexInstructionsAction(agentsRes)),
					payload,
				)
			}
			return w.Emit(
				fmt.Sprintf("wrote %s\nAGENTS.md: %s", wrote, describeCodexInstructionsAction(agentsRes)),
				payload,
			)
		},
	}
	c.Flags().BoolVar(&force, "force", false, "overwrite the file even if it exists (default: yes — this file is always managed)")
	_ = force
	return c
}

func installCodexDoc(projectDir string, _force, dryRun bool) (string, error) {
	return installManagedDoc(projectDir, integration.CodexDocRelPath, integration.CodexDoc(Version), dryRun)
}

func describeCodexInstructionsAction(r integration.CodexInstructionsResult) string {
	switch {
	case r.Created:
		return fmt.Sprintf("created %s with the autoresearch Codex block", r.Path)
	case r.Added:
		return fmt.Sprintf("added the autoresearch Codex block to %s", r.Path)
	case r.Updated:
		return fmt.Sprintf("updated the autoresearch Codex block in %s", r.Path)
	case r.AlreadyOK:
		return fmt.Sprintf("%s already has the autoresearch Codex block", r.Path)
	default:
		return "no change to " + r.Path
	}
}

func codexInstructionsResultToMap(r integration.CodexInstructionsResult) map[string]any {
	action := "unchanged"
	switch {
	case r.Created:
		action = "created"
	case r.Added:
		action = "added"
	case r.Updated:
		action = "updated"
	case r.AlreadyOK:
		action = "already_ok"
	}
	return map[string]any{
		"path":   r.Path,
		"action": action,
	}
}
