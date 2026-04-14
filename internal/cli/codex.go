package cli

import (
	"fmt"

	"github.com/bytter/autoresearch/internal/integration"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/spf13/cobra"
)

func installCodexCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "codex",
		Short: "Install Codex integration (AGENTS.md block, docs, custom agents)",
	}
	c.AddCommand(installCodexDocsCmd(), installCodexAgentsCmd())
	c.RunE = func(cmd *cobra.Command, args []string) error {
		w := output.Default(globalJSON)
		wrote, err := writeCodexDoc(globalProjectDir, false, globalDryRun)
		if err != nil {
			return err
		}

		var instrRes integration.CodexInstructionsResult
		if globalDryRun {
			instrRes, err = integration.PreviewCodexInstructions(globalProjectDir)
		} else {
			instrRes, err = integration.EnsureCodexInstructions(globalProjectDir)
		}
		if err != nil {
			return fmt.Errorf("update AGENTS.md: %w", err)
		}

		var agentRes integration.AgentInstallResult
		if globalDryRun {
			agentRes, err = integration.PreviewCodexAgents(globalProjectDir)
		} else {
			agentRes, err = integration.InstallCodexAgents(globalProjectDir)
		}
		if err != nil {
			return fmt.Errorf("install codex agents: %w", err)
		}

		payload := map[string]any{
			"status":       "ok",
			"path":         wrote,
			"instructions": codexInstructionsResultToMap(instrRes),
			"agents":       map[string]any{"dir": agentRes.Dir, "files": agentRes.Written, "count": agentRes.Count},
		}
		if globalDryRun {
			payload["status"] = "dry-run"
			return w.Emit(
				fmt.Sprintf("[dry-run] would write %s\n[dry-run] AGENTS.md: %s\n[dry-run] agents: %d file(s) to %s",
					wrote, describeCodexInstructionsAction(instrRes), agentRes.Count, agentRes.Dir),
				payload,
			)
		}
		return w.Emit(
			fmt.Sprintf("wrote %s\nAGENTS.md: %s\nagents: wrote %d custom agent(s) to %s",
				wrote, describeCodexInstructionsAction(instrRes), agentRes.Count, agentRes.Dir),
			payload,
		)
	}
	return c
}

func installCodexDocsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs",
		Short: "Write .codex/autoresearch.md and AGENTS.md block only (no custom agents)",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			wrote, err := writeCodexDoc(globalProjectDir, false, globalDryRun)
			if err != nil {
				return err
			}

			var instrRes integration.CodexInstructionsResult
			if globalDryRun {
				instrRes, err = integration.PreviewCodexInstructions(globalProjectDir)
			} else {
				instrRes, err = integration.EnsureCodexInstructions(globalProjectDir)
			}
			if err != nil {
				return fmt.Errorf("update AGENTS.md: %w", err)
			}

			payload := map[string]any{
				"status":       "ok",
				"path":         wrote,
				"instructions": codexInstructionsResultToMap(instrRes),
			}
			if globalDryRun {
				payload["status"] = "dry-run"
				return w.Emit(
					fmt.Sprintf("[dry-run] would write %s\n[dry-run] AGENTS.md: %s",
						wrote, describeCodexInstructionsAction(instrRes)),
					payload,
				)
			}
			return w.Emit(
				fmt.Sprintf("wrote %s\nAGENTS.md: %s", wrote, describeCodexInstructionsAction(instrRes)),
				payload,
			)
		},
	}
}

func installCodexAgentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agents",
		Short: "Write the research-* custom-agent configs into .codex/agents/",
		Long: `Write the orchestrator and gate-reviewer custom-agent configs into the
target project's .codex/agents/ directory. Codex auto-discovers
project-scoped custom agents from that directory when the project is
trusted.

This command never touches non-research files in .codex/agents/. The
managed research-*.toml files are overwritten on every run, and legacy
managed research-*.md briefs from older autoresearch versions are
removed. If you want custom behavior, create a sibling custom agent with
a different name.

This is idempotent; run it after upgrading autoresearch to pull in
updated custom-agent configs.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			if globalDryRun {
				preview, err := integration.PreviewCodexAgents(globalProjectDir)
				if err != nil {
					return err
				}
				return w.Emit(
					fmt.Sprintf("[dry-run] would write %d custom agent(s) to %s", preview.Count, preview.Dir),
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
				fmt.Sprintf("wrote %d custom agent(s) to %s", res.Count, res.Dir),
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

func writeCodexDoc(projectDir string, _force, dryRun bool) (string, error) {
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
