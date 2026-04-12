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

func newDashboardView() *dashboardView {
	return &dashboardView{}
}

func (v *dashboardView) title() string { return "Dashboard" }

func (v *dashboardView) init(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		snap, err := captureDashboard(s)
		return dashLoadedMsg{snap: snap, err: err}
	}
}

func (v *dashboardView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case dashLoadedMsg:
		v.snap = msg.snap
		v.err = msg.err
		return v, nil
	case tuiTickMsg:
		// Refresh both the dashboard and any open right-column overlay so
		// drilled-down details stay live.
		cmds := []tea.Cmd{v.init(s)}
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
			return v, nil
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
		return len(v.snap.RecentEvents)
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
		if idx < 0 || idx >= len(v.snap.RecentEvents) {
			return nil
		}
		v.rightOverlay = newEventDetailCompact(v.snap.RecentEvents[idx])
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
		// Single column: tree, frontier, in-flight, events stacked.
		sections := []string{
			v.renderTreePanel(width, remaining/2),
			v.renderFrontierPanel(width, remaining/4),
			v.renderInFlightPanel(width, remaining/8),
			v.renderEventsPanel(width, remaining/8),
		}
		return lipgloss.JoinVertical(lipgloss.Left, topStrip, "", strings.Join(sections, "\n"))
	}

	leftW := max(width*55/100-2, 40)
	rightW := max(width-leftW-3, 30)

	left := v.renderTreePanel(leftW, remaining)

	var right string
	if v.rightOverlay != nil {
		right = tuiPanelBorder.Width(rightW - 4).Render(
			tuiPanelTitle.Render("Detail") + "\n" +
				v.rightOverlay.view(rightW-6, remaining-4),
		)
	} else {
		rightHalf := remaining / 3
		right = lipgloss.JoinVertical(lipgloss.Left,
			v.renderFrontierPanel(rightW, rightHalf),
			v.renderInFlightPanel(rightW, rightHalf),
			v.renderEventsPanel(rightW, remaining-2*rightHalf),
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

	// Line 2: budget meters only.
	parts := []string{}
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
	lines = append(lines, tuiBold.Render("Budget:")+" "+strings.Join(parts, tuiDim.Render("  ·  ")))

	// Truncate each line to width so nothing wraps.
	for i, l := range lines {
		lines[i] = truncDisplay(l, width)
	}
	return strings.Join(lines, "\n")
}

func (v *dashboardView) renderTreePanel(width, height int) string {
	active := v.focus == dashFocusTree
	title := tuiPanelTitle.Render("Hypothesis tree")
	if active {
		title += " " + tuiDim.Render("(focused)")
	}
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

func (v *dashboardView) renderFrontierPanel(width, height int) string {
	active := v.focus == dashFocusFrontier
	title := tuiPanelTitle.Render("Frontier")
	if active {
		title += " " + tuiDim.Render("(focused)")
	}
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
	if active {
		title += " " + tuiDim.Render("(focused)")
	}
	if len(v.snap.InFlight) == 0 {
		return boxPanel(title, tuiDim.Render("(nothing in flight)"), width, height, active)
	}
	lines := []string{}
	for _, r := range v.snap.InFlight {
		elapsed := "?"
		if r.ImplementedAt != nil {
			elapsed = formatElapsed(time.Duration(r.ElapsedS) * time.Second)
		}
		line := fmt.Sprintf("%-8s  %s  %s  %s  inst=%s",
			r.ID,
			tuiExpStatusBadge(r.Status),
			tuiDim.Render("tier="+r.Tier),
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
	title := tuiPanelTitle.Render(fmt.Sprintf("Recent events (last %d)", len(v.snap.RecentEvents)))
	if active {
		title += " " + tuiDim.Render("(focused)")
	}
	if len(v.snap.RecentEvents) == 0 {
		return boxPanel(title, tuiDim.Render("(no events yet)"), width, height, active)
	}
	lines := []string{}
	for _, e := range v.snap.RecentEvents {
		subject := e.Subject
		if subject == "" {
			subject = "-"
		}
		lines = append(lines, fmt.Sprintf("%s  %s  %-12s  %s",
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

// boxPanel frames content in a bordered panel with a title line. In this
// version of lipgloss, Width with horizontal Padding treats the padding as
// *inside* the Width budget — so to get a panel whose total rendered width is
// `width` (including the 2 border cells), we must set Width to `width-2` and
// truncate each content line to `width-4` (border+padding). Callers are
// expected to have already truncated their body lines to `width-4`.
func boxPanel(title, content string, width, height int, active bool) string {
	style := tuiPanelBorder
	if active {
		style = tuiPanelBorderActive
	}
	inner := max(width-4, 10)
	innerH := max(height-2, 1)
	// Truncate title to inner so long titles don't wrap either.
	title = truncDisplay(title, inner)
	content = clampLines(content, innerH-1, inner)
	body := title + "\n" + content
	return style.Width(width - 2).Render(body)
}

