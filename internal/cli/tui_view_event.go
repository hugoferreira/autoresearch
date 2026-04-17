package cli

import (
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// ---- list view ----

type eventListView struct {
	scope      goalScope
	all        []store.Event
	filtered   []store.Event
	cursor     int
	kindFilter string // filter token ("" = all)
	follow     bool
	err        error

	// lastOffset is the byte offset into events.jsonl up to which we've
	// already streamed events into `all`. Each tick pulls only the new
	// tail via Store.EventsSince — see the Event log rule in CLAUDE.md.
	lastOffset int64
	// eventCap bounds `all` so a very-long-running session doesn't grow
	// unboundedly. Oldest-first eviction.
	eventCap int
}

type eventListLoadedMsg struct {
	events []store.Event
	newOff int64
	// replace is true for the initial load (no prior state); false for
	// incremental updates appended to `all`.
	replace bool
	err     error
}

const defaultEventListCap = 2000

func newEventListView(scope goalScope) *eventListView {
	return &eventListView{scope: scope, follow: true, eventCap: defaultEventListCap}
}

func (v *eventListView) title() string { return "Event log" }

func (v *eventListView) init(s *store.Store) tea.Cmd {
	off := v.lastOffset
	scope := v.scope
	replace := off == 0
	return func() tea.Msg {
		events, newOff, err := s.EventsSince(off)
		if err == nil && events != nil {
			// Advisory cache invalidation — see CLAUDE.md Event payload
			// rule. Entity reads will pick up these subjects on next miss.
			s.InvalidateFromEvents(events)
			events, err = newGoalScopeResolver(s, scope).filterEvents(events)
		}
		return eventListLoadedMsg{events: events, newOff: newOff, replace: replace, err: err}
	}
}

var eventKindFilters = []string{"", "hypothesis.", "experiment.", "observation.", "conclusion.", "pause", "resume", "init"}

func (v *eventListView) applyFilter() {
	v.filtered = v.filtered[:0]
	for _, e := range v.all {
		if v.kindFilter != "" {
			if strings.HasSuffix(v.kindFilter, ".") {
				if !strings.HasPrefix(e.Kind, v.kindFilter) {
					continue
				}
			} else if e.Kind != v.kindFilter {
				continue
			}
		}
		v.filtered = append(v.filtered, e)
	}
	// Follow mode pins the cursor to the tail; otherwise just clamp.
	if v.follow && len(v.filtered) > 0 {
		v.cursor = len(v.filtered) - 1
		return
	}
	v.cursor = clampCursor(v.cursor, len(v.filtered))
}

func (v *eventListView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case eventListLoadedMsg:
		v.err = msg.err
		v.lastOffset = msg.newOff
		if msg.replace {
			v.all = msg.events
		} else if len(msg.events) > 0 {
			v.all = append(v.all, msg.events...)
			if v.eventCap > 0 && len(v.all) > v.eventCap {
				v.all = v.all[len(v.all)-v.eventCap:]
			}
		}
		v.applyFilter()
		return v, nil
	case tuiTickMsg:
		return v, v.init(s)
	case tea.KeyMsg:
		// Event list's cursor interacts with follow-mode, so it handles
		// up/down itself rather than delegating to handleListNav.
		switch msg.String() {
		case "up", "k":
			if v.cursor > 0 {
				v.cursor--
				v.follow = false
			}
		case "down", "j":
			moveCursor(&v.cursor, 1, len(v.filtered))
			if v.cursor == len(v.filtered)-1 {
				v.follow = true
			}
		case "g":
			v.cursor = 0
			v.follow = false
		case "G":
			v.cursor = clampCursor(len(v.filtered)-1, len(v.filtered))
			v.follow = true
		case "W":
			v.follow = !v.follow
		case "f":
			v.kindFilter = nextStatusFilter(v.kindFilter, eventKindFilters)
			v.applyFilter()
		case "enter":
			if v.cursor >= 0 && v.cursor < len(v.filtered) {
				return v, tuiPush(newEventDetailView(v.filtered[v.cursor]))
			}
		}
	}
	return v, nil
}

func (v *eventListView) hints() []tuiHint {
	follow := "off"
	if v.follow {
		follow = "on"
	}
	return []tuiHint{
		{"↑↓", "move"}, {"Enter", "open"},
		{"f", "kind:" + filterLabel(v.kindFilter)},
		{"W", "follow:" + follow},
		{"g/G", "top/bottom"},
	}
}

func (v *eventListView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	header := tuiBold.Render(fmt.Sprintf("%d events  ·  filter=%s  ·  follow=%v",
		len(v.filtered), filterLabel(v.kindFilter), v.follow))
	if len(v.filtered) == 0 {
		return header + "\n\n" + tuiDim.Render("(no events)")
	}
	rows := make([]string, len(v.filtered))
	for i, e := range v.filtered {
		rows[i] = fmt.Sprintf("%s  %s  %-8s  %s",
			tuiDim.Render(e.Ts.UTC().Format("15:04:05")),
			padRight(tuiEventKindColor(e.Kind), 24),
			emptyDash(e.Subject),
			tuiDim.Render(e.Actor),
		)
	}
	return renderFilteredListBody(header, rows, v.cursor, width, height)
}

// ---- detail view ----

type eventDetailView struct {
	e       store.Event
	compact bool
}

func newEventDetailView(e store.Event) *eventDetailView {
	return &eventDetailView{e: e}
}

func (v *eventDetailView) title() string { return "Event " + v.e.Kind }

func (v *eventDetailView) init(_ *store.Store) tea.Cmd                         { return nil }
func (v *eventDetailView) update(_ tea.Msg, _ *store.Store) (tuiView, tea.Cmd) { return v, nil }
func (v *eventDetailView) hints() []tuiHint                                    { return nil }

func (v *eventDetailView) view(width, height int) string {
	lines := []string{}
	lines = append(lines, tuiBold.Render(v.e.Kind))
	lines = append(lines, tuiDim.Render("ts=")+v.e.Ts.UTC().Format("2006-01-02 15:04:05 MST"))
	lines = append(lines, tuiDim.Render("actor=")+emptyDash(v.e.Actor))
	lines = append(lines, tuiDim.Render("subject=")+emptyDash(v.e.Subject))
	lines = append(lines, tuiDim.Render("──────────"))
	lines = append(lines, tuiBold.Render("Payload:"))
	if len(v.e.Data) == 0 {
		lines = append(lines, tuiDim.Render("(no payload)"))
	} else {
		lines = append(lines, prettyJSON(v.e.Data, ""))
	}
	return clampLines(strings.Join(lines, "\n"), height, width)
}
