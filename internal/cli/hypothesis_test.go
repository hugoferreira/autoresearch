package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("hypothesis add", func() {
	BeforeEach(saveGlobals)

	It("rejects predicted instruments outside the active goal boundary", func() {
		dir := GinkgoT().TempDir()
		s, err := store.Create(dir, store.Config{
			Build: store.CommandSpec{Command: "true"},
			Test:  store.CommandSpec{Command: "true"},
			Instruments: map[string]store.Instrument{
				"timing":      {Unit: "s"},
				"binary_size": {Unit: "bytes"},
				"compile":     {Unit: "bool"},
				"qemu_cycles": {Unit: "cycles"},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		now := time.Now().UTC()
		max := 131072.0
		goal := &entity.Goal{
			ID:        "G-0001",
			Status:    entity.GoalStatusActive,
			CreatedAt: &now,
			Objective: entity.Objective{
				Instrument: "timing",
				Direction:  "decrease",
			},
			Constraints: []entity.Constraint{
				{Instrument: "binary_size", Max: &max},
				{Instrument: "compile", Require: "pass"},
			},
		}
		Expect(s.WriteGoal(goal)).To(Succeed())
		Expect(s.UpdateState(func(st *store.State) error {
			st.CurrentGoalID = goal.ID
			return nil
		})).To(Succeed())

		root := Root()
		root.SetArgs([]string{
			"-C", dir,
			"hypothesis", "add",
			"--claim", "improve qemu cycle count",
			"--predicts-instrument", "qemu_cycles",
			"--predicts-target", "firmware",
			"--predicts-direction", "decrease",
			"--predicts-min-effect", "0.1",
			"--kill-if", "tests fail",
		})

		Expect(root.Execute()).To(MatchError(ContainSubstring("goal objective or an explicit constraint instrument")))
	})
})
