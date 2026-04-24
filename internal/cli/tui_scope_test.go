package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	tea "github.com/charmbracelet/bubbletea"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TUI goal scope", func() {
	var goals []*entity.Goal

	BeforeEach(func() {
		goals = []*entity.Goal{
			{ID: "G-0001", Status: entity.GoalStatusConcluded, Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"}},
			{ID: "G-0002", Status: entity.GoalStatusActive, Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"}},
		}
	})

	It("marks the active session scope in the goal list", func() {
		v := newGoalListView(goalScope{GoalID: "G-0002"})
		nv, _ := v.update(goalListLoadedMsg{all: goals, current: "G-0002"}, nil)
		gv := nv.(*goalListView)
		Expect(gv.cursor).To(Equal(1))

		out := stripANSI(nv.view(100, 20))
		expectText(out, "scope=G-0002", ">* G-0002", "qemu_cycles")
	})

	It("shows the active session scope in goal detail", func() {
		v := newGoalDetailView("G-0001", goalScope{GoalID: "G-0001"})
		g := &entity.Goal{
			ID:         "G-0001",
			Status:     entity.GoalStatusActive,
			Objective:  entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"},
			Completion: &entity.Completion{Threshold: 0.2, OnThreshold: entity.GoalOnThresholdAskHuman},
		}
		nv, _ := v.update(goalDetailLoadedMsg{g: g}, nil)
		expectText(stripANSI(nv.view(100, 20)), "scope=G-0001")
	})

	It("can retarget all-goal sessions from the goal list", func() {
		v := newGoalListView(goalScope{All: true})
		nv, _ := v.update(goalListLoadedMsg{all: goals, current: "G-0002"}, nil)
		gv := nv.(*goalListView)
		gv.cursor = 1

		m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
		m.stack = []tuiView{newDashboardView(goalScope{All: true}), gv}

		model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		Expect(cmd).NotTo(BeNil())
		m = model.(tuiModel)

		model, _ = m.Update(cmd())
		m = model.(tuiModel)
		Expect(m.scope).To(Equal(goalScope{GoalID: "G-0002"}))
		Expect(m.top().kind()).To(Equal(kindGoalList))
	})

	It("can broaden a scoped goal-detail session back to all goals", func() {
		m := newTuiModel(nil, goalScope{GoalID: "G-0001"}, 2*time.Second)
		m.stack = []tuiView{
			newDashboardView(goalScope{GoalID: "G-0001"}),
			newGoalDetailView("G-0001", goalScope{GoalID: "G-0001"}),
		}

		model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		Expect(cmd).NotTo(BeNil())
		m = model.(tuiModel)

		model, _ = m.Update(cmd())
		m = model.(tuiModel)
		Expect(m.scope.All).To(BeTrue())
		Expect(m.top().kind()).To(Equal(kindGoalList))
	})
})
