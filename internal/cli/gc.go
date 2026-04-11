package cli

import "github.com/spf13/cobra"

func gcCommands() []*cobra.Command {
	return []*cobra.Command{
		{
			Use:   "gc",
			Short: "Prune disposable state (worktrees); artifacts are preserved",
			RunE:  stub("gc"),
		},
	}
}
