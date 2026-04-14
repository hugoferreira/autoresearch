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

type conclusionListView struct {
	scope    goalScope
	all      []*entity.Conclusion
	filtered []*entity.Conclusion
	cursor   int
	verdict  string
	err      error
}

type concListLoadedMsg struct {
	list []*entity.Conclusion
	err  error
}

func newConclusionListView(scope goalScope) *conclusionListView {
	return &conclusionListView{scope: scope}
}

func (v *conclusionListView) title() string { return "Conclusions" }

func (v *conclusionListView) init(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		list, err := s.ListConclusions()
		if err == nil {
			list, err = newGoalScopeResolver(s, v.scope).filterConclusions(list)
		}
		return concListLoadedMsg{list: list, err: err}
	}
}

var conclVerdictFilters = []string{"", "supported", "refuted", "inconclusive"}

func (v *conclusionListView) applyFilter() {
	v.filtered = v.filtered[:0]
	for _, c := range v.all {
		if v.verdict != "" && c.Verdict != v.verdict {
			continue
		}
		v.filtered = append(v.filtered, c)
	}
	v.cursor = clampCursor(v.cursor, len(v.filtered))
}

func (v *conclusionListView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case concListLoadedMsg:
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
			v.verdict = nextStatusFilter(v.verdict, conclVerdictFilters)
			v.applyFilter()
		case "enter":
			if v.cursor >= 0 && v.cursor < len(v.filtered) {
				return v, tuiPush(newConclusionDetailView(v.filtered[v.cursor].ID))
			}
		}
	}
	return v, nil
}

func (v *conclusionListView) hints() []tuiHint {
	return []tuiHint{{"↑↓", "move"}, {"Enter", "open"}, {"f", "filter:" + filterLabel(v.verdict)}}
}

func (v *conclusionListView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if len(v.all) == 0 {
		return tuiDim.Render("(no conclusions)")
	}
	header := tuiBold.Render(fmt.Sprintf("%d conclusions  ·  filter=%s",
		len(v.filtered), filterLabel(v.verdict)))
	rows := make([]string, len(v.filtered))
	for i, c := range v.filtered {
		extras := ""
		if c.Strict.RequestedFrom != "" {
			extras = tuiDim.Render("  ↓from " + c.Strict.RequestedFrom)
		}
		review := " "
		if c.ReviewedBy != "" {
			review = tuiGreen.Render("✓")
		} else if c.Verdict == entity.VerdictSupported || c.Verdict == entity.VerdictRefuted {
			review = tuiYellow.Render("±")
		}
		rows[i] = fmt.Sprintf("%-8s  %s %s hyp=%-8s  Δfrac=%+.4f  p=%.4g%s",
			c.ID, padRight(tuiVerdictBadge(c.Verdict), 12), review,
			c.Hypothesis, c.Effect.DeltaFrac, c.Effect.PValue, extras)
	}
	return renderFilteredListBody(header, rows, v.cursor, width, height)
}

// ---- detail view ----

type conclusionDetailView struct {
	id      string
	c       *entity.Conclusion
	compact bool
	err     error
}

type concDetailLoadedMsg struct {
	c   *entity.Conclusion
	err error
}

func newConclusionDetailView(id string) *conclusionDetailView {
	return &conclusionDetailView{id: id}
}

func (v *conclusionDetailView) title() string { return "Conclusion " + v.id }

func (v *conclusionDetailView) init(s *store.Store) tea.Cmd {
	id := v.id
	return func() tea.Msg {
		c, err := s.ReadConclusion(id)
		return concDetailLoadedMsg{c: c, err: err}
	}
}

func (v *conclusionDetailView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case concDetailLoadedMsg:
		v.c = msg.c
		v.err = msg.err
		return v, nil
	case tuiTickMsg:
		return v, v.init(s)
	}
	return v, nil
}

func (v *conclusionDetailView) hints() []tuiHint { return nil }

func (v *conclusionDetailView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if v.c == nil {
		return tuiDim.Render("loading…")
	}
	c := v.c
	lines := []string{}
	lines = append(lines, tuiBold.Render(c.ID)+"  "+tuiVerdictBadge(c.Verdict))
	lines = append(lines, tuiDim.Render("hypothesis=")+c.Hypothesis+"  "+tuiDim.Render("author=")+c.Author)
	if c.ReviewedBy != "" {
		lines = append(lines, tuiDim.Render("reviewed_by=")+c.ReviewedBy)
	}
	if c.CandidateExp != "" {
		lines = append(lines, tuiDim.Render("candidate=")+c.CandidateExp+"  "+tuiDim.Render("baseline=")+emptyDash(c.BaselineExp))
	}
	if c.Strict.RequestedFrom != "" {
		lines = append(lines, "")
		lines = append(lines, tuiBoldYellow.Render("⚠ downgraded from "+c.Strict.RequestedFrom))
		for _, r := range c.Strict.Reasons {
			lines = append(lines, "  · "+r)
		}
	}
	lines = append(lines, "")
	lines = append(lines, tuiBold.Render("Effect:"))
	lines = append(lines, fmt.Sprintf("  instrument=%s  test=%s", c.Effect.Instrument, c.StatTest))
	lines = append(lines, fmt.Sprintf("  delta_abs=%+.6g  delta_frac=%+.4f",
		c.Effect.DeltaAbs, c.Effect.DeltaFrac))
	lines = append(lines, fmt.Sprintf("  abs_ci=[%.6g,%.6g]  frac_ci=[%+.4f,%+.4f]",
		c.Effect.CILowAbs, c.Effect.CIHighAbs, c.Effect.CILowFrac, c.Effect.CIHighFrac))
	lines = append(lines, fmt.Sprintf("  p_value=%.4g  method=%s  n=(cand=%d, base=%d)",
		c.Effect.PValue, c.Effect.CIMethod, c.Effect.NCandidate, c.Effect.NBaseline))
	lines = append(lines, "")
	lines = append(lines, tuiBold.Render(fmt.Sprintf("Observations (%d):", len(c.Observations))))
	for _, oid := range c.Observations {
		lines = append(lines, "  "+oid)
	}
	if !v.compact && c.Body != "" {
		lines = append(lines, "")
		lines = append(lines, tuiBold.Render("Interpretation:"))
		body := strings.TrimSpace(c.Body)
		lines = append(lines, strings.TrimRight(renderMarkdown(width, body), "\n"))
	}
	return clampLines(strings.Join(lines, "\n"), height, width)
}
