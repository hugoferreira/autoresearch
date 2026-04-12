package cli

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/integration"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/worktree"
	"github.com/spf13/cobra"
)

func lifecycleCommands() []*cobra.Command {
	return []*cobra.Command{
		initCmd(),
		statusCmd(),
		pauseCmd(),
		resumeCmd(),
	}
}

func pauseCmd() *cobra.Command {
	var reason string
	c := &cobra.Command{
		Use:   "pause",
		Short: "Pause all mutating activity (subagents and humans alike)",
		Long: `Pause the research loop. While paused, every mutating CLI verb exits
non-zero with exit code 3 and a message pointing at "autoresearch resume".
Read-only verbs (status, log, tree, frontier, report, show/list/stat,
artifact navigation, hypothesis show, etc.) continue to work so humans
can inspect state before deciding to continue.

Re-pausing while already paused is a no-op.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			if st.Paused {
				return w.Emit(
					fmt.Sprintf("already paused: %s", st.PauseReason),
					map[string]any{"status": "noop", "paused": true, "reason": st.PauseReason},
				)
			}
			if globalDryRun {
				return w.Emit(
					fmt.Sprintf("[dry-run] would pause (reason=%q)", reason),
					map[string]any{"status": "dry-run", "reason": reason},
				)
			}
			now := nowUTC()
			err = s.UpdateState(func(st *store.State) error {
				st.Paused = true
				st.PauseReason = reason
				st.PausedAt = &now
				return nil
			})
			if err != nil {
				return err
			}
			if err := s.AppendEvent(store.Event{
				Kind:  "pause",
				Actor: "human",
				Data:  jsonRaw(map[string]string{"reason": reason}),
			}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("paused: %s", displayReason(reason)),
				map[string]any{"status": "ok", "paused": true, "reason": reason},
			)
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "human-readable reason for pausing")
	return c
}

func resumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Resume after a pause",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			if !st.Paused {
				return w.Emit(
					"already active",
					map[string]any{"status": "noop", "paused": false},
				)
			}
			prevReason := st.PauseReason
			if globalDryRun {
				return w.Emit(
					"[dry-run] would resume",
					map[string]any{"status": "dry-run"},
				)
			}
			err = s.UpdateState(func(st *store.State) error {
				st.Paused = false
				st.PauseReason = ""
				st.PausedAt = nil
				return nil
			})
			if err != nil {
				return err
			}
			if err := s.AppendEvent(store.Event{
				Kind:  "resume",
				Actor: "human",
				Data:  jsonRaw(map[string]string{"previous_reason": prevReason}),
			}); err != nil {
				return err
			}
			return w.Emit(
				"resumed",
				map[string]any{"status": "ok", "paused": false},
			)
		},
	}
}

func displayReason(reason string) string {
	if reason == "" {
		return "(no reason given)"
	}
	return reason
}

func initCmd() *cobra.Command {
	var buildCmd, testCmd string
	var trustShell bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize .research/ in the target project",
		Long: `Initialize autoresearch for the target project.

Runs the declared build and test commands against the untouched baseline
and refuses to initialize if either fails — you cannot distinguish a
genuine improvement from a regression without a working build and test
suite.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)

			if strings.TrimSpace(buildCmd) == "" || strings.TrimSpace(testCmd) == "" {
				return errors.New("--build-cmd and --test-cmd are required")
			}

			if _, err := store.Open(globalProjectDir); err == nil {
				return errors.New("autoresearch is already initialized in this directory; remove .research/ first")
			} else if !errors.Is(err, store.ErrNotInitialized) {
				return err
			}

			w.Textln("checking git precondition...")
			if !worktree.IsRepo(globalProjectDir) {
				return errors.New("target directory is not a git repository — autoresearch uses git worktrees to isolate experiments; run `git init` and make at least one commit before calling init")
			}

			w.Textln("checking build precondition...")
			if err := runProjectCommand(globalProjectDir, buildCmd); err != nil {
				return fmt.Errorf("build precondition failed:\n%w\n\nautoresearch requires a working build before it can manage experiments", err)
			}

			w.Textln("checking test precondition...")
			if err := runProjectCommand(globalProjectDir, testCmd); err != nil {
				return fmt.Errorf("test precondition failed:\n%w\n\nautoresearch requires a passing test suite — you cannot distinguish improvement from regression without one", err)
			}

			if globalDryRun {
				preview, err := integration.PreviewGitignoreLine(globalProjectDir, store.Dir+"/")
				if err != nil {
					return err
				}
				claudePath, err := writeClaudeDoc(globalProjectDir, false, true)
				if err != nil {
					return err
				}
				claudeAgents, err := integration.PreviewAgents(globalProjectDir)
				if err != nil {
					return err
				}
				claudeSettings, err := integration.PreviewClaudeSettings(globalProjectDir, claudeAllowEntries(trustShell))
				if err != nil {
					return err
				}
				codexPath, err := writeCodexDoc(globalProjectDir, false, true)
				if err != nil {
					return err
				}
				codexAgents, err := integration.PreviewCodexAgents(globalProjectDir)
				if err != nil {
					return err
				}
				codexInstructions, err := integration.PreviewCodexInstructions(globalProjectDir)
				if err != nil {
					return err
				}
				return w.Emit(
					fmt.Sprintf("[dry-run] would initialize .research/ under %s\n[dry-run] gitignore: %s\n[dry-run] claude doc: %s\n[dry-run] claude agents: %d file(s) to %s\n[dry-run] claude settings: %s\n[dry-run] codex doc: %s\n[dry-run] codex agents: %d file(s) to %s\n[dry-run] AGENTS.md: %s",
						globalProjectDir,
						describeGitignoreAction(preview),
						claudePath,
						claudeAgents.Count,
						claudeAgents.Dir,
						describeClaudeSettingsAction(claudeSettings),
						codexPath,
						codexAgents.Count,
						codexAgents.Dir,
						describeCodexInstructionsAction(codexInstructions)),
					map[string]any{
						"status":                "dry-run",
						"root":                  globalProjectDir,
						"build":                 "ok",
						"test":                  "ok",
						"gitignore":             gitignoreResultToMap(preview),
						"claude_doc":            claudePath,
						"claude_subagents_dir":  claudeAgents.Dir,
						"claude_subagent_files": claudeAgents.Written,
						"claude_subagent_count": claudeAgents.Count,
						"claude_settings":       claudeSettingsResultToMap(claudeSettings),
						"codex_doc":             codexPath,
						"codex_agents_dir":      codexAgents.Dir,
						"codex_agent_files":     codexAgents.Written,
						"codex_agent_count":     codexAgents.Count,
						"codex_instructions":    codexInstructionsResultToMap(codexInstructions),
					},
				)
			}

			cfg := store.Config{
				SchemaVersion: 1,
				Build:         store.CommandSpec{Command: buildCmd},
				Test:          store.CommandSpec{Command: testCmd},
				Mode:          "strict",
			}
			s, err := store.Create(globalProjectDir, cfg)
			if err != nil {
				return fmt.Errorf("create .research/: %w", err)
			}

			now := nowUTC()
			if err := s.UpdateState(func(st *store.State) error {
				st.ResearchStartedAt = &now
				return nil
			}); err != nil {
				return fmt.Errorf("record research_started_at: %w", err)
			}

			if err := s.AppendEvent(store.Event{Kind: "init", Actor: "system"}); err != nil {
				return fmt.Errorf("log init event: %w", err)
			}

			// Ensure .research/ and the worktree brief file are gitignored so
			// experiment metadata doesn't bleed into the main repo's history.
			gi, err := integration.EnsureGitignoreLine(globalProjectDir, store.Dir+"/")
			if err != nil {
				return fmt.Errorf("update .gitignore: %w", err)
			}
			if _, err := integration.EnsureGitignoreLine(globalProjectDir, entity.BriefFileName); err != nil {
				return fmt.Errorf("update .gitignore for brief: %w", err)
			}

			// Always install the Claude-facing reference doc. Never touches
			// the project's top-level CLAUDE.md — the user imports the
			// reference manually if they want it in the main session.
			claudePath, err := writeClaudeDoc(globalProjectDir, false, false)
			if err != nil {
				return fmt.Errorf("install claude doc: %w", err)
			}

			// Install the subagent prompts (orchestrator + gate-reviewer)
			// alongside the doc so the main session can invoke them
			// immediately. Non-research agent files are never touched.
			agentRes, err := integration.InstallAgents(globalProjectDir)
			if err != nil {
				return fmt.Errorf("install subagents: %w", err)
			}

			// Merge autoresearch's allow entries into .claude/settings.json
			// so the main session and subagents can invoke autoresearch verbs
			// and read/edit worktree files without permission prompts.
			settingsRes, err := integration.EnsureClaudeSettings(
				globalProjectDir,
				claudeAllowEntries(trustShell),
			)
			if err != nil {
				return fmt.Errorf("update claude settings: %w", err)
			}

			codexPath, err := writeCodexDoc(globalProjectDir, false, false)
			if err != nil {
				return fmt.Errorf("install codex doc: %w", err)
			}

			codexAgentRes, err := integration.InstallCodexAgents(globalProjectDir)
			if err != nil {
				return fmt.Errorf("install codex role briefs: %w", err)
			}

			codexInstructionsRes, err := integration.EnsureCodexInstructions(globalProjectDir)
			if err != nil {
				return fmt.Errorf("update AGENTS.md: %w", err)
			}

			return w.Emit(
				fmt.Sprintf("initialized .research/ at %s\ngitignore: %s\nclaude: wrote %s\nclaude: wrote %d subagent prompt(s) to %s\nclaude: settings: %s\ncodex: wrote %s\ncodex: wrote %d role brief(s) to %s\ncodex: AGENTS.md: %s\n(to load the Claude reference into Claude Code's main session, add `@.claude/autoresearch.md` to your CLAUDE.md)",
					s.DirPath(),
					describeGitignoreAction(gi),
					claudePath,
					agentRes.Count,
					agentRes.Dir,
					describeClaudeSettingsAction(settingsRes),
					codexPath,
					codexAgentRes.Count,
					codexAgentRes.Dir,
					describeCodexInstructionsAction(codexInstructionsRes)),
				map[string]any{
					"status":             "ok",
					"root":               s.Root(),
					"dir":                s.DirPath(),
					"build":              "ok",
					"test":               "ok",
					"claude_doc":         claudePath,
					"gitignore":          gitignoreResultToMap(gi),
					"subagents_dir":      agentRes.Dir,
					"subagent_files":     agentRes.Written,
					"subagent_count":     agentRes.Count,
					"settings":           claudeSettingsResultToMap(settingsRes),
					"codex_doc":          codexPath,
					"codex_agents_dir":   codexAgentRes.Dir,
					"codex_agent_files":  codexAgentRes.Written,
					"codex_agent_count":  codexAgentRes.Count,
					"codex_instructions": codexInstructionsResultToMap(codexInstructionsRes),
				},
			)
		},
	}
	cmd.Flags().StringVar(&buildCmd, "build-cmd", "", "shell command that builds the project (required)")
	cmd.Flags().StringVar(&testCmd, "test-cmd", "", "shell command that runs the test suite (required)")
	cmd.Flags().BoolVar(&trustShell, "trust-shell", false, "add Bash(*) to .claude/settings.json so subagents can run any shell command without prompts")
	return cmd
}

func describeGitignoreAction(r integration.GitignoreResult) string {
	switch {
	case r.Created:
		return "created " + r.Path + " with .research/"
	case r.Added:
		return "added .research/ to " + r.Path
	case r.AlreadyPresent:
		return ".research/ already in " + r.Path
	default:
		return "no change to " + r.Path
	}
}

func gitignoreResultToMap(r integration.GitignoreResult) map[string]any {
	action := "unchanged"
	switch {
	case r.Created:
		action = "created"
	case r.Added:
		action = "added"
	case r.AlreadyPresent:
		action = "already_present"
	}
	return map[string]any{
		"path":   r.Path,
		"action": action,
	}
}

func runProjectCommand(projectDir, shellCmd string) error {
	c := exec.Command("sh", "-c", shellCmd)
	c.Dir = projectDir
	out, err := c.CombinedOutput()
	if err != nil {
		excerpt := strings.TrimSpace(string(out))
		if len(excerpt) > 500 {
			excerpt = "..." + excerpt[len(excerpt)-500:]
		}
		return fmt.Errorf("command %q failed: %v\n%s", shellCmd, err, excerpt)
	}
	return nil
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show pause state, budget consumption, and entity counts",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)

			s, err := store.Open(globalProjectDir)
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			counts, err := s.Counts()
			if err != nil {
				return err
			}
			cfg, err := s.Config()
			if err != nil {
				return err
			}

			payload := map[string]any{
				"root":            s.Root(),
				"dir":             s.DirPath(),
				"mode":            cfg.Mode,
				"paused":          st.Paused,
				"pause_reason":    st.PauseReason,
				"current_goal_id": st.CurrentGoalID,
				"counts":          counts,
				"last_event_at":   st.LastEventAt,
			}

			if w.IsJSON() {
				return w.JSON(payload)
			}

			w.Textf("root:           %s\n", s.Root())
			w.Textf("dir:            %s\n", s.DirPath())
			w.Textf("mode:           %s\n", cfg.Mode)
			if st.CurrentGoalID != "" {
				w.Textf("active goal:    %s\n", st.CurrentGoalID)
			} else {
				w.Textln("active goal:    (none)")
			}
			if st.Paused {
				reason := st.PauseReason
				if reason == "" {
					reason = "(no reason given)"
				}
				w.Textf("state:          PAUSED — %s\n", reason)
			} else {
				w.Textln("state:          active")
			}
			w.Textf("hypotheses:     %d\n", counts["hypotheses"])
			w.Textf("experiments:    %d\n", counts["experiments"])
			w.Textf("observations:   %d\n", counts["observations"])
			w.Textf("conclusions:    %d\n", counts["conclusions"])
			if st.LastEventAt != nil {
				w.Textf("last event:     %s\n", st.LastEventAt.Format(time.RFC3339))
			} else {
				w.Textln("last event:     (none)")
			}
			return nil
		},
	}
}
