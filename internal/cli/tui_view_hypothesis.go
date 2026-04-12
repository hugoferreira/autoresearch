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

type hypothesisListView struct {
	all          []*entity.Hypothesis
	filtered     []*entity.Hypothesis
	cursor       int
	statusFilter string // "" means all
	reportMode   bool   // if true, Enter opens the report view
	err          error
}

type hypListLoadedMsg struct {
	list []*entity.Hypothesis
	err  error
}

func newHypothesisListView() *hypothesisListView { return &hypothesisListView{} }

// newHypothesisListViewForReport is the hypothesis list whose Enter key
// opens the report view instead of the detail view. Used when the user hits
// the top-level `R` shortcut.
func newHypothesisListViewForReport() *hypothesisListView {
	return &hypothesisListView{reportMode: true}
}

func (v *hypothesisListView) title() string {
	if v.reportMode {
		return "Report · pick hypothesis"
	}
	return "Hypotheses"
}

func (v *hypothesisListView) init(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		list, err := s.ListHypotheses()
		return hypListLoadedMsg{list: list, err: err}
	}
}

func (v *hypothesisListView) applyFilter() {
	v.filtered = v.filtered[:0]
	for _, h := range v.all {
		if v.statusFilter != "" && h.Status != v.statusFilter {
			continue
		}
		v.filtered = append(v.filtered, h)
	}
	v.cursor = clampCursor(v.cursor, len(v.filtered))
}

var hypStatusFilters = []string{"", "open", "supported", "refuted", "inconclusive", "killed"}

func (v *hypothesisListView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case hypListLoadedMsg:
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
			v.statusFilter = nextStatusFilter(v.statusFilter, hypStatusFilters)
			v.applyFilter()
		case "enter":
			if v.cursor >= 0 && v.cursor < len(v.filtered) {
				id := v.filtered[v.cursor].ID
				if v.reportMode {
					return v, tuiPush(newReportView(id))
				}
				return v, tuiPush(newHypothesisDetailView(id))
			}
		case "r":
			if v.cursor >= 0 && v.cursor < len(v.filtered) {
				return v, tuiPush(newReportView(v.filtered[v.cursor].ID))
			}
		}
	}
	return v, nil
}

func (v *hypothesisListView) hints() []tuiHint {
	open := "open"
	if v.reportMode {
		open = "report"
	}
	return []tuiHint{
		{"↑↓", "move"}, {"Enter", open},
		{"r", "report"},
		{"f", "filter:" + filterLabel(v.statusFilter)},
	}
}

func (v *hypothesisListView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if len(v.all) == 0 {
		return tuiDim.Render("(no hypotheses)")
	}
	header := tuiBold.Render(fmt.Sprintf("%d hypotheses  ·  filter=%s",
		len(v.filtered), filterLabel(v.statusFilter)))
	rows := make([]string, len(v.filtered))
	for i, h := range v.filtered {
		rows[i] = fmt.Sprintf("%s %-8s %-12s  %s",
			tuiStatusGlyph(h.Status), h.ID, h.Status, truncate(h.Claim, width-25))
	}
	return renderFilteredListBody(header, rows, v.cursor, width, height)
}

// ---- detail view ----

type hypothesisDetailView struct {
	id         string
	h          *entity.Hypothesis
	exps       []*entity.Experiment
	concls     []*entity.Conclusion
	obs        []*entity.Observation
	linkCursor int
	compact    bool
	err        error
}

type hypDetailLoadedMsg struct {
	h      *entity.Hypothesis
	exps   []*entity.Experiment
	concls []*entity.Conclusion
	obs    []*entity.Observation
	err    error
}

func newHypothesisDetailView(id string) *hypothesisDetailView {
	return &hypothesisDetailView{id: id}
}

func (v *hypothesisDetailView) title() string { return "Hypothesis " + v.id }

func (v *hypothesisDetailView) init(s *store.Store) tea.Cmd {
	id := v.id
	return func() tea.Msg {
		h, err := s.ReadHypothesis(id)
		if err != nil {
			return hypDetailLoadedMsg{err: err}
		}
		exps, _ := s.ListExperimentsForHypothesis(id)
		concls, _ := s.ListConclusionsForHypothesis(id)
		var allObs []*entity.Observation
		for _, e := range exps {
			obs, _ := s.ListObservationsForExperiment(e.ID)
			allObs = append(allObs, obs...)
		}
		return hypDetailLoadedMsg{h: h, exps: exps, concls: concls, obs: allObs}
	}
}

func (v *hypothesisDetailView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case hypDetailLoadedMsg:
		v.h = msg.h
		v.exps = msg.exps
		v.concls = msg.concls
		v.obs = msg.obs
		v.err = msg.err
		return v, nil
	case tuiTickMsg:
		return v, v.init(s)
	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			if v.h != nil {
				return v, tuiPush(newReportView(v.h.ID))
			}
		case "up", "k":
			if !v.compact {
				moveCursor(&v.linkCursor, -1, len(v.detailLinks()))
			}
		case "down", "j":
			if !v.compact {
				moveCursor(&v.linkCursor, 1, len(v.detailLinks()))
			}
		case "g":
			if !v.compact && len(v.detailLinks()) > 0 {
				v.linkCursor = 0
			}
		case "G":
			if !v.compact && len(v.detailLinks()) > 0 {
				v.linkCursor = len(v.detailLinks()) - 1
			}
		case "enter":
			if !v.compact {
				return v, v.openSelectedLink()
			}
		}
	}
	return v, nil
}

func (v *hypothesisDetailView) hints() []tuiHint {
	if v.compact {
		return []tuiHint{{"r", "report"}}
	}
	return []tuiHint{{"↑↓", "link"}, {"Enter", "open"}, {"g/G", "top/bot"}, {"r", "report"}}
}

func (v *hypothesisDetailView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if v.h == nil {
		return tuiDim.Render("loading…")
	}
	lines, links := v.renderLines(width)
	if !v.compact && len(links) > 0 {
		v.linkCursor = clampCursor(v.linkCursor, len(links))
		lines = truncLines(lines, width)
		lines = highlightRow(lines, links[v.linkCursor].line, width)
		lines = scrollWindow(lines, links[v.linkCursor].line, height)
		return strings.Join(lines, "\n")
	}
	return clampLines(strings.Join(lines, "\n"), height, width)
}

type hypDetailLink struct {
	kind string
	id   string
	line int
}

const (
	hypDetailLinkExperiment  = "experiment"
	hypDetailLinkConclusion  = "conclusion"
	hypDetailLinkObservation = "observation"
)

func (v *hypothesisDetailView) detailLinks() []hypDetailLink {
	_, links := v.renderLines(100)
	return links
}

func (v *hypothesisDetailView) openSelectedLink() tea.Cmd {
	links := v.detailLinks()
	if len(links) == 0 {
		return nil
	}
	link := links[clampCursor(v.linkCursor, len(links))]
	switch link.kind {
	case hypDetailLinkExperiment:
		return tuiPush(newExperimentDetailView(link.id))
	case hypDetailLinkConclusion:
		return tuiPush(newConclusionDetailView(link.id))
	case hypDetailLinkObservation:
		return tuiPush(newObservationDetailView(link.id))
	}
	return nil
}

func (v *hypothesisDetailView) renderLines(width int) ([]string, []hypDetailLink) {
	h := v.h
	lines := []string{}
	links := []hypDetailLink{}
	lines = append(lines, tuiBold.Render(h.ID)+"  "+tuiStatusGlyph(h.Status)+"  "+h.Status)
	lines = append(lines, tuiDim.Render("author=")+h.Author+"  "+tuiDim.Render("parent=")+emptyDash(h.Parent))
	lines = append(lines, "")
	lines = append(lines, tuiBold.Render("Claim:"))
	lines = append(lines, wrap(h.Claim, width-2))
	lines = append(lines, "")
	lines = append(lines, tuiBold.Render("Predicts:"))
	lines = append(lines, fmt.Sprintf("  %s %s on %s  (min_effect=%g)",
		h.Predicts.Direction, h.Predicts.Instrument, h.Predicts.Target, h.Predicts.MinEffect))
	if len(h.KillIf) > 0 {
		lines = append(lines, "")
		lines = append(lines, tuiBold.Render("Kill if:"))
		for _, k := range h.KillIf {
			lines = append(lines, "  · "+k)
		}
	}
	lines = append(lines, "")
	lines = append(lines, tuiBold.Render(fmt.Sprintf("Experiments (%d):", len(v.exps))))
	if len(v.exps) == 0 {
		lines = append(lines, "  "+tuiDim.Render("(none)"))
	} else {
		for _, e := range v.exps {
			lines = append(lines, fmt.Sprintf("  %s  %s  inst=%s",
				e.ID, tuiExpStatusBadge(e.Status), strings.Join(e.Instruments, ",")))
			links = append(links, hypDetailLink{kind: hypDetailLinkExperiment, id: e.ID, line: len(lines) - 1})
		}
	}
	lines = append(lines, "")
	lines = append(lines, tuiBold.Render(fmt.Sprintf("Conclusions (%d):", len(v.concls))))
	if len(v.concls) == 0 {
		lines = append(lines, "  "+tuiDim.Render("(none)"))
	} else {
		for _, c := range v.concls {
			extras := ""
			if c.Strict.RequestedFrom != "" {
				extras = tuiDim.Render("  (downgraded from " + c.Strict.RequestedFrom + ")")
			}
			lines = append(lines, fmt.Sprintf("  %s  %s  delta_frac=%.4f  p=%.4g%s",
				c.ID, tuiVerdictBadge(c.Verdict), c.Effect.DeltaFrac, c.Effect.PValue, extras))
			links = append(links, hypDetailLink{kind: hypDetailLinkConclusion, id: c.ID, line: len(lines) - 1})
		}
	}
	lines = append(lines, "")
	lines = append(lines, tuiBold.Render(fmt.Sprintf("Observations (%d):", len(v.obs))))
	if len(v.obs) == 0 {
		lines = append(lines, "  "+tuiDim.Render("(none)"))
	} else {
		rows := [][]string{{"id", "instrument", "value", "n", "ci", "pass", "exp"}}
		for _, o := range v.obs {
			ci := ""
			if o.CILow != nil && o.CIHigh != nil {
				ci = fmt.Sprintf("[%s, %s]", fmtValue(*o.CILow, o.Unit), fmtValue(*o.CIHigh, o.Unit))
			}
			pass := ""
			if o.Pass != nil {
				if *o.Pass {
					pass = "pass"
				} else {
					pass = "fail"
				}
			}
			rows = append(rows, []string{
				o.ID, o.Instrument,
				fmtValue(o.Value, o.Unit),
				fmt.Sprintf("%d", o.Samples),
				ci, pass, o.Experiment,
			})
		}
		tableLines := strings.Split(renderTable(rows, "  "), "\n")
		for i, tl := range tableLines {
			lines = append(lines, tl)
			if i > 0 { // skip header row for links
				obsIdx := i - 1
				if obsIdx < len(v.obs) {
					links = append(links, hypDetailLink{kind: hypDetailLinkObservation, id: v.obs[obsIdx].ID, line: len(lines) - 1})
				}
			}
		}
	}
	if !v.compact && strings.TrimSpace(h.Body) != "" {
		lines = append(lines, "")
		lines = append(lines, strings.TrimRight(renderMarkdown(width, h.Body), "\n"))
	}
	return lines, links
}
