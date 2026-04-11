package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/bytter/autoresearch/internal/cli"
)

// Exit codes:
//
//   0 — success
//   1 — generic error
//   2 — cobra usage error (set by cobra on bad flags; we don't override)
//   3 — autoresearch is paused (cli.ErrPaused)
//   4 — budget exhausted    (cli.ErrBudgetExhausted)
//
// Orchestrators use codes 3 and 4 to decide whether to stop the loop cleanly
// versus fail loudly.
func main() {
	if err := cli.Root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		switch {
		case errors.Is(err, cli.ErrPaused):
			os.Exit(3)
		case errors.Is(err, cli.ErrBudgetExhausted):
			os.Exit(4)
		default:
			os.Exit(1)
		}
	}
}
