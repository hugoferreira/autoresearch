package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

// ---- messages ----

// tuiTickMsg is internal to the root update loop: it schedules a poll of
// the event log and reschedules itself. Views no longer observe ticks
// directly — they react to storeChangedMsg when the poll surfaces new
// events that actually changed something in .research/.
type tuiTickMsg time.Time
type tuiErrMsg struct{ err error }

// storeChangedMsg is dispatched to the top view whenever the poll loop
// sees new events in events.jsonl. Views decide whether to reload based
// on what changed — most list and aggregate views just reload on any
// change; detail views can narrow to events with a matching subject.
// Quiet ticks produce no storeChangedMsg at all, so scroll and cursor
// state are preserved whenever .research/ hasn't changed.
type storeChangedMsg struct {
	events []store.Event
	newOff int64
}

// eventOffsetPrimedMsg carries the initial byte offset into events.jsonl
// at TUI startup. We prime to current EOF so the first poll after startup
// sees only events appended *during* the TUI session, not the entire
// research history (which is already reflected in the entity files that
// view.init() loaded).
type eventOffsetPrimedMsg struct{ offset int64 }

// pushMsg tells the root model to push a new view onto the stack.
type tuiPushMsg struct{ v tuiView }

// resetMsg clears the stack back to just the dashboard.
type tuiResetMsg struct{}

// setScopeMsg updates the TUI's read-only session scope.
type tuiSetScopeMsg struct{ scope goalScope }

// chromeLoadedMsg carries the cheap state+config+counts read that the header
// needs on every tick. It's read independently of the active view so the
// header stays fresh no matter which view is on top.
type chromeLoadedMsg struct {
	paused      bool
	pauseReason string
	mode        string
	counts      map[string]int
}

// ---- view interface ----

type tuiHint struct {
	keys string
	desc string
}

// tuiView is the contract every view implements. Views own their own cursor,
// scroll and filter state. init is called once when the view is pushed and
// should return a tea.Cmd that does the initial data load.
//
// kind returns a stable identifier used by the key router to canonicalize the
// breadcrumb when a top-level jump key is pressed (e.g. pressing H twice
// should not push two Hypotheses views). Two views with the same kind are
// considered interchangeable for jump-back purposes.
type tuiView interface {
	title() string
	kind() string
	init(s *store.Store) tea.Cmd
	update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd)
	view(width, height int) string
	hints() []tuiHint
}

// ---- root model ----

type tuiModel struct {
	store    *store.Store
	scope    goalScope
	stack    []tuiView
	width    int
	height   int
	refresh  time.Duration
	showHelp bool
	err      error

	chrome chromeLoadedMsg

	// eventOffset is the byte offset into events.jsonl up to which the
	// poll loop has already observed events. Primed at startup by
	// primeEventOffset(); advanced by every storeChangedMsg.
	eventOffset int64
}

func newTuiModel(s *store.Store, scope goalScope, refresh time.Duration) tuiModel {
	return tuiModel{
		store:   s,
		scope:   scope,
		stack:   []tuiView{newDashboardView(scope)},
		refresh: refresh,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(
		m.top().init(m.store),
		fetchChrome(m.store, m.scope),
		primeEventOffset(m.store),
		tuiTick(m.refresh),
	)
}

func tuiTick(d time.Duration) tea.Cmd {
	if d <= 0 {
		return nil
	}
	return tea.Tick(d, func(t time.Time) tea.Msg { return tuiTickMsg(t) })
}

// primeEventOffset reads the current EOF of events.jsonl and returns it
// as the starting offset for the poll loop. This guarantees the TUI only
// reacts to events appended *during* the session — historical events are
// already reflected in the entity files that view.init() loads.
func primeEventOffset(s *store.Store) tea.Cmd {
	if s == nil {
		return nil
	}
	return func() tea.Msg {
		_, off, err := s.EventsSince(0)
		if err != nil {
			// Non-fatal: fall back to offset 0 and let the first poll
			// dispatch historical events once. Better than a crashed
			// refresh loop.
			return eventOffsetPrimedMsg{offset: 0}
		}
		return eventOffsetPrimedMsg{offset: off}
	}
}

// pollEvents reads any events appended to events.jsonl since offset and
// returns them as a storeChangedMsg. Quiet polls (no new events) still
// emit a storeChangedMsg with an empty slice so the root handler can
// advance its offset, but the root filters empty batches out before
// dispatching to the top view — so views that haven't hooked into the
// refresh cycle see nothing, and their scroll/cursor state is preserved.
func pollEvents(s *store.Store, offset int64) tea.Cmd {
	if s == nil {
		return nil
	}
	return func() tea.Msg {
		events, newOff, err := s.EventsSince(offset)
		if err != nil {
			return storeChangedMsg{newOff: offset}
		}
		return storeChangedMsg{events: events, newOff: newOff}
	}
}

// fetchChrome reads the cheap state+config+counts summary used by the header
// status bar. It is scheduled independently of the active view so the header
// stays fresh on every tick regardless of what's on top of the stack.
func fetchChrome(s *store.Store, scope goalScope) tea.Cmd {
	return func() tea.Msg {
		msg := chromeLoadedMsg{}
		if s == nil {
			return msg
		}
		if st, err := s.State(); err == nil {
			msg.paused = st.Paused
			msg.pauseReason = st.PauseReason
		}
		if cfg, err := s.Config(); err == nil {
			msg.mode = cfg.Mode
		}
		// Unscoped chrome: Counts() is an O(ReadDir) summary per entity
		// directory — avoids re-parsing every entity file just to compute
		// len(). Scoped chrome still pays the full read today; once the
		// store cache lands it'll be cheap for both paths.
		if scope.All || scope.GoalID == "" {
			if counts, err := s.Counts(); err == nil {
				msg.counts = map[string]int{
					"hypotheses":   counts["hypotheses"],
					"experiments":  counts["experiments"],
					"observations": counts["observations"],
					"conclusions":  counts["conclusions"],
				}
			}
			return msg
		}
		resolver := newGoalScopeResolver(s, scope)
		hyps, herr := s.ListHypotheses()
		exps, eerr := s.ListExperiments()
		obs, oerr := s.ListObservations()
		concls, cerr := s.ListConclusions()
		if herr == nil && eerr == nil && oerr == nil && cerr == nil {
			exps, eerr = resolver.filterExperiments(exps)
			obs, oerr = resolver.filterObservations(obs)
			concls, cerr = resolver.filterConclusions(concls)
			if eerr == nil && oerr == nil && cerr == nil {
				msg.counts = map[string]int{
					"hypotheses":   len(resolver.filterHypotheses(hyps)),
					"experiments":  len(exps),
					"observations": len(obs),
					"conclusions":  len(concls),
				}
			}
		}
		return msg
	}
}

func (m tuiModel) top() tuiView      { return m.stack[len(m.stack)-1] }
func (m *tuiModel) setTop(v tuiView) { m.stack[len(m.stack)-1] = v }
func (m *tuiModel) push(v tuiView)   { m.stack = append(m.stack, v) }
func (m *tuiModel) pop() {
	if len(m.stack) > 1 {
		m.stack = m.stack[:len(m.stack)-1]
	}
}

// jumpTo canonicalizes the stack when a top-level jump key is pressed.
// If v's kind already exists anywhere in the stack, the stack is truncated
// to that existing view (preserving its cursor/filter state). Otherwise the
// stack is reset to [dashboard, v] so every top-level view sits at a stable
// 2-deep breadcrumb. The existing dashboard instance is preserved so jumping
// away and back doesn't drop its loaded snapshot/cursor state on the floor.
// Returns the init command for the new view (or nil if we jumped back to an
// existing one).
func (m *tuiModel) jumpTo(v tuiView, s *store.Store) tea.Cmd {
	k := v.kind()
	for i, existing := range m.stack {
		if existing.kind() == k {
			m.stack = m.stack[:i+1]
			return nil
		}
	}
	dashboard := tuiView(newDashboardView(m.scope))
	for _, existing := range m.stack {
		if existing.kind() == kindDashboard {
			dashboard = existing
			break
		}
	}
	m.stack = []tuiView{dashboard, v}
	return v.init(s)
}

func (m *tuiModel) applyScope(scope goalScope) tea.Cmd {
	m.scope = scope
	dashboard := newDashboardView(scope)
	goals := newGoalListView(scope)
	m.stack = []tuiView{dashboard, goals}
	if m.store == nil {
		return nil
	}
	return tea.Batch(
		dashboard.init(m.store),
		goals.init(m.store),
		fetchChrome(m.store, m.scope),
	)
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tuiErrMsg:
		m.err = msg.err
		return m, nil

	case tuiTickMsg:
		// Ticks drive the poll loop, not the view refresh. We fire a
		// single pollEvents against the known byte offset and reschedule
		// ourselves. Views only react if the poll surfaces new events,
		// so a quiet tick costs one os.Stat and zero view churn.
		return m, tea.Batch(pollEvents(m.store, m.eventOffset), tuiTick(m.refresh))

	case eventOffsetPrimedMsg:
		m.eventOffset = msg.offset
		return m, nil

	case storeChangedMsg:
		m.eventOffset = msg.newOff
		if len(msg.events) == 0 {
			// Quiet poll — no changes to react to, no view churn, no
			// chrome reload. Cursor and pager scroll stay where the
			// user left them.
			return m, nil
		}
		// Drop cache entries for any subjects these events mention, so
		// the next read picks up the change even if filesystem mtime
		// resolution would otherwise mask it (see
		// store.InvalidateFromEvents).
		m.store.InvalidateFromEvents(msg.events)
		// Dispatch to the top view; each view's case storeChangedMsg
		// decides whether to reload.
		top := m.top()
		nv, cmd := top.update(msg, m.store)
		m.setTop(nv)
		return m, tea.Batch(cmd, fetchChrome(m.store, m.scope))

	case chromeLoadedMsg:
		m.chrome = msg
		return m, nil

	case tuiPushMsg:
		m.push(msg.v)
		return m, msg.v.init(m.store)

	case tuiResetMsg:
		m.stack = []tuiView{newDashboardView(m.scope)}
		return m, m.top().init(m.store)

	case tuiSetScopeMsg:
		return m, m.applyScope(msg.scope)

	case tea.KeyMsg:
		if m.showHelp {
			// Any key dismisses help.
			m.showHelp = false
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "?":
			m.showHelp = true
			return m, nil
		case "esc", "backspace":
			if len(m.stack) > 1 {
				m.pop()
				// The dashboard sits hidden under every top-level view and
				// does not receive refresh messages while off-screen. When it
				// becomes visible again, force a reload so it never stays on
				// a stale or uninitialized snapshot.
				if m.store != nil && m.top().kind() == kindDashboard {
					return m, m.top().init(m.store)
				}
				return m, nil
			}
		case "H":
			return m, m.jumpTo(newHypothesisListView(m.scope), m.store)
		case "E":
			return m, m.jumpTo(newExperimentListView(m.scope), m.store)
		case "C":
			return m, m.jumpTo(newConclusionListView(m.scope), m.store)
		case "L":
			return m, m.jumpTo(newEventListView(m.scope), m.store)
		case "N":
			return m, m.jumpTo(newLessonListView(m.scope), m.store)
		case "T":
			return m, m.jumpTo(newTreeView(m.scope), m.store)
		case "F":
			return m, m.jumpTo(newFrontierView(m.scope), m.store)
		case "O":
			return m, m.jumpTo(newGoalListView(m.scope), m.store)
		case "S":
			return m, m.jumpTo(newStatusView(m.scope), m.store)
		case "A":
			return m, m.jumpTo(newArtifactListView(m.scope), m.store)
		case "I":
			return m, m.jumpTo(newInstrumentListView(), m.store)
		case "R":
			return m, m.jumpTo(newHypothesisListViewForReport(m.scope), m.store)
		case "D":
			return m, func() tea.Msg { return tuiResetMsg{} }
		}
		// Fall through to view-local handling.
		top := m.top()
		nv, cmd := top.update(msg, m.store)
		m.setTop(nv)
		return m, cmd
	}

	// Unhandled message — forward to top view.
	top := m.top()
	nv, cmd := top.update(msg, m.store)
	m.setTop(nv)
	return m, cmd
}

func (m tuiModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "initializing…"
	}
	header := m.renderHeader()
	hints := m.renderHintBar()
	bodyHeight := max(m.height-lipgloss.Height(header)-lipgloss.Height(hints), 1)
	body := m.top().view(m.width, bodyHeight)
	// Clamp body to bodyHeight.
	body = clampLines(body, bodyHeight, m.width)

	out := lipgloss.JoinVertical(lipgloss.Left, header, body, hints)
	if m.showHelp {
		out = overlay(out, m.renderHelp(), m.width, m.height)
	}
	if m.err != nil {
		out = overlay(out, tuiRed.Render("error: "+m.err.Error()), m.width, m.height)
	}
	return out
}

// tuiPush returns a tea.Cmd that emits a tuiPushMsg.
func tuiPush(v tuiView) tea.Cmd {
	return func() tea.Msg { return tuiPushMsg{v: v} }
}

func tuiSetScope(scope goalScope) tea.Cmd {
	return func() tea.Msg { return tuiSetScopeMsg{scope: scope} }
}

// clampLines truncates s to at most n lines and pads shorter content so the
// enclosing layout keeps a stable height. Lines longer than width are not
// clipped here (views are responsible for their own horizontal budget).
func clampLines(s string, n, width int) string {
	_ = width // kept in the signature so call sites read naturally
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// overlay centers an overlay panel atop a base view. It's a simple
// paste-on-top implementation — fine for our small help/error boxes.
func overlay(base, panel string, width, height int) string {
	panelW := lipgloss.Width(panel)
	panelH := lipgloss.Height(panel)
	if panelW >= width || panelH >= height {
		return panel
	}
	ox := (width - panelW) / 2
	oy := (height - panelH) / 2
	baseLines := strings.Split(base, "\n")
	panelLines := strings.Split(panel, "\n")
	for i, pl := range panelLines {
		row := oy + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLines[row] = spliceLine(baseLines[row], pl, ox, width)
	}
	return strings.Join(baseLines, "\n")
}

// spliceLine overwrites dst with src starting at column ox, measured in
// visible runes (ignoring ANSI escapes). It is deliberately simple and used
// only for overlays that know they fit.
func spliceLine(dst, src string, ox, width int) string {
	// Strip the base line's ANSI and truncate naive. We rely on the fact that
	// overlays cover a central box and the surrounding columns of the base
	// line are replaced verbatim by padding.
	base := stripANSI(dst)
	runes := []rune(base)
	for len(runes) < width {
		runes = append(runes, ' ')
	}
	// Splice src (also stripped for measurement, but kept styled for output).
	srcPlain := stripANSI(src)
	srcRunes := []rune(srcPlain)
	left := string(runes[:ox])
	rightStart := min(ox+len(srcRunes), len(runes))
	right := string(runes[rightStart:])
	return left + src + right
}

// ---- chrome ----

// renderHeader produces a single-line status bar with breadcrumb, project,
// mode, counts and pause state. Everything is packed onto one line and
// truncated from the right if the terminal is too narrow. The bar never
// wraps: if the content overflows, the paused-reason (then the counts, then
// the breadcrumb tail) are trimmed in that priority order.
func (m tuiModel) renderHeader() string {
	var crumbs []string
	for _, v := range m.stack {
		crumbs = append(crumbs, v.title())
	}
	crumbTxt := strings.Join(crumbs, " › ")

	proj := ""
	if m.store != nil {
		proj = filepath.Base(m.store.Root())
	}

	brand := tuiBold.Render("autoresearch")
	left := brand + "  ·  " + crumbTxt
	if proj != "" {
		left += "  ·  " + tuiDim.Render(proj)
	}

	// Build right-hand segments with priorities for progressive shedding.
	rightOrder := []string{}
	if m.chrome.mode != "" {
		rightOrder = append(rightOrder, tuiDim.Render("mode=")+m.chrome.mode)
	}
	if len(m.chrome.counts) > 0 {
		rightOrder = append(rightOrder, fmt.Sprintf("H%d/E%d/O%d/C%d",
			m.chrome.counts["hypotheses"],
			m.chrome.counts["experiments"],
			m.chrome.counts["observations"],
			m.chrome.counts["conclusions"]))
	}
	rightOrder = append(rightOrder, tuiDim.Render("scope=")+m.scope.label())
	if m.refresh > 0 {
		rightOrder = append(rightOrder, tuiDim.Render("↻"+m.refresh.String()))
	}
	state := tuiGreen.Render("● active")
	if m.chrome.paused {
		label := "⏸ PAUSED"
		if m.chrome.pauseReason != "" {
			label += ": " + m.chrome.pauseReason
		}
		state = tuiBoldYellow.Render(label)
	}
	rightOrder = append(rightOrder, state)

	// Content width inside the header bar's Padding(0,1). The outer bar
	// will be rendered with Width(m.width); lipgloss puts the 2 padding
	// cells inside that budget, so our usable line length is m.width-2.
	inner := max(m.width-2, 20)

	// Assemble right-hand side, measure, fit.
	right := strings.Join(rightOrder, "  ·  ")
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)

	// If it won't fit, shave right-hand segments from least-important (front
	// of rightOrder) until it does, keeping the state badge.
	for leftW+rightW+1 > inner && len(rightOrder) > 1 {
		rightOrder = rightOrder[1:]
		right = strings.Join(rightOrder, "  ·  ")
		rightW = lipgloss.Width(right)
	}
	// Still too wide — truncate the state badge itself.
	if leftW+rightW+1 > inner {
		maxRight := inner - leftW - 1
		if maxRight < 4 {
			// Give up on the right side; just truncate left.
			return tuiHeaderBar.Width(m.width).Render(xansi.Truncate(left, inner, "…"))
		}
		right = xansi.Truncate(right, maxRight, "…")
		rightW = lipgloss.Width(right)
	}
	// Finally, if left+right+gap still doesn't fit, trim the left end too.
	if leftW+rightW+1 > inner {
		maxLeft := inner - rightW - 1
		left = xansi.Truncate(left, maxLeft, "…")
		leftW = lipgloss.Width(left)
	}

	gap := max(inner-leftW-rightW, 1)
	line := left + strings.Repeat(" ", gap) + right
	return tuiHeaderBar.Width(m.width).Render(line)
}

func (m tuiModel) renderHintBar() string {
	var parts []string
	parts = append(parts, "? help", "q quit")
	if len(m.stack) > 1 {
		parts = append(parts, "Esc back")
	}
	for _, h := range m.top().hints() {
		parts = append(parts, fmt.Sprintf("%s %s", h.keys, h.desc))
	}
	line := strings.Join(parts, "  ·  ")
	// Fit to m.width - 2 (for Padding(0,1)), then render with Width(m.width).
	line = truncDisplay(line, m.width-2)
	return tuiHintBar.Width(m.width).Render(line)
}

func (m tuiModel) renderHelp() string {
	lines := []string{
		tuiBold.Render("autoresearch TUI — help"),
		"",
		"Global:",
		"  ?         toggle this help",
		"  q / C-c   quit",
		"  Esc / ⌫   pop current view",
		"  D         back to dashboard",
		"",
		"Jump to view:",
		"  H hypotheses   E experiments   C conclusions",
		"  L event log    T tree          F frontier",
		"  O goals        S status         A artifacts",
		"  I instruments  R report picker  N notebook",
		"",
		"Within a list:",
		"  ↑/↓ or j/k    move cursor",
		"  Enter         open detail",
		"  f             cycle filter",
		"",
		"Within goal views:",
		"  s             scope the TUI to the selected goal",
		"  a             broaden scope to all goals",
		"",
		"Within hypothesis detail / list:",
		"  r             open markdown report",
		"",
		"Within the artifact viewer:",
		"  Tab           cycle head/tail/full",
		"  /             grep (regex)",
		"  d             diff against another SHA",
		"  g / G         top / bottom",
	}
	return tuiHelpBox.Render(strings.Join(lines, "\n"))
}
