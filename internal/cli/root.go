package cli

import (
	"github.com/spf13/cobra"
)

var (
	globalJSON       bool
	globalProjectDir string
	globalDryRun     bool
)

func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "autoresearch",
		Short: "Autonomous, agentic research over an existing codebase",
		Long: `autoresearch turns Claude Code or Codex into a disciplined scientific
researcher over a working codebase. It generates falsifiable
hypotheses, runs instrument-backed experiments in isolated git
worktrees, and draws statistically-sound conclusions — with a strict
firewall between speculation and observation.

autoresearch is for optimizing existing, working systems against
measurable goals. It is not a feature-delivery or program-synthesis tool.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().BoolVar(&globalJSON, "json", false, "emit machine-readable JSON on stdout")
	root.PersistentFlags().StringVarP(&globalProjectDir, "project-dir", "C", ".", "target project directory")
	root.PersistentFlags().BoolVar(&globalDryRun, "dry-run", false, "describe the action without mutating state")

	groups := [][]*cobra.Command{
		lifecycleCommands(),
		goalCommands(),
		steeringCommands(),
		hypothesisCommands(),
		experimentCommands(),
		observeCommands(),
		analyzeCommands(),
		concludeCommands(),
		treeCommands(),
		logCommands(),
		frontierCommands(),
		reportCommands(),
		gcCommands(),
		artifactCommands(),
		instrumentCommands(),
		claudeCommands(),
		codexCommands(),
		budgetCommands(),
		conclusionCommands(),
		dashboardCommands(),
	}
	for _, g := range groups {
		for _, c := range g {
			root.AddCommand(c)
		}
	}

	return root
}
