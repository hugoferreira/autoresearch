package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	tea "github.com/charmbracelet/bubbletea"
)

func TestTUI_GoalListViewShowsSessionScope(t *testing.T) {
	v := newGoalListView(goalScope{GoalID: "G-0002"})
	goals := []*entity.Goal{
		{ID: "G-0001", Status: entity.GoalStatusConcluded, Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"}},
		{ID: "G-0002", Status: entity.GoalStatusActive, Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"}},
	}
	nv, _ := v.update(goalListLoadedMsg{all: goals, current: "G-0002"}, nil)
	gv := nv.(*goalListView)
	if got := gv.cursor; got != 1 {
		t.Fatalf("goal list cursor = %d, want scoped goal row", got)
	}

	out := stripANSI(nv.view(100, 20))
	for _, want := range []string{"scope=G-0002", ">* G-0002", "qemu_cycles"} {
		if !strings.Contains(out, want) {
			t.Fatalf("goal list missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_GoalDetailViewShowsSessionScope(t *testing.T) {
	v := newGoalDetailView("G-0001", goalScope{GoalID: "G-0001"})
	g := &entity.Goal{
		ID:         "G-0001",
		Status:     entity.GoalStatusActive,
		Objective:  entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"},
		Completion: &entity.Completion{Threshold: 0.2, OnThreshold: entity.GoalOnThresholdAskHuman},
	}
	nv, _ := v.update(goalDetailLoadedMsg{g: g}, nil)
	out := stripANSI(nv.view(100, 20))
	if !strings.Contains(out, "scope=G-0001") {
		t.Fatalf("goal detail should show session scope:\n%s", out)
	}
}

func TestTUI_GoalListCanRetargetScope(t *testing.T) {
	goals := []*entity.Goal{
		{ID: "G-0001", Status: entity.GoalStatusConcluded, Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"}},
		{ID: "G-0002", Status: entity.GoalStatusActive, Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"}},
	}
	v := newGoalListView(goalScope{All: true})
	nv, _ := v.update(goalListLoadedMsg{all: goals, current: "G-0002"}, nil)
	gv := nv.(*goalListView)
	gv.cursor = 1

	m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
	m.stack = []tuiView{newDashboardView(goalScope{All: true}), gv}

	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("goal list scope change should emit a command")
	}
	m = model.(tuiModel)

	model, _ = m.Update(cmd())
	m = model.(tuiModel)
	if got := m.scope; got.All || got.GoalID != "G-0002" {
		t.Fatalf("scope after goal-list retarget = %+v, want G-0002", got)
	}
	if got := m.top().kind(); got != kindGoalList {
		t.Fatalf("scope retarget should land on goal list, top=%s", got)
	}
}

func TestTUI_GoalDetailCanBroadenScope(t *testing.T) {
	m := newTuiModel(nil, goalScope{GoalID: "G-0001"}, 2*time.Second)
	m.stack = []tuiView{
		newDashboardView(goalScope{GoalID: "G-0001"}),
		newGoalDetailView("G-0001", goalScope{GoalID: "G-0001"}),
	}

	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatal("goal detail all-scope change should emit a command")
	}
	m = model.(tuiModel)

	model, _ = m.Update(cmd())
	m = model.(tuiModel)
	if !m.scope.All {
		t.Fatalf("scope after broadening = %+v, want all", m.scope)
	}
	if got := m.top().kind(); got != kindGoalList {
		t.Fatalf("broadening scope should land on goal list, top=%s", got)
	}
}
