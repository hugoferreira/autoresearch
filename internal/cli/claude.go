package cli

import (
	"errors"
	"fmt"

	"github.com/bytter/autoresearch/internal/integration"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/spf13/cobra"
)

// Satisfy the linter when subcommands need nothing else at package level.
var _ = errors.New

// Version is injected at link time via -ldflags; falls back to "dev" locally.
var Version = "dev"

func installClaudeCmd() *cobra.Command {
	var trustShell bool
	c := &cobra.Command{
		Use:   "claude",
		Short: "Install Claude Code integration (docs, settings, subagent prompts)",
	}
	c.AddCommand(installClaudeDocsCmd(), installClaudeAgentsCmd())
	c.PersistentFlags().BoolVar(&trustShell, "trust-shell", false,
		"add Bash(*) to allow all shell commands without prompts (subagents in worktrees run make, gcc, git, etc.)")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		w := output.Default(globalJSON)
		wrote, err := writeClaudeDoc(globalProjectDir, false, globalDryRun)
		if err != nil {
			return err
		}
		entries := claudeAllowEntries(trustShell)
		var settingsRes integration.ClaudeSettingsResult
		if globalDryRun {
			settingsRes, err = integration.PreviewClaudeSettings(globalProjectDir, entries)
		} else {
			settingsRes, err = integration.EnsureClaudeSettings(globalProjectDir, entries)
		}
		if err != nil {
			return fmt.Errorf("update claude settings: %w", err)
		}

		var agentRes integration.AgentInstallResult
		if globalDryRun {
			agentRes, err = integration.PreviewAgents(globalProjectDir)
		} else {
			agentRes, err = integration.InstallAgents(globalProjectDir)
		}
		if err != nil {
			return fmt.Errorf("install agents: %w", err)
		}

		payload := map[string]any{
			"status":   "ok",
			"path":     wrote,
			"settings": claudeSettingsResultToMap(settingsRes),
			"agents":   map[string]any{"dir": agentRes.Dir, "files": agentRes.Written, "count": agentRes.Count},
		}
		if globalDryRun {
			payload["status"] = "dry-run"
			return w.Emit(
				fmt.Sprintf("[dry-run] would write %s\n[dry-run] settings: %s\n[dry-run] agents: %d file(s) to %s",
					wrote, describeClaudeSettingsAction(settingsRes), agentRes.Count, agentRes.Dir),
				payload,
			)
		}
		return w.Emit(
			fmt.Sprintf("wrote %s\nsettings: %s\nagents: wrote %d prompt(s) to %s",
				wrote, describeClaudeSettingsAction(settingsRes), agentRes.Count, agentRes.Dir),
			payload,
		)
	}
	return c
}

func installClaudeDocsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs",
		Short: "Write .claude/autoresearch.md and settings only (no agent prompts)",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			wrote, err := writeClaudeDoc(globalProjectDir, false, globalDryRun)
			if err != nil {
				return err
			}

			trustShell, _ := cmd.Flags().GetBool("trust-shell")
			entries := claudeAllowEntries(trustShell)
			var settingsRes integration.ClaudeSettingsResult
			if globalDryRun {
				settingsRes, err = integration.PreviewClaudeSettings(globalProjectDir, entries)
			} else {
				settingsRes, err = integration.EnsureClaudeSettings(globalProjectDir, entries)
			}
			if err != nil {
				return fmt.Errorf("update claude settings: %w", err)
			}

			payload := map[string]any{
				"status":   "ok",
				"path":     wrote,
				"settings": claudeSettingsResultToMap(settingsRes),
			}
			if globalDryRun {
				payload["status"] = "dry-run"
				return w.Emit(
					fmt.Sprintf("[dry-run] would write %s\n[dry-run] settings: %s",
						wrote, describeClaudeSettingsAction(settingsRes)),
					payload,
				)
			}
			return w.Emit(
				fmt.Sprintf("wrote %s\nsettings: %s", wrote, describeClaudeSettingsAction(settingsRes)),
				payload,
			)
		},
	}
}

func installClaudeAgentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agents",
		Short: "Write the research-* subagent prompts into .claude/agents/",
		Long: `Write the orchestrator and gate-reviewer subagent prompts into the
target project's .claude/agents/ directory. Claude Code auto-discovers
these when you open the project, and the main session invokes them via
the Agent tool.

This command never touches non-research agent files in .claude/agents/.
The research-*.md files are fully managed — re-running this command
overwrites them with the current bundled version, so any hand edits you
made will be lost. If you want custom behavior, create a sibling agent
file with a different name.

This is idempotent; run it after upgrading autoresearch to pull in
updated prompts.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			if globalDryRun {
				preview, err := integration.PreviewAgents(globalProjectDir)
				if err != nil {
					return err
				}
				return w.Emit(
					fmt.Sprintf("[dry-run] would write %d agent file(s) to %s", preview.Count, preview.Dir),
					map[string]any{
						"status": "dry-run",
						"dir":    preview.Dir,
						"files":  preview.Written,
					},
				)
			}
			res, err := integration.InstallAgents(globalProjectDir)
			if err != nil {
				return fmt.Errorf("install agents: %w", err)
			}
			return w.Emit(
				fmt.Sprintf("wrote %d subagent prompt(s) to %s", res.Count, res.Dir),
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

// describeClaudeSettingsAction is the one-line human-readable summary of what
// EnsureClaudeSettings did, used in text output from `init` and
// `install claude`.
func describeClaudeSettingsAction(r integration.ClaudeSettingsResult) string {
	switch {
	case r.Created:
		return fmt.Sprintf("created %s with %d allow entry (entries=%v)", r.Path, len(r.Added), r.Added)
	case r.Updated:
		return fmt.Sprintf("added %d allow entry to %s (entries=%v)", len(r.Added), r.Path, r.Added)
	case r.AlreadyOK:
		return fmt.Sprintf("%s already has the autoresearch allow entry", r.Path)
	default:
		return "no change to " + r.Path
	}
}

func claudeSettingsResultToMap(r integration.ClaudeSettingsResult) map[string]any {
	action := "unchanged"
	switch {
	case r.Created:
		action = "created"
	case r.Updated:
		action = "updated"
	case r.AlreadyOK:
		action = "already_ok"
	}
	return map[string]any{
		"path":   r.Path,
		"action": action,
		"added":  r.Added,
	}
}

// claudeAllowEntries builds the permission entries list. Always includes the
// CLI allow and worktree file permissions. With trustShell, adds Bash(*)
// so subagents can run arbitrary shell commands without prompts.
func claudeAllowEntries(trustShell bool) []string {
	entries := []string{integration.AutoresearchAllowEntry}
	if trustShell {
		entries = append(entries, "Bash(*)")
	}
	if s, err := openStore(); err == nil {
		if wtRoot, err := s.WorktreesRoot(); err == nil {
			entries = append(entries, integration.WorktreeAllowEntries(wtRoot)...)
		}
	}
	return entries
}

// writeClaudeDoc writes .claude/autoresearch.md under projectDir.
func writeClaudeDoc(projectDir string, _force, dryRun bool) (string, error) {
	return installManagedDoc(projectDir, integration.ClaudeDocRelPath, integration.ClaudeDoc(Version), dryRun)
}
