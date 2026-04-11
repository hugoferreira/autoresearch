package cli

import "errors"

// Sentinel errors the binary's main() inspects to pick a specific exit code.
// Wrap with fmt.Errorf("%w: ...", ErrX) when returning from a RunE.
var (
	// ErrPaused is returned by mutating verbs when .research/state.json has
	// Paused=true. Orchestrators treat exit code 3 as "paused" and stop.
	ErrPaused = errors.New("autoresearch is paused")

	// ErrBudgetExhausted is returned when a budget constraint would be
	// violated by a new mutation (currently: max_experiments or
	// max_wall_time_h on experiment design). Orchestrators treat exit code
	// 4 as "budget exhausted" and stop the loop cleanly.
	ErrBudgetExhausted = errors.New("budget exhausted")
)
