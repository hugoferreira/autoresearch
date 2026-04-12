package cli

import (
	"errors"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func dashboardTuiCmd() *cobra.Command {
	var refresh int
	c := &cobra.Command{
		Use:   "tui",
		Short: "Interactive read-only TUI over the research state",
		Long: `Open a Bubble Tea TUI that lets you navigate every read-only surface
of autoresearch: dashboard, hypotheses, experiments, conclusions, event
log, tree, frontier, goal, and status.

The TUI never mutates .research/. Effectful actions (init, pause,
resume, goal set, hypothesis add, experiment design/implement, observe,
conclude, budget set) remain CLI-only — humans steer by talking to the
main agent session, which translates intent into CLI calls.

Press ? inside the TUI for a full key reference. Top-level jumps:
H hypotheses  E experiments  C conclusions  L log
T tree  F frontier  G goal  S status  D dashboard
A artifacts  I instruments  R report picker`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !term.IsTerminal(int(os.Stdout.Fd())) {
				return errors.New("dashboard tui requires a TTY; for scripting use `dashboard --json`")
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			if refresh < 1 {
				// Default cadence when --refresh is omitted or set to 0.
				refresh = 2
			}
			m := newTuiModel(s, time.Duration(refresh)*time.Second)
			p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
			_, err = p.Run()
			return err
		},
	}
	c.Flags().IntVar(&refresh, "refresh", 2, "seconds between background refreshes (min 1)")
	return c
}
