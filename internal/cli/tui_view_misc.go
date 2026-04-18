package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// ---- instrument list view ----

type instrumentListView struct {
	names []string
	by    map[string]store.Instrument
	err   error
}

type instrumentListLoadedMsg struct {
	by  map[string]store.Instrument
	err error
}

func newInstrumentListView() *instrumentListView { return &instrumentListView{} }

func (v *instrumentListView) title() string { return "Instruments" }

func (v *instrumentListView) init(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		insts, err := s.ListInstruments()
		return instrumentListLoadedMsg{by: insts, err: err}
	}
}

func (v *instrumentListView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case instrumentListLoadedMsg:
		v.by = msg.by
		v.err = msg.err
		v.names = v.names[:0]
		for n := range msg.by {
			v.names = append(v.names, n)
		}
		sort.Strings(v.names)
		return v, nil
	case storeChangedMsg:
		return v, v.init(s)
	}
	return v, nil
}

func (v *instrumentListView) hints() []tuiHint { return nil }

func (v *instrumentListView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if len(v.names) == 0 {
		return tuiDim.Render("(no instruments registered)")
	}
	lines := []string{}
	lines = append(lines, tuiBold.Render(fmt.Sprintf("%d instruments", len(v.names))))
	lines = append(lines, "")
	for _, n := range v.names {
		inst := v.by[n]
		unit := emptyDash(inst.Unit)
		line := fmt.Sprintf("  %-16s  parser=%-18s  unit=%-12s",
			tuiCyan.Render(n), inst.Parser, unit)
		if inst.Pattern != "" {
			line += "  " + tuiDim.Render("pattern=/"+inst.Pattern+"/")
		}
		if inst.MinSamples > 0 {
			line += fmt.Sprintf("  %s%d", tuiDim.Render("min_samples="), inst.MinSamples)
		}
		if len(inst.Cmd) > 0 {
			line += "\n      " + tuiDim.Render("cmd="+strings.Join(inst.Cmd, " "))
		}
		lines = append(lines, line)
	}
	return clampLines(strings.Join(lines, "\n"), height, width)
}

// ---- tree view (full hypothesis forest, scrollable, Enter -> hypothesis detail) ----

type treeView struct {
	scope  goalScope
	flat   []*treeNode
	cursor int
	err    error
}

type treeLoadedMsg struct {
	roots []*treeNode
	err   error
}

func newTreeView(scope goalScope) *treeView { return &treeView{scope: scope} }

func (v *treeView) title() string { return "Tree" }

func (v *treeView) init(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		all, err := s.ListHypotheses()
		if err != nil {
			return treeLoadedMsg{err: err}
		}
		roots, children := buildHypothesisForest(filterHypothesesByScope(all, v.scope))
		return treeLoadedMsg{roots: buildTreeJSON(roots, children)}
	}
}

func (v *treeView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case treeLoadedMsg:
		v.err = msg.err
		v.flat = flattenTree(msg.roots)
		if v.cursor >= len(v.flat) {
			v.cursor = len(v.flat) - 1
		}
		if v.cursor < 0 {
			v.cursor = 0
		}
		return v, nil
	case storeChangedMsg:
		return v, v.init(s)
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if v.cursor > 0 {
				v.cursor--
			}
		case "down", "j":
			if v.cursor < len(v.flat)-1 {
				v.cursor++
			}
		case "enter":
			if v.cursor >= 0 && v.cursor < len(v.flat) {
				return v, tuiPush(newHypothesisDetailView(v.flat[v.cursor].ID))
			}
		}
	}
	return v, nil
}

func (v *treeView) hints() []tuiHint {
	return []tuiHint{{"↑↓", "move"}, {"Enter", "open"}}
}

func (v *treeView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if len(v.flat) == 0 {
		return tuiDim.Render("(no hypotheses)")
	}
	// Rebuild a nested tree from the flat slice to render with prefixes.
	// We already computed line ordering; re-use renderTreeLines with a
	// synthesized nested structure built from flat.Parent-ish order — but
	// flat loses structure. Simplest: re-read snapshot during render.
	// Instead, recompute roots from the currently loaded set via IDs in flat.
	idToNode := map[string]*treeNode{}
	for _, n := range v.flat {
		idToNode[n.ID] = n
	}
	// flat is in DFS order, so renderTreeLines needs the roots slice of the
	// nested tree; but flat came from flattenTree which walked roots
	// in order. Rebuild by scanning flat for items whose parent-id is not in
	// the set. treeNode doesn't carry parent — so we take a different path:
	// render directly from the flat slice using the walk order.
	lines := make([]string, 0, len(v.flat))
	for _, n := range v.flat {
		glyph := tuiStatusGlyph(n.Status)
		claim := truncate(n.Claim, width-20)
		lines = append(lines, fmt.Sprintf("  %s %-8s  %s", glyph, n.ID, claim))
	}
	if v.cursor >= 0 && v.cursor < len(lines) {
		lines[v.cursor] = tuiSelected.Render(padRight(stripANSI(lines[v.cursor]), width-2))
	}
	inner := max(height-1, 1)
	lines = scrollWindow(lines, v.cursor, inner)
	return strings.Join(lines, "\n")
}

// ---- frontier view ----

type frontierView struct {
	scope      goalScope
	goal       *entity.Goal
	rows       []frontierRow
	assessment frontierGoalAssessment
	stalled    int
	cursor     int
	err        error
}

type frontierLoadedMsg struct {
	goal       *entity.Goal
	rows       []frontierRow
	assessment frontierGoalAssessment
	stalled    int
	err        error
}

func newFrontierView(scope goalScope) *frontierView { return &frontierView{scope: scope} }

func (v *frontierView) title() string { return "Frontier" }

func (v *frontierView) init(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		if v.scope.All {
			return frontierLoadedMsg{}
		}
		goal, err := s.ReadGoal(v.scope.GoalID)
		if err != nil {
			return frontierLoadedMsg{err: err}
		}
		concls, err := s.ListConclusions()
		if err != nil {
			return frontierLoadedMsg{err: err}
		}
		concls, err = newGoalScopeResolver(s, v.scope).filterConclusions(concls)
		if err != nil {
			return frontierLoadedMsg{err: err}
		}
		obsByExp := loadObservationsByExperiment(s)
		expClassByID := loadExperimentReadClasses(s)
		rows, stalled := computeFrontierFromObservations(goal, concls, obsByExp, expClassByID)
		return frontierLoadedMsg{
			goal:       goal,
			rows:       rows,
			assessment: assessGoalCompletion(goal, concls, obsByExp, expClassByID),
			stalled:    stalled,
		}
	}
}

func (v *frontierView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case frontierLoadedMsg:
		v.goal = msg.goal
		v.rows = msg.rows
		v.assessment = msg.assessment
		v.stalled = msg.stalled
		v.err = msg.err
		return v, nil
	case storeChangedMsg:
		return v, v.init(s)
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if v.cursor > 0 {
				v.cursor--
			}
		case "down", "j":
			if v.cursor < len(v.rows)-1 {
				v.cursor++
			}
		case "enter":
			if v.cursor >= 0 && v.cursor < len(v.rows) {
				return v, tuiPush(newConclusionDetailView(v.rows[v.cursor].Conclusion))
			}
		}
	}
	return v, nil
}

func (v *frontierView) hints() []tuiHint {
	return []tuiHint{{"↑↓", "move"}, {"Enter", "open"}}
}

func (v *frontierView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if v.scope.All {
		return tuiDim.Render("(frontier is goal-specific; start the TUI with --goal G-NNNN to inspect one)")
	}
	if v.goal == nil {
		return tuiDim.Render("(no goal set)")
	}
	header := tuiBold.Render(fmt.Sprintf("%s %s  ·  %d rows  ·  stalled_for=%d",
		v.goal.Objective.Direction, v.goal.Objective.Instrument, len(v.rows), v.stalled))
	assessment := ""
	switch v.assessment.Mode {
	case "threshold":
		assessment = fmt.Sprintf("threshold=%g -> %s", v.assessment.Threshold, v.assessment.OnThreshold)
		if v.assessment.Met {
			assessment += " (met)"
		}
	default:
		assessment = "open-ended -> continue_until_stall"
	}
	header += "  ·  " + assessment
	if len(v.rows) == 0 {
		return header + "\n\n" + tuiDim.Render("(no feasible supported conclusions yet)")
	}
	rows := make([]string, len(v.rows))
	for i, r := range v.rows {
		marker := "  "
		if i == 0 {
			marker = tuiBoldYellow.Render("* ")
		}
		rows[i] = fmt.Sprintf("%s%s  %s  %s=%.6g  Δfrac=%+.4f",
			marker,
			tuiCyan.Render(r.Conclusion),
			tuiCyan.Render(r.Hypothesis),
			v.goal.Objective.Instrument,
			r.Value,
			r.DeltaFrac)
		if r.Classification == experimentClassificationDead {
			rows[i] += "  " + tuiDim.Render(experimentClassificationMarker(r.Classification))
		}
	}
	return renderFilteredListBody(header, rows, v.cursor, width, height)
}

// ---- goal views ----

type goalListView struct {
	scope     goalScope
	all       []*entity.Goal
	current   string
	focusGoal string
	cursor    int
	err       error
}

type goalListLoadedMsg struct {
	all     []*entity.Goal
	current string
	err     error
}

func newGoalListView(scopes ...goalScope) *goalListView {
	scope := goalScope{}
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	return &goalListView{scope: scope, focusGoal: scope.GoalID}
}

func (v *goalListView) title() string { return "Goals" }

func (v *goalListView) init(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		all, err := s.ListGoals()
		if err != nil {
			return goalListLoadedMsg{err: err}
		}
		st, err := s.State()
		if err != nil {
			return goalListLoadedMsg{err: err}
		}
		return goalListLoadedMsg{all: all, current: st.CurrentGoalID}
	}
}

func (v *goalListView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case goalListLoadedMsg:
		v.all = msg.all
		v.current = msg.current
		v.err = msg.err
		if v.focusGoal != "" {
			for i, g := range v.all {
				if g != nil && g.ID == v.focusGoal {
					v.cursor = i
					break
				}
			}
			v.focusGoal = ""
		}
		v.cursor = clampCursor(v.cursor, len(v.all))
		return v, nil
	case storeChangedMsg:
		return v, v.init(s)
	case tea.KeyMsg:
		if handleListNav(msg, &v.cursor, len(v.all)) {
			return v, nil
		}
		switch msg.String() {
		case "enter":
			if v.cursor >= 0 && v.cursor < len(v.all) {
				return v, tuiPush(newGoalDetailView(v.all[v.cursor].ID, v.scope))
			}
		case "s":
			if v.cursor >= 0 && v.cursor < len(v.all) {
				return v, tuiSetScope(goalScope{GoalID: v.all[v.cursor].ID})
			}
		case "a":
			return v, tuiSetScope(goalScope{All: true})
		}
	}
	return v, nil
}

func (v *goalListView) hints() []tuiHint {
	return []tuiHint{{"↑↓", "move"}, {"Enter", "open"}, {"s", "scope"}, {"a", "all"}}
}

func (v *goalListView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if len(v.all) == 0 {
		return tuiDim.Render("(no goals)")
	}
	header := tuiBold.Render(fmt.Sprintf("%d goals  ·  scope=%s  ·  > scoped  * active", len(v.all), v.scope.label()))
	rows := make([]string, len(v.all))
	for i, g := range v.all {
		scopeMarker := " "
		activeMarker := " "
		if !v.scope.All && g.ID == v.scope.GoalID {
			scopeMarker = ">"
		}
		if g.ID == v.current {
			activeMarker = "*"
		}
		rows[i] = fmt.Sprintf("%s%s %-8s %-10s %s", scopeMarker, activeMarker, g.ID, g.Status, formatGoalObjective(g))
	}
	return renderFilteredListBody(header, rows, v.cursor, width, height)
}

type goalDetailView struct {
	id    string
	scope goalScope
	g     *entity.Goal
	err   error
}

type goalDetailLoadedMsg struct {
	g   *entity.Goal
	err error
}

func newGoalDetailView(id string, scopes ...goalScope) *goalDetailView {
	scope := goalScope{}
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	return &goalDetailView{id: id, scope: scope}
}

func (v *goalDetailView) title() string { return "Goal " + v.id }

func (v *goalDetailView) init(s *store.Store) tea.Cmd {
	id := v.id
	return func() tea.Msg {
		g, err := s.ReadGoal(id)
		return goalDetailLoadedMsg{g: g, err: err}
	}
}

func (v *goalDetailView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case goalDetailLoadedMsg:
		v.g = msg.g
		v.err = msg.err
		return v, nil
	case storeChangedMsg:
		return v, v.init(s)
	case tea.KeyMsg:
		switch msg.String() {
		case "s":
			return v, tuiSetScope(goalScope{GoalID: v.id})
		case "a":
			return v, tuiSetScope(goalScope{All: true})
		}
	}
	return v, nil
}

func (v *goalDetailView) hints() []tuiHint {
	return []tuiHint{{"s", "scope"}, {"a", "all"}}
}

func (v *goalDetailView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	return renderGoalDetail(v.g, v.scope, width, height)
}

func renderGoalDetail(g *entity.Goal, scope goalScope, width, height int) string {
	if g == nil {
		return tuiDim.Render("(no goal set)")
	}
	lines := []string{}
	lines = append(lines, tuiBold.Render(g.ID)+"  "+tuiCyan.Render(g.Status))
	lines = append(lines, tuiDim.Render("scope=")+scope.label())
	if g.DerivedFrom != "" {
		lines = append(lines, tuiDim.Render("derived_from=")+g.DerivedFrom)
	}
	if g.Trigger != "" {
		lines = append(lines, tuiDim.Render("trigger=")+g.Trigger)
	}
	lines = append(lines, "")
	lines = append(lines, tuiBold.Render("Objective:"))
	lines = append(lines, "  "+tuiCyan.Render(formatGoalObjective(g)))
	lines = append(lines, "")
	lines = append(lines, tuiBold.Render("Completion:"))
	lines = append(lines, "  "+formatGoalCompletion(g))
	lines = append(lines, "")
	lines = append(lines, tuiBold.Render(fmt.Sprintf("Constraints (%d):", len(g.Constraints))))
	if len(g.Constraints) == 0 {
		lines = append(lines, "  "+tuiDim.Render("(none)"))
	} else {
		for _, c := range g.Constraints {
			lines = append(lines, "  "+entity.FormatConstraint(c))
		}
	}
	steering := g.Steering()
	if steering != "" {
		lines = append(lines, "")
		lines = append(lines, tuiBold.Render("Steering:"))
		lines = append(lines, steering)
	}
	return clampLines(strings.Join(lines, "\n"), height, width)
}

// ---- status view ----

type statusView struct {
	scope goalScope
	snap  *dashboardSnapshot
	err   error
}

func newStatusView(scope goalScope) *statusView { return &statusView{scope: scope} }

func (v *statusView) title() string { return "Status" }

func (v *statusView) init(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		snap, err := captureDashboardScoped(s, v.scope)
		return dashLoadedMsg{snap: snap, err: err}
	}
}

func (v *statusView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case dashLoadedMsg:
		v.snap = msg.snap
		v.err = msg.err
		return v, nil
	case storeChangedMsg:
		return v, v.init(s)
	}
	return v, nil
}

func (v *statusView) hints() []tuiHint { return nil }

func (v *statusView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if v.snap == nil {
		return tuiDim.Render("loading…")
	}
	snap := v.snap
	lines := []string{tuiBold.Render("Scope:") + " " + v.scope.label()}
	state := tuiGreen.Render("active")
	if snap.Paused {
		label := "PAUSED"
		if snap.PauseReason != "" {
			label += ": " + snap.PauseReason
		}
		state = tuiBoldYellow.Render(label)
	}
	lines = append(lines, tuiBold.Render("State:")+" "+state)
	lines = append(lines, tuiBold.Render("Mode:")+"  "+snap.Mode)
	if snap.MainCheckoutDirty {
		lines = append(lines, tuiBold.Render("Main checkout:")+" "+tuiBoldYellow.Render("dirty outside autoresearch-managed files"))
		for _, path := range snap.MainCheckoutDirtyPaths {
			lines = append(lines, "  "+path)
		}
	} else {
		lines = append(lines, tuiBold.Render("Main checkout:")+" clean")
	}
	lines = append(lines, "")
	lines = append(lines, tuiBold.Render("Budget:"))
	if snap.Budgets.Limits.MaxExperiments > 0 {
		lines = append(lines, fmt.Sprintf("  %s", tuiMeterColor(
			float64(snap.Budgets.Usage.Experiments),
			float64(snap.Budgets.Limits.MaxExperiments),
			fmt.Sprintf("%d/%d experiments", snap.Budgets.Usage.Experiments, snap.Budgets.Limits.MaxExperiments),
		)))
	} else {
		lines = append(lines, fmt.Sprintf("  %d experiments (no limit)", snap.Budgets.Usage.Experiments))
	}
	if snap.Budgets.Limits.MaxWallTimeH > 0 {
		lines = append(lines, fmt.Sprintf("  %s", tuiMeterColor(
			snap.Budgets.Usage.ElapsedH,
			float64(snap.Budgets.Limits.MaxWallTimeH),
			fmt.Sprintf("%.1fh/%dh elapsed", snap.Budgets.Usage.ElapsedH, snap.Budgets.Limits.MaxWallTimeH),
		)))
	}
	if snap.Budgets.Limits.FrontierStallK > 0 && !snap.ScopeAll {
		lines = append(lines, fmt.Sprintf("  %s", tuiMeterColor(
			float64(snap.StalledFor),
			float64(snap.Budgets.Limits.FrontierStallK),
			fmt.Sprintf("stalled %d/%d", snap.StalledFor, snap.Budgets.Limits.FrontierStallK),
		)))
	}
	lines = append(lines, "")
	// Counts table (sorted deterministically)
	keys := []string{"hypotheses", "experiments", "observations", "conclusions"}
	sort.Strings(keys)
	lines = append(lines, tuiBold.Render("Counts:"))
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("  %-14s %d", k, snap.Counts[k]))
	}
	return clampLines(strings.Join(lines, "\n"), height, width)
}
