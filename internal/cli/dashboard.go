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
	"unicode/utf8"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func dashboardCommands() []*cobra.Command {
	root := dashboardCmd()
	root.AddCommand(dashboardTuiCmd())
	return []*cobra.Command{root}
}

func dashboardCmd() *cobra.Command {
	var (
		refresh   int
		colorMode string
		goalFlag  string
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
steer by talking to the main agent session, which translates intent
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
			scope, err := resolveGoalScope(s, goalFlag)
			if err != nil {
				return err
			}
			if globalJSON {
				snap, err := captureDashboardScoped(s, scope)
				if err != nil {
					return err
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(snap)
			}
			if refresh == 0 {
				return runDashboardOnce(s, scope, os.Stdout, colors)
			}
			if refresh < 1 {
				return errors.New("--refresh must be at least 1 second")
			}
			if !term.IsTerminal(int(os.Stdout.Fd())) {
				return errors.New("dashboard --refresh requires a TTY; for scripting use one-shot `dashboard --json`")
			}
			return runDashboardLoop(s, scope, os.Stdout, time.Duration(refresh)*time.Second, colors)
		},
	}
	c.Flags().IntVar(&refresh, "refresh", 0, "seconds between auto-refreshes (0 = one-shot, requires a TTY when > 0)")
	c.Flags().StringVar(&colorMode, "color", "auto", "color output: auto (TTY-detect), always (force on, for `watch -c`), never")
	c.Flags().StringVar(&goalFlag, "goal", "", "goal to scope the dashboard to (defaults to active goal; use 'all' for every goal)")
	return c
}

// ---- snapshot capture ----

type dashboardSnapshot struct {
	Project                string                `json:"project"`
	ScopeGoalID            string                `json:"scope_goal_id,omitempty"`
	ScopeAll               bool                  `json:"scope_all"`
	Paused                 bool                  `json:"paused"`
	PauseReason            string                `json:"pause_reason,omitempty"`
	Mode                   string                `json:"mode"`
	MainCheckoutDirty      bool                  `json:"main_checkout_dirty"`
	MainCheckoutDirtyPaths []string              `json:"main_checkout_dirty_paths"`
	Goal                   *entity.Goal          `json:"goal,omitempty"`
	Budgets                dashboardBudgets      `json:"budgets"`
	Counts                 map[string]int        `json:"counts"`
	Tree                   []*treeNode           `json:"tree"`
	Frontier               []frontierRow         `json:"frontier"`
	StalledFor             int                   `json:"stalled_for"`
	InFlight               []dashboardInFlight   `json:"in_flight"`
	StaleExperiments       []staleExperimentView `json:"stale_experiments,omitempty"`
	RecentLessons          []*entity.Lesson      `json:"recent_lessons,omitempty"`
	recentLessonAccuracy   map[string]lessonAccuracySummary
	RecentEvents           []store.Event `json:"recent_events"`
	CapturedAt             time.Time     `json:"captured_at"`
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
	Instruments   []string   `json:"instruments"`
	ImplementedAt *time.Time `json:"implemented_at,omitempty"`
	ElapsedS      float64    `json:"elapsed_s"`
}

const dashboardRecentEventsSummaryLimit = 10

// readDashboardRecentEvents returns a descending slice of events, starting
// from the newest event and paging backward by `offsetNewest`. Operates on
// a pre-loaded slice to avoid redundant reads.
func readDashboardRecentEvents(all []store.Event, offsetNewest, limit int) ([]store.Event, bool) {
	if limit <= 0 || len(all) == 0 || offsetNewest >= len(all) {
		return []store.Event{}, true
	}
	if offsetNewest < 0 {
		offsetNewest = 0
	}
	end := len(all) - offsetNewest
	if end < 0 {
		end = 0
	}
	start := max(end-limit, 0)
	out := make([]store.Event, 0, end-start)
	for i := end - 1; i >= start; i-- {
		out = append(out, all[i])
	}
	return out, start == 0
}

// captureDashboard gathers everything the dashboard renders. All reads go
// through existing store methods — no new store APIs, no mutation.
func captureDashboard(s *store.Store) (*dashboardSnapshot, error) {
	scope, err := resolveGoalScope(s, "")
	if err != nil {
		return nil, err
	}
	return captureDashboardScoped(s, scope)
}

func captureDashboardScoped(s *store.Store, scope goalScope) (*dashboardSnapshot, error) {
	snap := &dashboardSnapshot{
		Project:                s.Root(),
		ScopeGoalID:            scope.GoalID,
		ScopeAll:               scope.All,
		CapturedAt:             time.Now().UTC(),
		MainCheckoutDirtyPaths: []string{},
		Tree:                   []*treeNode{},
		Frontier:               []frontierRow{},
		InFlight:               []dashboardInFlight{},
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

	mainCheckout, err := captureMainCheckoutState(s.Root())
	if err != nil {
		return nil, err
	}
	snap.MainCheckoutDirty = mainCheckout.Dirty
	snap.MainCheckoutDirtyPaths = mainCheckout.Paths

	if !scope.All && scope.GoalID != "" {
		if g, err := s.ReadGoal(scope.GoalID); err == nil {
			snap.Goal = g
		}
	}

	snap.Budgets.Limits.MaxExperiments = cfg.Budgets.MaxExperiments
	snap.Budgets.Limits.MaxWallTimeH = cfg.Budgets.MaxWallTimeH
	snap.Budgets.Limits.FrontierStallK = cfg.Budgets.FrontierStallK
	snap.Budgets.Usage.Experiments = st.Counters["E"]
	if st.ResearchStartedAt != nil {
		snap.Budgets.Usage.ElapsedH = time.Since(*st.ResearchStartedAt).Hours()
	}

	resolver := newGoalScopeResolver(s, scope)

	hyps, err := s.ListHypotheses()
	if err != nil {
		return nil, err
	}
	lessonLinks := buildLessonLinkIndex(hyps)
	hyps = resolver.filterHypotheses(hyps)
	roots, children := buildHypothesisForest(hyps)
	snap.Tree = buildTreeJSON(roots, children)

	concls, err := s.ListConclusions()
	if err != nil {
		return nil, err
	}
	concls, err = resolver.filterConclusions(concls)
	if err != nil {
		return nil, err
	}

	if snap.Goal != nil {
		rows, stalled := readmodel.ComputeFrontier(s, snap.Goal, concls)
		snap.Frontier = rows
		snap.StalledFor = stalled
	}

	// Load events once — used for both in-flight timestamps and the
	// recent-events panel (avoids N+1 full reads of events.jsonl).
	allEvents, err := s.Events(0)
	if err != nil {
		return nil, err
	}
	allEvents, err = resolver.filterEvents(allEvents)
	if err != nil {
		return nil, err
	}

	exps, err := s.ListExperiments()
	if err != nil {
		return nil, err
	}
	exps, err = resolver.filterExperiments(exps)
	if err != nil {
		return nil, err
	}
	expClassByID, err := readmodel.ClassifyExperimentsForRead(s, exps)
	if err != nil {
		return nil, err
	}
	for _, e := range exps {
		if e.Status != entity.ExpImplemented && e.Status != entity.ExpMeasured {
			continue
		}
		if len(e.ReferencedAsBaselineBy) > 0 {
			continue
		}
		if !readmodel.ExperimentReadClassForID(expClassByID, e.ID).LoopActionable() {
			continue
		}
		row := dashboardInFlight{
			ID:          e.ID,
			Hypothesis:  e.Hypothesis,
			Status:      e.Status,
			Instruments: append([]string{}, e.Instruments...),
		}
		if ts, kind := readmodel.FindLastEventForExperiment(allEvents, e.ID); ts != nil && kind == "experiment.implement" {
			row.ImplementedAt = ts
			row.ElapsedS = time.Since(*ts).Seconds()
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

	// Stale experiment detection: flag loop-actionable, non-baseline
	// experiments whose last event is older than the configured threshold.
	if staleMinutes := cfg.Budgets.StaleExperimentMinutes; staleMinutes > 0 {
		threshold := time.Duration(staleMinutes) * time.Minute
		snap.StaleExperiments = readmodel.FindStaleExperimentsForRead(exps, expClassByID, allEvents, threshold, time.Now().UTC())
	}

	allObs, err := s.ListObservations()
	if err != nil {
		return nil, err
	}
	allObs, err = resolver.filterObservations(allObs)
	if err != nil {
		return nil, err
	}

	var lessonCount int
	if lessons, err := s.ListLessons(); err == nil {
		lessons, err = resolver.filterLessons(lessons)
		if err != nil {
			return nil, err
		}
		_, accuracy, err := computeLessonAccuracy(s, lessons, concls, lessonLinks)
		if err != nil {
			return nil, err
		}
		snap.recentLessonAccuracy = accuracy
		lessonCount = len(lessons)
		// Split by effective lifecycle so steering lessons surface first.
		buckets := map[string][]*entity.Lesson{
			entity.LessonStatusActive:      {},
			entity.LessonStatusProvisional: {},
			entity.LessonStatusInvalidated: {},
			entity.LessonStatusSuperseded:  {},
		}
		for _, l := range lessons {
			view, err := annotateLessonForRead(s, l)
			if err != nil {
				return nil, err
			}
			buckets[view.Status] = append(buckets[view.Status], view)
		}
		for _, key := range []string{
			entity.LessonStatusActive,
			entity.LessonStatusProvisional,
			entity.LessonStatusInvalidated,
			entity.LessonStatusSuperseded,
		} {
			sort.SliceStable(buckets[key], func(i, j int) bool {
				return buckets[key][i].CreatedAt.After(buckets[key][j].CreatedAt)
			})
			snap.RecentLessons = append(snap.RecentLessons, buckets[key]...)
		}
	}

	// Derive counts from already-loaded data instead of calling Counts()
	// which re-scans every entity directory. Only observations need a
	// targeted ReadDir because they're loaded inside computeFrontier, not
	// directly available here.
	snap.Counts = map[string]int{
		"hypotheses":   len(hyps),
		"experiments":  len(exps),
		"observations": len(allObs),
		"conclusions":  len(concls),
		"lessons":      lessonCount,
	}

	snap.RecentEvents, _ = readDashboardRecentEvents(allEvents, 0, dashboardRecentEventsSummaryLimit)

	return snap, nil
}

// ---- one-shot + refresh loop ----

func runDashboardOnce(s *store.Store, scope goalScope, w io.Writer, colors *ansi) error {
	snap, err := captureDashboardScoped(s, scope)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	renderDashboard(&buf, snap, termWidth(), "snapshot", colors)
	_, err = io.Copy(w, &buf)
	return err
}

func runDashboardLoop(s *store.Store, scope goalScope, w io.Writer, refresh time.Duration, colors *ansi) error {
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
		snap, err := captureDashboardScoped(s, scope)
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

// sectionHeader writes a bold title with a dim underline.
func sectionHeader(w io.Writer, title string, a *ansi) {
	fmt.Fprintln(w, " "+a.bold(title))
	fmt.Fprintln(w, " "+a.dim(strings.Repeat("─", utf8.RuneCountInString(title))))
}

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
	if snap.MainCheckoutDirty {
		renderDashboardMainCheckout(w, snap, a)
		fmt.Fprintln(w)
	}
	renderDashboardTree(w, snap, a)
	fmt.Fprintln(w)
	renderDashboardFrontier(w, snap, a)
	fmt.Fprintln(w)
	if len(snap.InFlight) > 0 {
		renderDashboardInFlight(w, snap, a)
		fmt.Fprintln(w)
	}
	if len(snap.StaleExperiments) > 0 {
		renderDashboardStale(w, snap, a)
		fmt.Fprintln(w)
	}
	if len(snap.RecentLessons) > 0 {
		renderDashboardLessons(w, snap, a)
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
	gap := width - utf8.RuneCountInString(leftPlain) - utf8.RuneCountInString(rightPlain)
	if gap < 1 {
		gap = 1
	}
	fmt.Fprintln(w, a.bold(leftPlain)+strings.Repeat(" ", gap)+rightColored)
}

func renderDashboardGoal(w io.Writer, snap *dashboardSnapshot, a *ansi) {
	if snap.ScopeAll {
		fmt.Fprintln(w, " "+a.bold("Scope:")+" "+a.cyan("all goals"))
		fmt.Fprintln(w, " "+a.bold("Goal:")+" "+a.dim("(goal-specific panels are disabled in all-goal scope)"))
		return
	}
	if snap.Goal == nil {
		fmt.Fprintln(w, " "+a.bold("Goal:")+" "+a.dim("(no goal set — run `autoresearch goal set`)"))
		return
	}
	fmt.Fprintln(w, " "+a.bold("Scope:")+" "+a.cyan(snap.Goal.ID))
	fmt.Fprintln(w, " "+a.bold("Goal:")+" "+a.cyan(formatGoalObjective(snap.Goal)))
	fmt.Fprintln(w, " "+a.bold("Completion:")+" "+formatGoalCompletion(snap.Goal))
	if len(snap.Goal.Constraints) > 0 {
		fmt.Fprintln(w, " "+a.bold("Constraints:"))
		for _, c := range snap.Goal.Constraints {
			fmt.Fprintf(w, "   %s\n", entity.FormatConstraint(c))
		}
	}
}

// meterColor picks a traffic-light color based on usage ratio. Below 50%:
// green. 50–80%: yellow. At or above 80%: red.
func meterColor(a *ansi, used, limit float64, s string) string {
	if limit <= 0 {
		return s
	}
	switch r := used / limit; {
	case r >= 0.8:
		return a.red(s)
	case r >= 0.5:
		return a.yellow(s)
	default:
		return a.green(s)
	}
}

func renderDashboardBudget(w io.Writer, snap *dashboardSnapshot, a *ansi) {
	var parts []string
	if lim := snap.Budgets.Limits.MaxExperiments; lim > 0 {
		s := fmt.Sprintf("%d/%d experiments", snap.Budgets.Usage.Experiments, lim)
		parts = append(parts, meterColor(a, float64(snap.Budgets.Usage.Experiments), float64(lim), s))
	} else {
		parts = append(parts, fmt.Sprintf("%d experiments", snap.Budgets.Usage.Experiments))
	}
	if lim := snap.Budgets.Limits.MaxWallTimeH; lim > 0 {
		s := fmt.Sprintf("%.1fh/%dh elapsed", snap.Budgets.Usage.ElapsedH, lim)
		parts = append(parts, meterColor(a, snap.Budgets.Usage.ElapsedH, float64(lim), s))
	}
	if lim := snap.Budgets.Limits.FrontierStallK; lim > 0 && !snap.ScopeAll {
		s := fmt.Sprintf("stalled %d/%d", snap.StalledFor, lim)
		parts = append(parts, meterColor(a, float64(snap.StalledFor), float64(lim), s))
	}
	fmt.Fprintf(w, " %s %s\n", a.bold("Budget:"), strings.Join(parts, a.dim("  ·  ")))
	fmt.Fprintf(w, " %s   %s\n", a.bold("Mode:"), snap.Mode)
	fmt.Fprintf(w, " %s %d hypotheses · %d experiments · %d observations · %d conclusions\n",
		a.bold("Counts:"),
		snap.Counts["hypotheses"], snap.Counts["experiments"], snap.Counts["observations"], snap.Counts["conclusions"])
}

func renderDashboardMainCheckout(w io.Writer, snap *dashboardSnapshot, a *ansi) {
	sectionHeader(w, "Main checkout", a)
	fmt.Fprintln(w, " "+a.boldYellow("WARNING: dirty outside autoresearch-managed files"))
	fmt.Fprintln(w, " research should keep experiment edits in worktrees; treat these as explicit maintenance:")
	for _, path := range snap.MainCheckoutDirtyPaths {
		fmt.Fprintf(w, "   %s\n", path)
	}
}

func renderDashboardTree(w io.Writer, snap *dashboardSnapshot, a *ansi) {
	sectionHeader(w, "Hypothesis tree", a)
	if len(snap.Tree) == 0 {
		fmt.Fprintln(w, "   "+a.dim("(no hypotheses)"))
		return
	}
	roots, children := treeJSONToHypothesisForest(snap.Tree)
	var tbuf bytes.Buffer
	renderForestToWriter(&tbuf, roots, children, 72, a)
	for _, line := range strings.Split(strings.TrimRight(tbuf.String(), "\n"), "\n") {
		fmt.Fprintln(w, " "+line)
	}
}

func renderDashboardFrontier(w io.Writer, snap *dashboardSnapshot, a *ansi) {
	sectionHeader(w, "Frontier", a)
	if snap.ScopeAll {
		fmt.Fprintln(w, "   "+a.dim("(goal-specific — rerun with --goal G-NNNN)"))
		return
	}
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
			dead := ""
			if r.Classification == experimentClassificationDead {
				dead = "  " + a.dim(experimentClassificationMarker(r.Classification))
			}
			fmt.Fprintf(w, " %s %s  %s  %s=%.6g%s\n",
				marker, a.cyan(r.Conclusion), a.cyan(r.Hypothesis), snap.Goal.Objective.Instrument, r.Value, dead)
		}
	}
	if lim := snap.Budgets.Limits.FrontierStallK; lim > 0 {
		fmt.Fprintf(w, "   %s\n", a.dim(fmt.Sprintf("(stalled_for: %d of %d)", snap.StalledFor, lim)))
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
	sectionHeader(w, "In flight", a)
	for _, r := range snap.InFlight {
		elapsed := "?"
		if r.ImplementedAt != nil {
			elapsed = formatElapsed(time.Duration(r.ElapsedS) * time.Second)
		}
		statusCell := fmt.Sprintf("%-12s", r.Status)
		switch r.Status {
		case entity.ExpImplemented:
			statusCell = a.cyan(statusCell)
		case entity.ExpMeasured:
			statusCell = a.yellow(statusCell)
		}
		fmt.Fprintf(w, "   %-8s  %s  %s elapsed  instruments=%s\n",
			r.ID, statusCell, elapsed, strings.Join(r.Instruments, ","))
	}
}

func renderDashboardStale(w io.Writer, snap *dashboardSnapshot, a *ansi) {
	sectionHeader(w, a.yellow("⚠ Stale experiments"), a)
	for _, r := range snap.StaleExperiments {
		age := formatStaleAge(r.StaleMinutes)
		fmt.Fprintf(w, "   %-8s  %-12s  hyp=%-8s  %s ago  (last: %s)\n",
			r.ID, r.Status, r.Hypothesis, age, r.LastEventKind)
	}
}

func formatStaleAge(minutes float64) string {
	if minutes >= 60 {
		return fmt.Sprintf("%.1fh", minutes/60)
	}
	return fmt.Sprintf("%.0fm", minutes)
}

func renderDashboardLessons(w io.Writer, snap *dashboardSnapshot, a *ansi) {
	sectionHeader(w, fmt.Sprintf("Recent lessons (last %d)", len(snap.RecentLessons)), a)
	for _, l := range snap.RecentLessons {
		scopeCell := fmt.Sprintf("%-10s", l.Scope)
		switch l.Scope {
		case entity.LessonScopeSystem:
			scopeCell = a.magenta(scopeCell)
		case entity.LessonScopeHypothesis:
			scopeCell = a.cyan(scopeCell)
		}
		subj := ""
		if len(l.Subjects) > 0 {
			subj = " from=" + strings.Join(l.Subjects, ",")
		}
		status := ""
		switch l.Status {
		case entity.LessonStatusProvisional:
			status = " [" + l.Status + "]"
		case entity.LessonStatusInvalidated:
			status = " [" + l.Status + "]"
		case entity.LessonStatusSuperseded:
			status = " [" + l.Status + "]"
		}
		source := ""
		if l.Provenance != nil && l.Provenance.SourceChain != "" && l.Provenance.SourceChain != entity.LessonSourceSystem {
			source = " {" + l.Provenance.SourceChain + "}"
		}
		pred := ""
		if l.PredictedEffect != nil {
			pred = " → predicts " + formatPredictedEffect(l.PredictedEffect)
		}
		fmt.Fprintf(w, "   %-8s  %s  %s%s%s%s%s\n",
			a.cyan(l.ID), scopeCell, truncate(l.Claim, 60), a.dim(status), a.dim(source), a.dim(subj), a.yellow(pred))
	}
}

// eventKindColor colors an event kind token by its category prefix.
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
	sectionHeader(w, fmt.Sprintf("Recent events (last %d)", len(snap.RecentEvents)), a)
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
		fmt.Fprintf(w, "   %s  %s  %-8s  %s\n",
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
			return min(w, 200)
		}
	}
	if s := os.Getenv("COLUMNS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 100
}

func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}
