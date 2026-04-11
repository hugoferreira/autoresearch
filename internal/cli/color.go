package cli

import (
	"fmt"
	"io"
	"os"
	"regexp"

	"golang.org/x/term"
)

// ansi is a tiny color helper. It is safe to pass around as nil or disabled;
// every method is a no-op when the colorizer is off, so callers never need to
// branch on color support.
//
// Color is enabled only when:
//   - the destination is an *os.File that is a TTY
//   - the NO_COLOR environment variable is unset (https://no-color.org)
//
// In every other context (pipes, redirects, tests, --json mode) the helper
// returns raw strings unchanged.
type ansi struct {
	enabled bool
}

// newANSI inspects w and the environment to decide whether to emit ANSI
// escape sequences. Pass os.Stdout (or whatever the rendering destination is)
// so the detection matches the actual output target.
func newANSI(w io.Writer) *ansi {
	if os.Getenv("NO_COLOR") != "" {
		return &ansi{}
	}
	if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return &ansi{enabled: true}
	}
	return &ansi{}
}

// newANSIMode is the flag-driven variant: mode is one of "auto", "always", or
// "never". Auto delegates to newANSI (TTY + NO_COLOR detection). Always forces
// colors on even when piped (e.g. under `watch -c`, `less -R`, or a CI log
// collector that re-renders ANSI). Never forces them off.
func newANSIMode(w io.Writer, mode string) (*ansi, error) {
	switch mode {
	case "", "auto":
		return newANSI(w), nil
	case "always":
		return &ansi{enabled: true}, nil
	case "never":
		return &ansi{}, nil
	default:
		return nil, fmt.Errorf("invalid --color mode %q (want auto|always|never)", mode)
	}
}

func (a *ansi) wrap(code, s string) string {
	if a == nil || !a.enabled || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (a *ansi) bold(s string) string    { return a.wrap("1", s) }
func (a *ansi) dim(s string) string     { return a.wrap("2", s) }
func (a *ansi) red(s string) string     { return a.wrap("31", s) }
func (a *ansi) green(s string) string   { return a.wrap("32", s) }
func (a *ansi) yellow(s string) string  { return a.wrap("33", s) }
func (a *ansi) blue(s string) string    { return a.wrap("34", s) }
func (a *ansi) magenta(s string) string { return a.wrap("35", s) }
func (a *ansi) cyan(s string) string    { return a.wrap("36", s) }

// boldYellow and similar combinations show up often enough to warrant shortcuts.
func (a *ansi) boldYellow(s string) string { return a.bold(a.yellow(s)) }
func (a *ansi) boldRed(s string) string    { return a.bold(a.red(s)) }

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripANSI removes any ANSI escape sequences from s. Used by layout code
// that needs to measure visible width without counting escape characters.
func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }
