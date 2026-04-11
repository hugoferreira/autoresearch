package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func dashboardCommands() []*cobra.Command {
	return []*cobra.Command{dashboardCmd()}
}

func dashboardCmd() *cobra.Command {
	var (
		refresh   int
		colorMode string
	)
	c := &cobra.Command{
		Use:   "dashboard",
		Short: "Composite live view of the research loop (goal, tree, frontier, in-flight, events)",
		Long: `Render a composite read-only snapshot of the research state: goal and
constraints, budget usage, hypothesis tree, frontier, in-flight
experiments, and the last 10 events. One-shot by default; pass
--refresh N (seconds, min 1) to stay alive and auto-redraw.

The dashboard is read-only. It never mutates .research/; it works fine
while the store is paused; and it is not a steering surface. Humans
steer by talking to the main Claude session, which translates intent
into CLI calls.

Use --json for a structured one-shot snapshot suitable for external
tooling. --refresh is rejected in --json mode (use a polling loop
externally if you need streaming JSON).

Colors auto-enable on a TTY and disable when piped, so tools like
watch(1) strip them by default. Pass --color=always to force ANSI
output (recommended under ` + "`watch -c autoresearch dashboard`" + `).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if globalJSON && refresh > 0 {
				return errors.New("--refresh is not supported in --json mode (use a polling loop externally)")
			}
			colors, err := newANSIMode(os.Stdout, colorMode)
			if err != nil {
				return err
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			if globalJSON {
				snap, err := captureDashboard(s)
				if err != nil {
					return err
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(snap)
			}
			if refresh == 0 {
				return runDashboardOnce(s, os.Stdout, colors)
			}
			if refresh < 1 {
				return errors.New("--refresh must be at least 1 second")
			}
			if !term.IsTerminal(int(os.Stdout.Fd())) {
				return errors.New("dashboard --refresh requires a TTY; for scripting use one-shot `dashboard --json`")
			}
			return runDashboardLoop(s, os.Stdout, time.Duration(refresh)*time.Second, colors)
		},
	}
	c.Flags().IntVar(&refresh, "refresh", 0, "seconds between auto-refreshes (0 = one-shot, requires a TTY when > 0)")
	c.Flags().StringVar(&colorMode, "color", "auto", "color output: auto (TTY-detect), always (force on, for `watch -c`), never")
	return c
}

// ---- snapshot capture ----

type dashboardSnapshot struct {
	Project      string              `json:"project"`
	Paused       bool                `json:"paused"`
	PauseReason  string              `json:"pause_reason,omitempty"`
	Mode         string              `json:"mode"`
	Goal         *entity.Goal        `json:"goal,omitempty"`
	Budgets      dashboardBudgets    `json:"budgets"`
	Counts       map[string]int      `json:"counts"`
	Tree         []*treeNode         `json:"tree"`
	Frontier     []frontierRow       `json:"frontier"`
	StalledFor   int                 `json:"stalled_for"`
	InFlight     []dashboardInFlight `json:"in_flight"`
	RecentEvents []store.Event       `json:"recent_events"`
	CapturedAt   time.Time           `json:"captured_at"`
}

type dashboardBudgets struct {
	Limits struct {
		MaxExperiments int `json:"max_experiments"`
		MaxWallTimeH   int `json:"max_wall_time_h"`
		FrontierStallK int `json:"frontier_stall_k"`
	} `json:"limits"`
	Usage struct {
		Experiments int     `json:"experiments"`
		ElapsedH    float64 `json:"elapsed_h"`
	} `json:"usage"`
}

type dashboardInFlight struct {
	ID            string     `json:"id"`
	Hypothesis    string     `json:"hypothesis"`
	Status        string     `json:"status"`
	Tier          string     `json:"tier"`
	Instruments   []string   `json:"instruments"`
	ImplementedAt *time.Time `json:"implemented_at,omitempty"`
	ElapsedS      float64    `json:"elapsed_s"`
}

// captureDashboard gathers everything the dashboard renders. All reads go
// through existing store methods — no new store APIs, no mutation.
func captureDashboard(s *store.Store) (*dashboardSnapshot, error) {
	snap := &dashboardSnapshot{
		Project:    s.Root(),
		CapturedAt: time.Now().UTC(),
		Tree:       []*treeNode{},
		Frontier:   []frontierRow{},
		InFlight:   []dashboardInFlight{},
	}

	cfg, err := s.Config()
	if err != nil {
		return nil, err
	}
	snap.Mode = cfg.Mode

	st, err := s.State()
	if err != nil {
		return nil, err
	}
	snap.Paused = st.Paused
	snap.PauseReason = st.PauseReason

	if g, err := s.ReadGoal(); err == nil {
		snap.Goal = g
	}

	counts, err := s.Counts()
	if err != nil {
		return nil, err
	}
	snap.Counts = counts

	snap.Budgets.Limits.MaxExperiments = cfg.Budgets.MaxExperiments
	snap.Budgets.Limits.MaxWallTimeH = cfg.Budgets.MaxWallTimeH
	snap.Budgets.Limits.FrontierStallK = cfg.Budgets.FrontierStallK
	snap.Budgets.Usage.Experiments = st.Counters["E"]
	if st.ResearchStartedAt != nil {
		snap.Budgets.Usage.ElapsedH = time.Since(*st.ResearchStartedAt).Hours()
	}

	hyps, err := s.ListHypotheses()
	if err != nil {
		return nil, err
	}
	roots, children := buildHypothesisForest(hyps)
	snap.Tree = buildTreeJSON(roots, children)

	if snap.Goal != nil {
		concls, err := s.ListConclusions()
		if err != nil {
			return nil, err
		}
		rows, stalled := computeFrontier(s, snap.Goal, concls)
		snap.Frontier = rows
		snap.StalledFor = stalled
	}

	exps, err := s.ListExperiments()
	if err != nil {
		return nil, err
	}
	for _, e := range exps {
		if e.Status != entity.ExpImplemented && e.Status != entity.ExpMeasured {
			continue
		}
		row := dashboardInFlight{
			ID:          e.ID,
			Hypothesis:  e.Hypothesis,
			Status:      e.Status,
			Tier:        e.Tier,
			Instruments: append([]string{}, e.Instruments...),
		}
		if impAt := findImplementedAt(s, e.ID); impAt != nil {
			row.ImplementedAt = impAt
			row.ElapsedS = time.Since(*impAt).Seconds()
		}
		snap.InFlight = append(snap.InFlight, row)
	}
	sort.SliceStable(snap.InFlight, func(i, j int) bool {
		a, b := snap.InFlight[i].ImplementedAt, snap.InFlight[j].ImplementedAt
		if a == nil && b == nil {
			return snap.InFlight[i].ID < snap.InFlight[j].ID
		}
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return a.After(*b)
	})

	events, err := s.Events(10)
	if err != nil {
		return nil, err
	}
	snap.RecentEvents = events

	return snap, nil
}

func findImplementedAt(s *store.Store, expID string) *time.Time {
	events, err := s.Events(0)
	if err != nil {
		return nil
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Subject == expID && e.Kind == "experiment.implement" {
			ts := e.Ts
			return &ts
		}
	}
	return nil
}

// ---- one-shot + refresh loop ----

func runDashboardOnce(s *store.Store, w io.Writer, colors *ansi) error {
	snap, err := captureDashboard(s)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	renderDashboard(&buf, snap, termWidth(), "snapshot", colors)
	_, err = io.Copy(w, &buf)
	return err
}

func runDashboardLoop(s *store.Store, w io.Writer, refresh time.Duration, colors *ansi) error {
	_, _ = io.WriteString(w, "\x1b[?25l")
	defer io.WriteString(w, "\x1b[?25h\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		<-sigCh
		cancel()
	}()

	refreshLabel := fmt.Sprintf("refreshing every %s", refresh)

	render := func() error {
		snap, err := captureDashboard(s)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		buf.WriteString("\x1b[2J\x1b[H")
		renderDashboard(&buf, snap, termWidth(), refreshLabel, colors)
		_, err = io.Copy(w, &buf)
		return err
	}

	if err := render(); err != nil {
		return err
	}
	ticker := time.NewTicker(refresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := render(); err != nil {
				return err
			}
		}
	}
}

// ---- rendering ----

// renderDashboard is a pure function from snapshot to bytes. Kept separate
// from capture so tests can feed synthetic snapshots. Pass a disabled ansi
// (or one built against a non-TTY) for plain output.
func renderDashboard(w io.Writer, snap *dashboardSnapshot, width int, footerMode string, a *ansi) {
	if a == nil {
		a = &ansi{}
	}
	renderDashboardHeader(w, snap, width, a)
	fmt.Fprintln(w, a.dim(strings.Repeat("─", width)))
	renderDashboardGoal(w, snap, a)
	fmt.Fprintln(w)
	renderDashboardBudget(w, snap, a)
	fmt.Fprintln(w)
	renderDashboardTreeAndFrontier(w, snap, width, a)
	fmt.Fprintln(w)
	if len(snap.InFlight) > 0 {
		renderDashboardInFlight(w, snap, a)
		fmt.Fprintln(w)
	}
	renderDashboardRecent(w, snap, a)
	fmt.Fprintln(w)
	fmt.Fprintf(w, " %s\n", a.dim(footerMode+" · Ctrl-C to exit"))
}

func renderDashboardHeader(w io.Writer, snap *dashboardSnapshot, width int, a *ansi) {
	leftPlain := "autoresearch — " + snap.Project
	rightPlain := "[active]"
	rightColored := a.green(rightPlain)
	if snap.Paused {
		rightPlain = "[PAUSED"
		if snap.PauseReason != "" {
			rightPlain += ": " + snap.PauseReason
		}
		rightPlain += "]"
		rightColored = a.boldYellow(rightPlain)
	}
	gap := width - runeLen(leftPlain) - runeLen(rightPlain)
	if gap < 1 {
		gap = 1
	}
	fmt.Fprintln(w, a.bold(leftPlain)+strings.Repeat(" ", gap)+rightColored)
}

func renderDashboardGoal(w io.Writer, snap *dashboardSnapshot, a *ansi) {
	if snap.Goal == nil {
		fmt.Fprintln(w, " "+a.bold("Goal:")+" "+a.dim("(no goal set — run `autoresearch goal set`)"))
		return
	}
	obj := snap.Goal.Objective
	line := " " + a.bold("Goal:") + " " + a.cyan(obj.Direction) + " " + a.cyan(obj.Instrument)
	if obj.Target != "" {
		line += " on " + obj.Target
	}
	if obj.TargetEffect > 0 {
		line += fmt.Sprintf(" (target_effect=%g)", obj.TargetEffect)
	}
	fmt.Fprintln(w, line)
	if len(snap.Goal.Constraints) > 0 {
		fmt.Fprintln(w, " "+a.bold("Constraints:"))
		for _, c := range snap.Goal.Constraints {
			switch {
			case c.Max != nil:
				fmt.Fprintf(w, "   %s %s %g\n", c.Instrument, a.dim("≤"), *c.Max)
			case c.Min != nil:
				fmt.Fprintf(w, "   %s %s %g\n", c.Instrument, a.dim("≥"), *c.Min)
			case c.Require != "":
				fmt.Fprintf(w, "   %s require=%s\n", c.Instrument, a.cyan(c.Require))
			}
		}
	}
}

// meterColor picks a traffic-light color based on usage ratio. Below 50%:
// green. 50–80%: yellow. At or above 80%: red. Kept here rather than on ansi
// so the thresholds live next to the callers that know what "full" means.
func meterColor(a *ansi, used, limit float64, s string) string {
	if limit <= 0 {
		return s
	}
	r := used / limit
	switch {
	case r >= 0.8:
		return a.red(s)
	case r >= 0.5:
		return a.yellow(s)
	default:
		return a.green(s)
	}
}

func renderDashboardBudget(w io.Writer, snap *dashboardSnapshot, a *ansi) {
	parts := []string{}
	if snap.Budgets.Limits.MaxExperiments > 0 {
		s := fmt.Sprintf("%d/%d experiments",
			snap.Budgets.Usage.Experiments, snap.Budgets.Limits.MaxExperiments)
		parts = append(parts, meterColor(a,
			float64(snap.Budgets.Usage.Experiments),
			float64(snap.Budgets.Limits.MaxExperiments), s))
	} else {
		parts = append(parts, fmt.Sprintf("%d experiments", snap.Budgets.Usage.Experiments))
	}
	if snap.Budgets.Limits.MaxWallTimeH > 0 {
		s := fmt.Sprintf("%.1fh/%dh elapsed",
			snap.Budgets.Usage.ElapsedH, snap.Budgets.Limits.MaxWallTimeH)
		parts = append(parts, meterColor(a,
			snap.Budgets.Usage.ElapsedH,
			float64(snap.Budgets.Limits.MaxWallTimeH), s))
	}
	if snap.Budgets.Limits.FrontierStallK > 0 {
		s := fmt.Sprintf("stalled %d/%d", snap.StalledFor, snap.Budgets.Limits.FrontierStallK)
		parts = append(parts, meterColor(a,
			float64(snap.StalledFor),
			float64(snap.Budgets.Limits.FrontierStallK), s))
	}
	fmt.Fprintf(w, " %s %s\n", a.bold("Budget:"), strings.Join(parts, a.dim("  ·  ")))
	fmt.Fprintf(w, " %s   %s\n", a.bold("Mode:"), snap.Mode)
	fmt.Fprintf(w, " %s %d hypotheses · %d experiments · %d observations · %d conclusions\n",
		a.bold("Counts:"),
		snap.Counts["hypotheses"], snap.Counts["experiments"], snap.Counts["observations"], snap.Counts["conclusions"])
}

func renderDashboardTreeAndFrontier(w io.Writer, snap *dashboardSnapshot, width int, a *ansi) {
	fmt.Fprintln(w, " "+a.bold("Hypothesis tree"))
	fmt.Fprintln(w, " "+a.dim("─────────────────"))
	if len(snap.Tree) == 0 {
		fmt.Fprintln(w, "   "+a.dim("(no hypotheses)"))
	} else {
		roots, children := treeJSONToHypothesisForest(snap.Tree)
		var tbuf bytes.Buffer
		renderForestToWriter(&tbuf, roots, children, 72, a)
		for _, line := range strings.Split(strings.TrimRight(tbuf.String(), "\n"), "\n") {
			fmt.Fprintln(w, " "+line)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, " "+a.bold("Frontier"))
	fmt.Fprintln(w, " "+a.dim("────────"))
	if snap.Goal == nil {
		fmt.Fprintln(w, "   "+a.dim("(no goal set)"))
		return
	}
	if len(snap.Frontier) == 0 {
		fmt.Fprintln(w, "   "+a.dim("(no feasible supported conclusions yet)"))
	} else {
		for i, r := range snap.Frontier {
			marker := "  "
			if i == 0 {
				marker = " " + a.boldYellow("*")
			}
			fmt.Fprintf(w, " %s %s  %s  %s=%.6g\n",
				marker, a.cyan(r.Conclusion), a.cyan(r.Hypothesis), snap.Goal.Objective.Instrument, r.Value)
		}
	}
	if snap.Budgets.Limits.FrontierStallK > 0 {
		fmt.Fprintf(w, "   %s\n",
			a.dim(fmt.Sprintf("(stalled_for: %d of %d)", snap.StalledFor, snap.Budgets.Limits.FrontierStallK)))
	} else {
		fmt.Fprintf(w, "   %s\n", a.dim(fmt.Sprintf("(stalled_for: %d)", snap.StalledFor)))
	}
}

// treeJSONToHypothesisForest rehydrates a captured treeNode slice back into
// the shape expected by renderForestToWriter so the dashboard reuses the
// `tree` verb's exact renderer.
func treeJSONToHypothesisForest(nodes []*treeNode) ([]*entity.Hypothesis, map[string][]*entity.Hypothesis) {
	children := map[string][]*entity.Hypothesis{}
	var roots []*entity.Hypothesis
	var walk func(parentID string, ns []*treeNode)
	walk = func(parentID string, ns []*treeNode) {
		for _, n := range ns {
			h := &entity.Hypothesis{
				ID:     n.ID,
				Parent: parentID,
				Claim:  n.Claim,
				Status: n.Status,
				Author: n.Author,
			}
			if parentID == "" {
				roots = append(roots, h)
			} else {
				children[parentID] = append(children[parentID], h)
			}
			walk(n.ID, n.Children)
		}
	}
	walk("", nodes)
	return roots, children
}

func renderDashboardInFlight(w io.Writer, snap *dashboardSnapshot, a *ansi) {
	fmt.Fprintln(w, " "+a.bold("In flight"))
	fmt.Fprintln(w, " "+a.dim("─────────"))
	for _, r := range snap.InFlight {
		elapsed := "?"
		if r.ImplementedAt != nil {
			elapsed = formatElapsed(time.Duration(r.ElapsedS) * time.Second)
		}
		// Pad BEFORE coloring so ANSI escapes don't inflate column widths.
		statusCell := fmt.Sprintf("%-12s", r.Status)
		switch r.Status {
		case entity.ExpImplemented:
			statusCell = a.cyan(statusCell)
		case entity.ExpMeasured:
			statusCell = a.yellow(statusCell)
		}
		tierCell := fmt.Sprintf("%-8s", r.Tier)
		fmt.Fprintf(w, "   %-8s  %s  %s %s elapsed  instruments=%s\n",
			r.ID, statusCell, a.dim("tier="+tierCell), elapsed, strings.Join(r.Instruments, ","))
	}
}

// eventKindColor colors an event kind token by its category prefix, so the
// recent-events stream is scannable at a glance: cyan hypothesis moves,
// yellow experiment moves, blue observations, green conclusions, magenta
// pause/resume. Anything unrecognized is returned uncolored.
func eventKindColor(a *ansi, kindPadded, kindRaw string) string {
	switch {
	case strings.HasPrefix(kindRaw, "hypothesis."):
		return a.cyan(kindPadded)
	case strings.HasPrefix(kindRaw, "experiment."):
		return a.yellow(kindPadded)
	case strings.HasPrefix(kindRaw, "observation."):
		return a.blue(kindPadded)
	case strings.HasPrefix(kindRaw, "conclusion."):
		return a.green(kindPadded)
	case kindRaw == "pause" || kindRaw == "resume":
		return a.magenta(kindPadded)
	case kindRaw == "init":
		return a.bold(kindPadded)
	default:
		return kindPadded
	}
}

func renderDashboardRecent(w io.Writer, snap *dashboardSnapshot, a *ansi) {
	fmt.Fprintf(w, " %s\n", a.bold(fmt.Sprintf("Recent events (last %d)", len(snap.RecentEvents))))
	fmt.Fprintln(w, " "+a.dim("──────────────────────"))
	if len(snap.RecentEvents) == 0 {
		fmt.Fprintln(w, "   "+a.dim("(no events yet)"))
		return
	}
	for _, e := range snap.RecentEvents {
		subject := e.Subject
		if subject == "" {
			subject = "-"
		}
		kindCell := fmt.Sprintf("%-24s", e.Kind)
		kindCell = eventKindColor(a, kindCell, e.Kind)
		fmt.Fprintf(w, "   %s  %s  %-16s  %s\n",
			a.dim(e.Ts.UTC().Format("15:04:05")),
			kindCell,
			subject,
			a.dim(e.Actor),
		)
	}
}

// ---- small helpers ----

// termWidth returns the terminal's column count, falling back through:
// 1. actual TTY width via x/term
// 2. $COLUMNS env var
// 3. 100
func termWidth() int {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			if w > 200 {
				return 200
			}
			return w
		}
	}
	if s := os.Getenv("COLUMNS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 100
}

func runeLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	m := total / 60
	sec := total - m*60
	return fmt.Sprintf("%02d:%02d", m, sec)
}
