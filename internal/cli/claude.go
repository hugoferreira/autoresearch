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

func claudeCommands() []*cobra.Command {
	c := &cobra.Command{
		Use:   "claude",
		Short: "Manage Claude Code integration (agent-facing docs, subagent prompts)",
	}
	c.AddCommand(claudeInstallCmd(), claudeAgentsCommand())
	return []*cobra.Command{c}
}

func claudeAgentsCommand() *cobra.Command {
	a := &cobra.Command{
		Use:   "agents",
		Short: "Manage autoresearch's Claude Code subagent prompts",
	}
	a.AddCommand(claudeAgentsInstallCmd())
	return a
}

func claudeAgentsInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Write the six research-* subagent prompts into .claude/agents/",
		Long: `Write the generator, designer, implementer, observer, analyst, and
critic subagent prompts into the target project's .claude/agents/
directory. Claude Code auto-discovers these when you open the project,
and the main session invokes them via the Agent tool.

This command never touches non-research agent files in .claude/agents/.
The six research-*.md files are fully managed — re-running this command
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

func claudeInstallCmd() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "install",
		Short: "Write .claude/autoresearch.md so agents know about autoresearch",
		Long: `Write a Claude-facing reference document to
.claude/autoresearch.md in the target project. The file describes the CLI
surface, the strict-mode firewall, the entity lifecycle, and the agent
safety notes about bounded output.

This command NEVER touches the user's top-level CLAUDE.md. To make the main
session read the reference, add the line "@.claude/autoresearch.md" to your
CLAUDE.md yourself, or instruct subagents to read it directly.

The file is fully managed: running this command or ` + "`autoresearch init`" + `
again overwrites it with the current version. Do not edit it by hand —
your edits will be lost on the next refresh.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			wrote, err := installClaudeDoc(globalProjectDir, force, globalDryRun)
			if err != nil {
				return err
			}

			entries := []string{integration.AutoresearchAllowEntry}
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
	c.Flags().BoolVar(&force, "force", false, "overwrite the file even if it exists (default: yes — this file is always managed)")
	_ = force // currently unconditional overwrite; flag reserved for a future "refuse if modified" mode
	return c
}

// describeClaudeSettingsAction is the one-line human-readable summary of what
// EnsureClaudeSettings did, used in text output from `init` and
// `claude install`.
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

// installClaudeDoc writes .claude/autoresearch.md under projectDir. The file
// is fully managed by autoresearch: we overwrite unconditionally, since the
// doc is refreshed on every `init` / `claude install` call. We do NOT touch
// any sibling files (no CLAUDE.md, no .claude/agents/*).
func installClaudeDoc(projectDir string, _force, dryRun bool) (string, error) {
	return installManagedDoc(projectDir, integration.ClaudeDocRelPath, integration.ClaudeDoc(Version), dryRun)
}
