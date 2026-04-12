package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// dashboardView is the home screen. It shows a responsive two-column layout
// above ~110 cols and a single stacked column below that. Focus cycles across
// four panels (tree, frontier, in-flight, events) and Enter on a selected row
// replaces the right column with a compact detail panel for that entity.
type dashboardView struct {
	snap *dashboardSnapshot
	err  error

	focus        dashFocus
	cursors      [dashPanelCount]int
	rightOverlay tuiView // non-nil = replace right column with this compact detail

	events          []store.Event
	eventsErr       error
	eventsReady     bool
	eventsAllLoaded bool
	eventsLoading   bool
}

type dashFocus int

const (
	dashFocusTree dashFocus = iota
	dashFocusFrontier
	dashFocusInFlight
	dashFocusEvents
	dashPanelCount
)

type dashLoadedMsg struct {
	snap *dashboardSnapshot
	err  error
}

type dashEventsLoadedMsg struct {
	list      []store.Event
	allLoaded bool
	replace   bool
	err       error
}

const (
	dashboardRecentEventsPageSize     = 64
	dashboardRecentEventsPrefetchRows = 8
	dashboardCompactPanelMaxHeight    = 7
	dashboardCompactPanelMinHeight    = 4
)

func newDashboardView() *dashboardView {
	return &dashboardView{}
}

func (v *dashboardView) title() string { return "Dashboard" }

func (v *dashboardView) init(s *store.Store) tea.Cmd {
	v.eventsLoading = true
	return tea.Batch(
		loadDashboardSnapshotCmd(s),
		loadDashboardEventsCmd(s, 0, dashboardRecentEventsPageSize, true),
	)
}

func (v *dashboardView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case dashLoadedMsg:
		v.snap = msg.snap
		v.err = msg.err
		return v, nil
	case dashEventsLoadedMsg:
		selected := v.selectedEventKey()
		v.eventsLoading = false
		if msg.err != nil {
			v.eventsErr = msg.err
			return v, nil
		}
		if msg.replace {
			v.events = append([]store.Event{}, msg.list...)
		} else {
			v.events = append(v.events, msg.list...)
		}
		v.eventsErr = nil
		v.eventsReady = true
		v.eventsAllLoaded = msg.allLoaded
		v.restoreEventCursor(selected)
		return v, v.maybeLoadMoreEvents(s)
	case tuiTickMsg:
		// Refresh both the dashboard and any open right-column overlay so
		// drilled-down details stay live.
		cmds := []tea.Cmd{loadDashboardSnapshotCmd(s)}
		if !v.eventsLoading {
			v.eventsLoading = true
			cmds = append(cmds, loadDashboardEventsCmd(s, 0, max(len(v.currentEvents()), dashboardRecentEventsPageSize), true))
		}
		if v.rightOverlay != nil {
			nv, cmd := v.rightOverlay.update(msg, s)
			v.rightOverlay = nv
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return v, tea.Batch(cmds...)
	case tea.KeyMsg:
		// If right overlay is showing, Esc clears it first.
		if v.rightOverlay != nil {
			switch msg.String() {
			case "esc", "backspace":
				v.rightOverlay = nil
				return v, nil
			}
		}
		switch msg.String() {
		case "tab":
			v.focus = (v.focus + 1) % dashPanelCount
			return v, nil
		case "shift+tab":
			v.focus = (v.focus + dashPanelCount - 1) % dashPanelCount
			return v, nil
		case "up", "k":
			v.moveCursor(-1)
			return v, nil
		case "down", "j":
			v.moveCursor(1)
			return v, v.maybeLoadMoreEvents(s)
		case "enter":
			return v, v.openSelected(s)
		}
		return v, nil
	}
	// Unhandled messages (e.g. hypDetailLoadedMsg from a compact drill-down's
	// init cmd) must reach the overlay — otherwise it would stay stuck on
	// "loading…" forever, which was observed as the hypothesis-detail bug.
	if v.rightOverlay != nil {
		nv, cmd := v.rightOverlay.update(msg, s)
		v.rightOverlay = nv
		return v, cmd
	}
	return v, nil
}

func (v *dashboardView) moveCursor(delta int) {
	n := v.panelLen(v.focus)
	if n == 0 {
		v.cursors[v.focus] = 0
		return
	}
	v.cursors[v.focus] = clampCursor(v.cursors[v.focus]+delta, n)
	// Changing selection updates the compact drill-down if one is open.
	v.rightOverlay = nil
}

func (v *dashboardView) panelLen(f dashFocus) int {
	if v.snap == nil {
		return 0
	}
	switch f {
	case dashFocusTree:
		return countTreeNodes(v.snap.Tree)
	case dashFocusFrontier:
		return len(v.snap.Frontier)
	case dashFocusInFlight:
		return len(v.snap.InFlight)
	case dashFocusEvents:
		return len(v.currentEvents())
	}
	return 0
}

func (v *dashboardView) openSelected(s *store.Store) tea.Cmd {
	if v.snap == nil {
		return nil
	}
	idx := v.cursors[v.focus]
	switch v.focus {
	case dashFocusTree:
		flat := flattenTree(v.snap.Tree)
		if idx < 0 || idx >= len(flat) {
			return nil
		}
		// Use a compact right-column detail.
		v.rightOverlay = newHypothesisDetailCompact(flat[idx].ID)
		return v.rightOverlay.init(s)
	case dashFocusFrontier:
		if idx < 0 || idx >= len(v.snap.Frontier) {
			return nil
		}
		v.rightOverlay = newConclusionDetailCompact(v.snap.Frontier[idx].Conclusion)
		return v.rightOverlay.init(s)
	case dashFocusInFlight:
		if idx < 0 || idx >= len(v.snap.InFlight) {
			return nil
		}
		v.rightOverlay = newExperimentDetailCompact(v.snap.InFlight[idx].ID)
		return v.rightOverlay.init(s)
	case dashFocusEvents:
		events := v.currentEvents()
		if idx < 0 || idx >= len(events) {
			return nil
		}
		v.rightOverlay = newEventDetailCompact(events[idx])
		return v.rightOverlay.init(s)
	}
	return nil
}

func (v *dashboardView) hints() []tuiHint {
	return []tuiHint{
		{"Tab", "focus"},
		{"↑↓", "select"},
		{"Enter", "drill-down"},
	}
}

func (v *dashboardView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if v.snap == nil {
		return tuiDim.Render("loading…")
	}

	// Responsive split. Below ~110 cols fall back to a single column.
	twoCol := width >= 110

	topStrip := v.renderTopStrip(width)
	topH := lipgloss.Height(topStrip)

	remaining := max(height-topH-1, 4)

	if !twoCol {
		treeH, frontierH, inFlightH, eventsH := v.stackedColumnHeights(remaining)
		sections := []string{
			v.renderTreePanel(width, treeH),
			v.renderFrontierPanel(width, frontierH),
			v.renderInFlightPanel(width, inFlightH),
			v.renderEventsPanel(width, eventsH),
		}
		return lipgloss.JoinVertical(lipgloss.Left, topStrip, "", strings.Join(sections, "\n"))
	}

	leftW := max(width*55/100-2, 40)
	rightW := max(width-leftW-3, 30)

	treeH := remaining * 60 / 100
	lessonsH := remaining - treeH
	left := lipgloss.JoinVertical(lipgloss.Left,
		v.renderTreePanel(leftW, treeH),
		v.renderLessonsPanel(leftW, lessonsH),
	)

	var right string
	if v.rightOverlay != nil {
		title := tuiPanelTitle.Render("Detail")
		content := v.rightOverlay.view(rightW-4, remaining-2)
		right = boxPanel(title, content, rightW, remaining, true)
	} else {
		frontierH, inFlightH, eventsH := v.rightColumnHeights(remaining)
		right = lipgloss.JoinVertical(lipgloss.Left,
			v.renderFrontierPanel(rightW, frontierH),
			v.renderInFlightPanel(rightW, inFlightH),
			v.renderEventsPanel(rightW, eventsH),
		)
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	return lipgloss.JoinVertical(lipgloss.Left, topStrip, body)
}

// renderTopStrip renders goal + constraints + budget in two compact lines.
// Mode/state/counts have moved to the global header bar so they aren't
// repeated here.
func (v *dashboardView) renderTopStrip(width int) string {
	snap := v.snap
	var lines []string

	// Line 1: goal objective, with constraints inlined on the right.
	if snap.Goal == nil {
		lines = append(lines, tuiBold.Render("Goal:")+" "+tuiDim.Render("(no goal set — run `autoresearch goal set`)"))
	} else {
		obj := snap.Goal.Objective
		line := tuiBold.Render("Goal:") + " " + tuiCyan.Render(obj.Direction+" "+obj.Instrument)
		if obj.Target != "" {
			line += " on " + obj.Target
		}
		if obj.TargetEffect > 0 {
			line += fmt.Sprintf(" (target_effect=%g)", obj.TargetEffect)
		}
		if len(snap.Goal.Constraints) > 0 {
			var cs []string
			for _, c := range snap.Goal.Constraints {
				switch {
				case c.Max != nil:
					cs = append(cs, fmt.Sprintf("%s%s%g", c.Instrument, tuiDim.Render("≤"), *c.Max))
				case c.Min != nil:
					cs = append(cs, fmt.Sprintf("%s%s%g", c.Instrument, tuiDim.Render("≥"), *c.Min))
				case c.Require != "":
					cs = append(cs, fmt.Sprintf("%s=%s", c.Instrument, tuiCyan.Render(c.Require)))
				}
			}
			line += tuiDim.Render("  |  ") + strings.Join(cs, tuiDim.Render("  "))
		}
		lines = append(lines, line)
	}

	// Append budget meters to the goal line so everything fits on one line.
	var parts []string
	if snap.Budgets.Limits.MaxExperiments > 0 {
		s := fmt.Sprintf("%d/%d exp",
			snap.Budgets.Usage.Experiments, snap.Budgets.Limits.MaxExperiments)
		parts = append(parts, tuiMeterColor(float64(snap.Budgets.Usage.Experiments),
			float64(snap.Budgets.Limits.MaxExperiments), s))
	} else {
		parts = append(parts, fmt.Sprintf("%d exp", snap.Budgets.Usage.Experiments))
	}
	if snap.Budgets.Limits.MaxWallTimeH > 0 {
		s := fmt.Sprintf("%.1fh/%dh",
			snap.Budgets.Usage.ElapsedH, snap.Budgets.Limits.MaxWallTimeH)
		parts = append(parts, tuiMeterColor(snap.Budgets.Usage.ElapsedH,
			float64(snap.Budgets.Limits.MaxWallTimeH), s))
	}
	if snap.Budgets.Limits.FrontierStallK > 0 {
		s := fmt.Sprintf("stalled %d/%d", snap.StalledFor, snap.Budgets.Limits.FrontierStallK)
		parts = append(parts, tuiMeterColor(float64(snap.StalledFor),
			float64(snap.Budgets.Limits.FrontierStallK), s))
	}
	budget := strings.Join(parts, tuiDim.Render(" · "))
	if len(lines) > 0 {
		lines[len(lines)-1] += tuiDim.Render("  |  ") + budget
	} else {
		lines = append(lines, tuiBold.Render("Budget:")+" "+budget)
	}

	// Truncate each line to width so nothing wraps.
	for i, l := range lines {
		lines[i] = truncDisplay(l, width)
	}
	return strings.Join(lines, "\n")
}

func (v *dashboardView) renderTreePanel(width, height int) string {
	active := v.focus == dashFocusTree
	title := tuiPanelTitle.Render("Hypothesis ") + tuiBoldYellow.Render("t") + tuiPanelTitle.Render("ree")
	flat := flattenTree(v.snap.Tree)
	if len(flat) == 0 {
		return boxPanel(title, tuiDim.Render("(no hypotheses)"), width, height, active)
	}
	innerW := width - 4
	lines := renderTreeLines(v.snap.Tree, innerW)
	lines = truncLines(lines, innerW)
	cursor := clampCursor(v.cursors[dashFocusTree], len(lines))
	if active {
		lines = highlightRow(lines, cursor, innerW)
	}
	inner := max(height-2, 1)
	lines = scrollWindow(lines, cursor, inner)
	return boxPanel(title, strings.Join(lines, "\n"), width, height, active)
}

func (v *dashboardView) renderLessonsPanel(width, height int) string {
	title := tuiPanelTitle.Render("Lessons")
	snap := v.snap
	if len(snap.RecentLessons) == 0 {
		return boxPanel(title, tuiDim.Render("(no active lessons)"), width, height, false)
	}
	innerW := width - 4
	// Prefix: "L-0006  " = 8 visible chars.
	claimW := max(innerW-8, 20)
	var lines []string
	for _, l := range snap.RecentLessons {
		lines = append(lines, fmt.Sprintf("%s  %s",
			tuiCyan.Render(l.ID), truncate(l.Claim, claimW)))
	}
	lines = truncLines(lines, innerW)
	return boxPanel(title, strings.Join(lines, "\n"), width, height, false)
}

func (v *dashboardView) renderFrontierPanel(width, height int) string {
	active := v.focus == dashFocusFrontier
	title := tuiPanelTitle.Render("Frontier")
	snap := v.snap
	if snap.Goal == nil {
		return boxPanel(title, tuiDim.Render("(no goal set)"), width, height, active)
	}
	if len(snap.Frontier) == 0 {
		return boxPanel(title, tuiDim.Render("(no feasible supported conclusions yet)"), width, height, active)
	}
	lines := []string{}
	for i, r := range snap.Frontier {
		marker := "  "
		if i == 0 {
			marker = tuiBoldYellow.Render("* ")
		}
		line := fmt.Sprintf("%s%s  %s  %s=%.6g",
			marker,
			tuiCyan.Render(r.Conclusion),
			tuiCyan.Render(r.Hypothesis),
			snap.Goal.Objective.Instrument,
			r.Value)
		lines = append(lines, line)
	}
	innerW := width - 4
	lines = truncLines(lines, innerW)
	cursor := v.cursors[dashFocusFrontier]
	if active {
		lines = highlightRow(lines, cursor, innerW)
	}
	inner := max(height-2, 1)
	lines = scrollWindow(lines, cursor, inner)
	return boxPanel(title, strings.Join(lines, "\n"), width, height, active)
}

func (v *dashboardView) renderInFlightPanel(width, height int) string {
	active := v.focus == dashFocusInFlight
	title := tuiPanelTitle.Render("In flight")
	if len(v.snap.InFlight) == 0 {
		return boxPanel(title, tuiDim.Render("(nothing in flight)"), width, height, active)
	}
	lines := []string{}
	for _, r := range v.snap.InFlight {
		elapsed := "?"
		if r.ImplementedAt != nil {
			elapsed = formatElapsed(time.Duration(r.ElapsedS) * time.Second)
		}
		line := fmt.Sprintf("%-8s  %s  %s  inst=%s",
			r.ID,
			tuiExpStatusBadge(r.Status),
			elapsed,
			strings.Join(r.Instruments, ","))
		lines = append(lines, line)
	}
	innerW := width - 4
	lines = truncLines(lines, innerW)
	cursor := v.cursors[dashFocusInFlight]
	if active {
		lines = highlightRow(lines, cursor, innerW)
	}
	inner := max(height-2, 1)
	lines = scrollWindow(lines, cursor, inner)
	return boxPanel(title, strings.Join(lines, "\n"), width, height, active)
}

func (v *dashboardView) renderEventsPanel(width, height int) string {
	active := v.focus == dashFocusEvents
	events := v.currentEvents()
	title := tuiPanelTitle.Render("Recent events")
	switch {
	case v.eventsLoading && len(events) > 0:
		title += " " + tuiDim.Render("(loading more)")
	case !v.eventsAllLoaded && v.eventsReady:
		title += " " + tuiDim.Render("(scroll for older)")
	}
	if v.eventsErr != nil && len(events) == 0 {
		return boxPanel(title, tuiRed.Render("error: "+v.eventsErr.Error()), width, height, active)
	}
	if len(events) == 0 {
		return boxPanel(title, tuiDim.Render("(no events yet)"), width, height, active)
	}
	lines := []string{}
	for _, e := range events {
		subject := e.Subject
		if subject == "" {
			subject = "-"
		}
		lines = append(lines, fmt.Sprintf("%s  %s  %-8s  %s",
			tuiDim.Render(e.Ts.UTC().Format("15:04:05")),
			padRight(tuiEventKindColor(e.Kind), 24),
			subject,
			tuiDim.Render(e.Actor),
		))
	}
	innerW := width - 4
	lines = truncLines(lines, innerW)
	cursor := v.cursors[dashFocusEvents]
	if active {
		lines = highlightRow(lines, cursor, innerW)
	}
	inner := max(height-2, 1)
	lines = scrollWindow(lines, cursor, inner)
	return boxPanel(title, strings.Join(lines, "\n"), width, height, active)
}

func loadDashboardSnapshotCmd(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		snap, err := captureDashboard(s)
		return dashLoadedMsg{snap: snap, err: err}
	}
}

func loadDashboardEventsCmd(s *store.Store, offsetNewest, limit int, replace bool) tea.Cmd {
	return func() tea.Msg {
		all, err := s.Events(0)
		if err != nil {
			return dashEventsLoadedMsg{err: err}
		}
		list, allLoaded := readDashboardRecentEvents(all, offsetNewest, limit)
		return dashEventsLoadedMsg{
			list:      list,
			allLoaded: allLoaded,
			replace:   replace,
		}
	}
}

func (v *dashboardView) currentEvents() []store.Event {
	if v.eventsReady {
		return v.events
	}
	if v.snap != nil {
		return v.snap.RecentEvents
	}
	return nil
}

func (v *dashboardView) selectedEventKey() string {
	events := v.currentEvents()
	idx := v.cursors[dashFocusEvents]
	if idx < 0 || idx >= len(events) {
		return ""
	}
	return dashboardEventKey(events[idx])
}

func (v *dashboardView) restoreEventCursor(selected string) {
	if selected != "" {
		for i, e := range v.currentEvents() {
			if dashboardEventKey(e) == selected {
				v.cursors[dashFocusEvents] = i
				return
			}
		}
	}
	v.cursors[dashFocusEvents] = clampCursor(v.cursors[dashFocusEvents], len(v.currentEvents()))
}

func (v *dashboardView) maybeLoadMoreEvents(s *store.Store) tea.Cmd {
	if s == nil || !v.eventsReady || v.eventsLoading || v.eventsAllLoaded || v.focus != dashFocusEvents {
		return nil
	}
	if len(v.events)-1-v.cursors[dashFocusEvents] > dashboardRecentEventsPrefetchRows {
		return nil
	}
	v.eventsLoading = true
	return loadDashboardEventsCmd(s, len(v.events), dashboardRecentEventsPageSize, false)
}

func dashboardEventKey(e store.Event) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s",
		e.Ts.UTC().Format(time.RFC3339Nano),
		e.Kind,
		e.Actor,
		e.Subject,
		string(e.Data),
	)
}

func dashboardPanelHeight(rows, maxHeight int) int {
	rows = max(rows, 1)
	h := rows + 3
	if h < dashboardCompactPanelMinHeight {
		return dashboardCompactPanelMinHeight
	}
	if h > maxHeight {
		return maxHeight
	}
	return h
}

func shrinkDashboardPanels(total, fillMin int, heights ...*int) {
	used := fillMin
	for _, h := range heights {
		used += *h
	}
	for used > total {
		shrunk := false
		for _, h := range heights {
			if *h > dashboardCompactPanelMinHeight && used > total {
				*h--
				used--
				shrunk = true
			}
		}
		if !shrunk {
			return
		}
	}
}

func (v *dashboardView) rightColumnHeights(total int) (frontierH, inFlightH, eventsH int) {
	frontierH = dashboardPanelHeight(v.frontierRows(), dashboardCompactPanelMaxHeight)
	inFlightH = dashboardPanelHeight(v.inFlightRows(), dashboardCompactPanelMaxHeight)
	shrinkDashboardPanels(total, dashboardCompactPanelMinHeight, &frontierH, &inFlightH)
	eventsH = max(total-frontierH-inFlightH, dashboardCompactPanelMinHeight)
	return frontierH, inFlightH, eventsH
}

func (v *dashboardView) stackedColumnHeights(total int) (treeH, frontierH, inFlightH, eventsH int) {
	treeH = max(total*45/100, 8)
	if treeH > total {
		treeH = total
	}
	frontierH = dashboardPanelHeight(v.frontierRows(), dashboardCompactPanelMaxHeight)
	inFlightH = dashboardPanelHeight(v.inFlightRows(), dashboardCompactPanelMaxHeight)
	for treeH+frontierH+inFlightH+dashboardCompactPanelMinHeight > total && treeH > 4 {
		treeH--
	}
	shrinkDashboardPanels(total-treeH, dashboardCompactPanelMinHeight, &frontierH, &inFlightH)
	eventsH = max(total-treeH-frontierH-inFlightH, dashboardCompactPanelMinHeight)
	return treeH, frontierH, inFlightH, eventsH
}

func (v *dashboardView) frontierRows() int {
	if v.snap == nil || v.snap.Goal == nil || len(v.snap.Frontier) == 0 {
		return 1
	}
	return len(v.snap.Frontier)
}

func (v *dashboardView) inFlightRows() int {
	if v.snap == nil || len(v.snap.InFlight) == 0 {
		return 1
	}
	return len(v.snap.InFlight)
}

// boxPanel frames content in a bordered panel. In this version of lipgloss,
// Width with horizontal Padding treats the padding as *inside* the Width
// budget — so to get a panel whose total rendered width is `width`
// (including the 2 border cells), we must set Width to `width-2` and
// truncate each content line to `width-4` (border+padding). The title is
// overlaid onto the top border so it does not consume a body row.
func boxPanel(title, content string, width, height int, active bool) string {
	style := tuiPanelBorder
	if active {
		style = tuiPanelBorderActive
	}
	inner := max(width-4, 10)
	innerH := max(height-2, 1)
	content = clampLines(content, innerH, inner)
	panel := style.Width(width - 2).Render(content)
	if title == "" {
		return panel
	}
	return panelWithBorderTitle(panel, title, width, style)
}

func panelWithBorderTitle(panel, title string, width int, style lipgloss.Style) string {
	if width < 8 {
		return panel
	}
	lines := strings.Split(panel, "\n")
	if len(lines) == 0 {
		return panel
	}
	borderParts := style.GetBorderStyle()
	border := lipgloss.NewStyle().
		Foreground(style.GetBorderTopForeground()).
		Background(style.GetBorderTopBackground())

	innerW := max(width-lipgloss.Width(borderParts.TopLeft)-lipgloss.Width(borderParts.TopRight), 0)
	leftSep := borderParts.MiddleRight + " "
	rightSep := " " + borderParts.MiddleLeft
	leftRun := 1
	maxTitleW := innerW - leftRun - lipgloss.Width(leftSep) - lipgloss.Width(rightSep)
	if maxTitleW < 1 {
		leftRun = 0
		maxTitleW = innerW - lipgloss.Width(leftSep) - lipgloss.Width(rightSep)
	}
	title = truncDisplay(title, max(maxTitleW, 1))
	rightRun := max(innerW-leftRun-lipgloss.Width(leftSep)-lipgloss.Width(title)-lipgloss.Width(rightSep), 0)

	lines[0] =
		border.Render(borderParts.TopLeft+strings.Repeat(borderParts.Top, leftRun)) +
			border.Render(leftSep) +
			title +
			border.Render(rightSep+strings.Repeat(borderParts.Top, rightRun)+borderParts.TopRight)
	return strings.Join(lines, "\n")
}
