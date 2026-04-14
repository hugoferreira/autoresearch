package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// ---- list view ----

type lessonListView struct {
	all      []*entity.Lesson
	filtered []*entity.Lesson
	cursor   int
	scope    string // "" means all
	status   string // "" means all
	err      error
}

type lessonListLoadedMsg struct {
	list []*entity.Lesson
	err  error
}

func newLessonListView() *lessonListView { return &lessonListView{} }

func (v *lessonListView) title() string { return "Lessons" }

func (v *lessonListView) init(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		list, err := s.ListLessons()
		if err == nil {
			views := make([]*entity.Lesson, 0, len(list))
			for _, l := range list {
				view, viewErr := annotateLessonForRead(s, l)
				if viewErr != nil {
					return lessonListLoadedMsg{err: viewErr}
				}
				views = append(views, view)
			}
			list = views
		}
		return lessonListLoadedMsg{list: list, err: err}
	}
}

var lessonScopeFilters = []string{"", entity.LessonScopeHypothesis, entity.LessonScopeSystem}
var lessonStatusFilters = []string{"", entity.LessonStatusActive, entity.LessonStatusProvisional, entity.LessonStatusInvalidated, entity.LessonStatusSuperseded}

func (v *lessonListView) applyFilter() {
	v.filtered = v.filtered[:0]
	for _, l := range v.all {
		if v.scope != "" && l.Scope != v.scope {
			continue
		}
		if v.status != "" && l.Status != v.status {
			continue
		}
		v.filtered = append(v.filtered, l)
	}
	v.cursor = clampCursor(v.cursor, len(v.filtered))
}

func (v *lessonListView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case lessonListLoadedMsg:
		v.all = msg.list
		v.err = msg.err
		sort.Slice(v.all, func(i, j int) bool { return v.all[i].ID < v.all[j].ID })
		v.applyFilter()
		return v, nil
	case tuiTickMsg:
		return v, v.init(s)
	case tea.KeyMsg:
		if handleListNav(msg, &v.cursor, len(v.filtered)) {
			return v, nil
		}
		switch msg.String() {
		case "f":
			v.scope = nextStatusFilter(v.scope, lessonScopeFilters)
			v.applyFilter()
		case "s":
			v.status = nextStatusFilter(v.status, lessonStatusFilters)
			v.applyFilter()
		case "enter":
			if v.cursor >= 0 && v.cursor < len(v.filtered) {
				return v, tuiPush(newLessonDetailView(v.filtered[v.cursor].ID))
			}
		}
	}
	return v, nil
}

func (v *lessonListView) hints() []tuiHint {
	return []tuiHint{
		{"↑↓", "move"},
		{"Enter", "open"},
		{"f", "scope:" + filterLabel(v.scope)},
		{"s", "status:" + filterLabel(v.status)},
	}
}

func (v *lessonListView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if len(v.all) == 0 {
		return tuiDim.Render("(no lessons yet — the analyst writes them on decisive conclusions)")
	}
	header := tuiBold.Render(fmt.Sprintf("%d lessons  ·  scope=%s  ·  status=%s",
		len(v.filtered), filterLabel(v.scope), filterLabel(v.status)))
	rows := make([]string, len(v.filtered))
	for i, l := range v.filtered {
		subj := "-"
		if len(l.Subjects) > 0 {
			subj = strings.Join(l.Subjects, ",")
		}
		pred := tuiDim.Render("     ")
		if l.PredictedEffect != nil {
			pred = padRight(formatPredictedEffectCompact(l.PredictedEffect), 5)
		}
		source := "-"
		if l.Provenance != nil && l.Provenance.SourceChain != "" {
			source = l.Provenance.SourceChain
		}
		rows[i] = fmt.Sprintf("%-8s %-11s %-12s %-20s %s from=%-18s %s",
			l.ID,
			padRight(tuiLessonScopeBadge(l.Scope), 11),
			padRight(tuiLessonStatusBadge(l.Status), 12),
			padRight(source, 20),
			pred,
			truncate(subj, 18),
			truncate(l.Claim, max(width-87, 10)),
		)
	}
	return renderFilteredListBody(header, rows, v.cursor, width, height)
}

// ---- detail view ----

type lessonDetailView struct {
	id            string
	l             *entity.Lesson
	err           error
	rendered      string
	renderedWidth int
	pager         pagerState
}

type lessonDetailLoadedMsg struct {
	l   *entity.Lesson
	err error
}

func newLessonDetailView(id string) *lessonDetailView {
	return &lessonDetailView{id: id, renderedWidth: -1}
}

func (v *lessonDetailView) title() string { return "Lesson " + v.id }

func (v *lessonDetailView) init(s *store.Store) tea.Cmd {
	id := v.id
	return func() tea.Msg {
		l, err := s.ReadLesson(id)
		if err == nil {
			l, err = annotateLessonForRead(s, l)
		}
		return lessonDetailLoadedMsg{l: l, err: err}
	}
}

func (v *lessonDetailView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case lessonDetailLoadedMsg:
		v.l = msg.l
		v.err = msg.err
		v.renderedWidth = -1
		if v.pager.ready {
			v.pager.setContent(v.ensureRendered(v.pager.vp.Width))
			v.pager.gotoTop()
		}
		return v, nil
	case tuiTickMsg:
		return v, v.init(s)
	case tea.KeyMsg:
		return v, v.pager.handleKey(msg)
	case tea.MouseMsg:
		return v, v.pager.handleMouse(msg)
	}
	return v, nil
}

func (v *lessonDetailView) hints() []tuiHint {
	return []tuiHint{{"g/G", "top/bot"}, {"↑↓/PgUp/PgDn", "scroll"}}
}

func (v *lessonDetailView) ensureRendered(width int) string {
	if width <= 0 {
		width = 80
	}
	if v.rendered != "" && v.renderedWidth == width {
		return v.rendered
	}
	l := v.l
	lines := []string{}
	lines = append(lines, tuiBold.Render(l.ID)+"  "+tuiLessonScopeBadge(l.Scope)+"  "+tuiLessonStatusBadge(l.Status))
	lines = append(lines, tuiDim.Render("author=")+l.Author)
	if l.Provenance != nil && l.Provenance.SourceChain != "" {
		lines = append(lines, tuiDim.Render("source_chain=")+l.Provenance.SourceChain)
	}
	lines = append(lines, "")
	lines = append(lines, tuiBold.Render("Claim:"))
	lines = append(lines, wrap(l.Claim, max(width-2, 1)))
	if len(l.Subjects) > 0 {
		lines = append(lines, "")
		lines = append(lines, tuiBold.Render("From:"))
		for _, sub := range l.Subjects {
			lines = append(lines, "  · "+sub)
		}
	}
	if len(l.Tags) > 0 {
		lines = append(lines, "")
		lines = append(lines, tuiBold.Render("Tags:")+" "+strings.Join(l.Tags, ", "))
	}
	if l.PredictedEffect != nil {
		lines = append(lines, "")
		lines = append(lines, tuiBold.Render("Predicted effect:")+" "+tuiYellow.Render(formatPredictedEffect(l.PredictedEffect)))
	}
	if l.SupersedesID != "" || l.SupersededByID != "" {
		lines = append(lines, "")
		if l.SupersedesID != "" {
			lines = append(lines, tuiDim.Render("supersedes: ")+l.SupersedesID)
		}
		if l.SupersededByID != "" {
			lines = append(lines, tuiBoldYellow.Render("superseded by: ")+l.SupersededByID)
		}
	}
	if body := strings.TrimSpace(l.Body); body != "" {
		lines = append(lines, "")
		lines = append(lines, strings.TrimRight(renderMarkdown(width, body), "\n"))
	}
	v.rendered = strings.Join(lines, "\n")
	v.renderedWidth = width
	return v.rendered
}

func (v *lessonDetailView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if v.l == nil {
		return tuiDim.Render("loading…")
	}
	v.pager.ensureSize(width, height)
	v.pager.setContent(v.ensureRendered(width))
	return v.pager.view()
}

// ---- badges ----

func tuiLessonScopeBadge(scope string) string {
	switch scope {
	case entity.LessonScopeSystem:
		return tuiMag.Render("system")
	case entity.LessonScopeHypothesis:
		return tuiCyan.Render("hypothesis")
	default:
		return scope
	}
}

func tuiLessonStatusBadge(status string) string {
	switch status {
	case entity.LessonStatusActive:
		return tuiGreen.Render("active")
	case entity.LessonStatusProvisional:
		return tuiYellow.Render("provisional")
	case entity.LessonStatusInvalidated:
		return tuiRed.Render("invalidated")
	case entity.LessonStatusSuperseded:
		return tuiDim.Render("superseded")
	default:
		return status
	}
}
