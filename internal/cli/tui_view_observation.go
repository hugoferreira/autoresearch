package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

type observationDetailView struct {
	id    string
	o     *entity.Observation
	err   error
	pager pagerState
}

type obsDetailLoadedMsg struct {
	o   *entity.Observation
	err error
}

func newObservationDetailView(id string) *observationDetailView {
	return &observationDetailView{id: id}
}

func (v *observationDetailView) title() string { return "Observation " + v.id }

func (v *observationDetailView) init(s *store.Store) tea.Cmd {
	id := v.id
	return func() tea.Msg {
		o, err := s.ReadObservation(id)
		return obsDetailLoadedMsg{o: o, err: err}
	}
}

func (v *observationDetailView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case obsDetailLoadedMsg:
		v.o = msg.o
		v.err = msg.err
		return v, nil
	case storeChangedMsg:
		return v, v.init(s)
	case tea.KeyMsg:
		return v, v.pager.handleKey(msg)
	case tea.MouseMsg:
		return v, v.pager.handleMouse(msg)
	}
	return v, nil
}

func (v *observationDetailView) hints() []tuiHint {
	return []tuiHint{{"g/G", "top/bot"}, {"↑↓/PgUp/PgDn", "scroll"}}
}

func (v *observationDetailView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if v.o == nil {
		return tuiDim.Render("loading…")
	}
	o := v.o
	lines := []string{}
	lines = append(lines, tuiBold.Render(o.ID)+"  "+tuiCyan.Render(o.Instrument)+"="+fmt.Sprintf("%.6g %s", o.Value, o.Unit))
	lines = append(lines, tuiDim.Render("experiment=")+o.Experiment+"  "+tuiDim.Render("author=")+o.Author)
	lines = append(lines, tuiDim.Render("measured_at=")+o.MeasuredAt.UTC().Format(time.RFC3339))
	lines = append(lines, "")

	kv := [][2]string{
		{"samples", fmt.Sprintf("%d", o.Samples)},
		{"exit", fmt.Sprintf("%d", o.ExitCode)},
	}
	if o.CILow != nil && o.CIHigh != nil {
		kv = append(kv, [2]string{"ci", fmt.Sprintf("[%.6g, %.6g] (%s)", *o.CILow, *o.CIHigh, emptyDash(o.CIMethod))})
	}
	if o.Pass != nil {
		pass := tuiRed.Render("fail")
		if *o.Pass {
			pass = tuiGreen.Render("pass")
		}
		kv = append(kv, [2]string{"pass", pass})
	}
	if o.Worktree != "" {
		kv = append(kv, [2]string{"worktree", o.Worktree})
	}
	if o.BaselineSHA != "" {
		kv = append(kv, [2]string{"baseline", shortSHA(o.BaselineSHA)})
	}
	lines = append(lines, renderKeyValueTable(kv, "  "))

	if len(o.Artifacts) > 0 {
		lines = append(lines, "")
		lines = append(lines, tuiBold.Render(fmt.Sprintf("Artifacts (%d):", len(o.Artifacts))))
		rows := [][]string{{"name", "sha", "bytes", "path"}}
		for _, a := range o.Artifacts {
			rows = append(rows, []string{
				a.Name,
				shortSHA(a.SHA),
				humanBytes(a.Bytes),
				a.Path,
			})
		}
		lines = append(lines, renderTable(rows, "  "))
	}

	if len(o.EvidenceFailures) > 0 {
		lines = append(lines, "")
		lines = append(lines, tuiBold.Render("Evidence failures:"))
		for _, failure := range o.EvidenceFailures {
			lines = append(lines, "  - "+formatEvidenceFailure(failure))
		}
	}

	if len(o.PerSample) > 0 {
		lines = append(lines, "")
		lines = append(lines, tuiBold.Render("Per-sample:"))
		lines = append(lines, "  "+fmt.Sprintf("%v", o.PerSample))
	}

	if strings.TrimSpace(o.Command) != "" {
		lines = append(lines, "")
		lines = append(lines, tuiBold.Render("Command:"))
		lines = append(lines, wrap(o.Command, max(width-2, 1)))
	}

	if len(o.Aux) > 0 {
		lines = append(lines, "")
		lines = append(lines, tuiBold.Render("Aux:"))
		if raw, err := json.Marshal(o.Aux); err == nil {
			lines = append(lines, prettyJSON(raw, "  "))
		}
	}

	v.pager.ensureSize(width, height)
	v.pager.setContent(strings.Join(lines, "\n"))
	return v.pager.view()
}
