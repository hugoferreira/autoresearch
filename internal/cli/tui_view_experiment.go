package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/stats"
	"github.com/bytter/autoresearch/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// ---- list view ----

type experimentListView struct {
	all      []*entity.Experiment
	filtered []*entity.Experiment
	cursor   int
	statusFilter string
	err      error
}

type expListLoadedMsg struct {
	list []*entity.Experiment
	err  error
}

func newExperimentListView() *experimentListView { return &experimentListView{} }

func (v *experimentListView) title() string { return "Experiments" }

func (v *experimentListView) init(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		list, err := s.ListExperiments()
		return expListLoadedMsg{list: list, err: err}
	}
}

var expStatusFilters = []string{"", "designed", "implemented", "measured", "analyzed", "failed"}

func (v *experimentListView) applyFilter() {
	v.filtered = v.filtered[:0]
	for _, e := range v.all {
		if v.statusFilter != "" && e.Status != v.statusFilter {
			continue
		}
		v.filtered = append(v.filtered, e)
	}
	v.cursor = clampCursor(v.cursor, len(v.filtered))
}

func (v *experimentListView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case expListLoadedMsg:
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
			v.statusFilter = nextStatusFilter(v.statusFilter, expStatusFilters)
			v.applyFilter()
		case "enter":
			if v.cursor >= 0 && v.cursor < len(v.filtered) {
				return v, tuiPush(newExperimentDetailView(v.filtered[v.cursor].ID))
			}
		}
	}
	return v, nil
}

func (v *experimentListView) hints() []tuiHint {
	return []tuiHint{{"↑↓", "move"}, {"Enter", "open"}, {"f", "filter:" + filterLabel(v.statusFilter)}}
}

func (v *experimentListView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if len(v.all) == 0 {
		return tuiDim.Render("(no experiments)")
	}
	header := tuiBold.Render(fmt.Sprintf("%d experiments  ·  filter=%s",
		len(v.filtered), filterLabel(v.statusFilter)))
	rows := make([]string, len(v.filtered))
	for i, e := range v.filtered {
		rows[i] = fmt.Sprintf("%-8s  %s  %-8s  hyp=%-8s  inst=%s",
			e.ID, padRight(tuiExpStatusBadge(e.Status), 12),
			e.Tier, e.Hypothesis, strings.Join(e.Instruments, ","))
	}
	return renderFilteredListBody(header, rows, v.cursor, width, height)
}

// ---- detail view ----

type experimentDetailView struct {
	id      string
	e       *entity.Experiment
	obs     []*entity.Observation
	summ    map[string]stats.Summary
	compact bool
	err     error
}

type expDetailLoadedMsg struct {
	e    *entity.Experiment
	obs  []*entity.Observation
	summ map[string]stats.Summary
	err  error
}

func newExperimentDetailView(id string) *experimentDetailView {
	return &experimentDetailView{id: id}
}

func newExperimentDetailCompact(id string) *experimentDetailView {
	return &experimentDetailView{id: id, compact: true}
}

func (v *experimentDetailView) title() string { return "Experiment " + v.id }

func (v *experimentDetailView) init(s *store.Store) tea.Cmd {
	id := v.id
	return func() tea.Msg {
		e, err := s.ReadExperiment(id)
		if err != nil {
			return expDetailLoadedMsg{err: err}
		}
		obs, _ := s.ListObservationsForExperiment(id)
		// Bucket samples by instrument and summarize with BCa CI. Mirrors
		// what `autoresearch analyze` computes on the CLI.
		summ := map[string]stats.Summary{}
		for name, group := range groupByInstrument(obs) {
			xs := flattenSamples(group)
			if len(xs) > 0 {
				summ[name] = stats.Summarize(xs, 0, 0)
			}
		}
		return expDetailLoadedMsg{e: e, obs: obs, summ: summ}
	}
}

func (v *experimentDetailView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case expDetailLoadedMsg:
		v.e = msg.e
		v.obs = msg.obs
		v.summ = msg.summ
		v.err = msg.err
		return v, nil
	case tuiTickMsg:
		return v, v.init(s)
	}
	return v, nil
}

func (v *experimentDetailView) hints() []tuiHint { return nil }

func (v *experimentDetailView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if v.e == nil {
		return tuiDim.Render("loading…")
	}
	e := v.e
	lines := []string{}
	// Title line: bold ID, status badge, tier.
	lines = append(lines, tuiBold.Render(e.ID)+"  "+tuiExpStatusBadge(e.Status)+"  "+tuiDim.Render("tier="+e.Tier))
	lines = append(lines, "")

	// Aligned key/value table — keys right-padded to the longest key.
	kv := [][2]string{
		{"hypothesis", e.Hypothesis},
		{"author", e.Author},
	}
	if e.Worktree != "" {
		kv = append(kv, [2]string{"worktree", e.Worktree})
	}
	if e.Branch != "" {
		kv = append(kv, [2]string{"branch", e.Branch})
	}
	kv = append(kv, [2]string{"baseline", fmt.Sprintf("%s (%s)", emptyDash(e.Baseline.Ref), shortSHA(e.Baseline.SHA))})
	kv = append(kv, [2]string{"instruments", strings.Join(e.Instruments, ", ")})
	if e.Budget.WallTimeS > 0 || e.Budget.MaxSamples > 0 {
		kv = append(kv, [2]string{"budget", fmt.Sprintf("wall=%ds  max_samples=%d", e.Budget.WallTimeS, e.Budget.MaxSamples)})
	}
	lines = append(lines, renderKeyValueTable(kv, "  "))

	lines = append(lines, "")
	lines = append(lines, tuiBold.Render(fmt.Sprintf("Observations (%d):", len(v.obs))))
	if len(v.obs) == 0 {
		lines = append(lines, "  "+tuiDim.Render("(none)"))
	} else {
		max := len(v.obs)
		if v.compact && max > 6 {
			max = 6
		}
		// Aligned observation table: id, instrument=value unit, n, ci, pass.
		rows := make([][]string, 0, max)
		rows = append(rows, []string{"id", "measurement", "n", "ci", "pass"})
		for i := 0; i < max; i++ {
			o := v.obs[i]
			meas := fmt.Sprintf("%s=%.6g %s", o.Instrument, o.Value, o.Unit)
			ci := ""
			if o.CILow != nil && o.CIHigh != nil {
				ci = fmt.Sprintf("[%.6g, %.6g]", *o.CILow, *o.CIHigh)
			}
			pass := ""
			if o.Pass != nil {
				if *o.Pass {
					pass = tuiGreen.Render("pass")
				} else {
					pass = tuiRed.Render("fail")
				}
			}
			rows = append(rows, []string{o.ID, meas, fmt.Sprintf("%d", o.Samples), ci, pass})
		}
		lines = append(lines, renderTable(rows, "  "))
		if !v.compact {
			// Per-sample + command lines for each shown observation (dim,
			// indented further so they don't interfere with the table).
			for i := 0; i < max; i++ {
				o := v.obs[i]
				if len(o.PerSample) > 0 {
					lines = append(lines, "      "+tuiDim.Render(fmt.Sprintf("%s per_sample=%v", o.ID, o.PerSample)))
				}
				if o.Command != "" {
					lines = append(lines, "      "+tuiDim.Render(o.ID+" cmd="+truncate(o.Command, width-14)))
				}
			}
		}
		if max < len(v.obs) {
			lines = append(lines, tuiDim.Render(fmt.Sprintf("  … %d more", len(v.obs)-max)))
		}
	}

	if len(v.summ) > 0 && !v.compact {
		lines = append(lines, "")
		lines = append(lines, tuiBold.Render("Summary (per instrument):"))
		keys := make([]string, 0, len(v.summ))
		for k := range v.summ {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		srows := make([][]string, 0, len(keys)+1)
		srows = append(srows, []string{"instrument", "n", "mean", "ci", "stddev", "min", "max", "method"})
		for _, k := range keys {
			s := v.summ[k]
			srows = append(srows, []string{
				k,
				fmt.Sprintf("%d", s.N),
				fmt.Sprintf("%.6g", s.Mean),
				fmt.Sprintf("[%.6g, %.6g]", s.CILow, s.CIHigh),
				fmt.Sprintf("%.4g", s.StdDev),
				fmt.Sprintf("%.6g", s.Min),
				fmt.Sprintf("%.6g", s.Max),
				s.CIMethod,
			})
		}
		lines = append(lines, renderTable(srows, "  "))
	}
	return clampLines(strings.Join(lines, "\n"), height, width)
}


