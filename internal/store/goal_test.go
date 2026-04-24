package store_test

import (
	"os"
	"path/filepath"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("goal persistence", func() {
	It("allocates, writes, reads, lists, and switches active goals", func() {
		s, _ := mustCreate()

		gID, err := s.AllocID(store.KindGoal)
		Expect(err).NotTo(HaveOccurred())
		Expect(gID).To(Equal("G-0001"))
		now := time.Now().UTC()
		flash := 65536.0
		g := &entity.Goal{
			ID:        gID,
			Status:    entity.GoalStatusActive,
			CreatedAt: &now,
			Objective: entity.Objective{
				Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease",
			},
			Completion: &entity.Completion{Threshold: 0.15, OnThreshold: entity.GoalOnThresholdAskHuman},
			Constraints: []entity.Constraint{
				{Instrument: "size_flash", Max: &flash},
			},
			Body: "# Steering\n\nfocus on loops\n",
		}
		Expect(s.WriteGoal(g)).To(Succeed())
		Expect(s.UpdateState(func(st *store.State) error {
			st.CurrentGoalID = gID
			return nil
		})).To(Succeed())

		back, err := s.ReadGoal(gID)
		Expect(err).NotTo(HaveOccurred())
		Expect(back.Objective.Instrument).To(Equal("qemu_cycles"))
		Expect(back.Status).To(Equal(entity.GoalStatusActive))
		active, err := s.ActiveGoal()
		Expect(err).NotTo(HaveOccurred())
		Expect(active.ID).To(Equal(gID))

		closed := time.Now().UTC()
		back.Status = entity.GoalStatusConcluded
		back.ClosedAt = &closed
		Expect(s.WriteGoal(back)).To(Succeed())
		Expect(s.UpdateState(func(st *store.State) error {
			st.CurrentGoalID = ""
			return nil
		})).To(Succeed())

		gID2, err := s.AllocID(store.KindGoal)
		Expect(err).NotTo(HaveOccurred())
		Expect(gID2).To(Equal("G-0002"))
		g2 := &entity.Goal{
			ID: gID2, Status: entity.GoalStatusActive, DerivedFrom: gID,
			CreatedAt: &now,
			Objective: entity.Objective{Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease"},
			Constraints: []entity.Constraint{
				{Instrument: "size_flash", Max: &flash},
			},
		}
		Expect(s.WriteGoal(g2)).To(Succeed())
		Expect(s.UpdateState(func(st *store.State) error {
			st.CurrentGoalID = gID2
			return nil
		})).To(Succeed())

		all, err := s.ListGoals()
		Expect(err).NotTo(HaveOccurred())
		Expect(all).To(HaveLen(2))
		Expect(all[1].DerivedFrom).To(Equal(gID))
		active2, err := s.ActiveGoal()
		Expect(err).NotTo(HaveOccurred())
		Expect(active2.ID).To(Equal(gID2))
	})

	It("migrates legacy single-goal stores to goal entities", func() {
		s, dir := mustCreate()
		legacyData := []byte(`---
schema_version: 1
objective:
  instrument: qemu_cycles
  target: dsp_fir
  direction: decrease
  target_effect: 0.15
constraints:
  - instrument: size_flash
    max: 65536
---

# Steering

start with unrolling
`)
		Expect(os.WriteFile(filepath.Join(dir, ".research", "goal.md"), legacyData, 0o644)).To(Succeed())

		hID, err := s.AllocID(store.KindHypothesis)
		Expect(err).NotTo(HaveOccurred())
		h := &entity.Hypothesis{
			ID: hID, Claim: "unroll dsp_fir",
			Predicts:  entity.Predicts{Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease", MinEffect: 0.1},
			KillIf:    []string{"flash grows"},
			Status:    entity.StatusOpen,
			Author:    "human:alice",
			CreatedAt: time.Now().UTC(),
		}
		Expect(s.WriteHypothesis(h)).To(Succeed())
		Expect(s.UpdateState(func(st *store.State) error {
			st.SchemaVersion = 1
			st.CurrentGoalID = ""
			return nil
		})).To(Succeed())
		Expect(os.RemoveAll(filepath.Join(dir, ".research", "goals"))).To(Succeed())

		s2, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		_, err = os.Stat(filepath.Join(dir, ".research", "goal.md"))
		Expect(os.IsNotExist(err)).To(BeTrue())

		st, err := s2.State()
		Expect(err).NotTo(HaveOccurred())
		Expect(st.SchemaVersion).To(Equal(store.StateSchemaVersion))
		Expect(st.CurrentGoalID).To(Equal("G-0001"))

		g, err := s2.ReadGoal("G-0001")
		Expect(err).NotTo(HaveOccurred())
		Expect(g.Status).To(Equal(entity.GoalStatusActive))
		Expect(g.CreatedAt).NotTo(BeNil())
		Expect(g.Objective.Instrument).To(Equal("qemu_cycles"))
		Expect(g.Completion).NotTo(BeNil())
		Expect(*g.Completion).To(Equal(entity.Completion{
			Threshold:   0.15,
			OnThreshold: entity.GoalOnThresholdAskHuman,
		}))

		hBack, err := s2.ReadHypothesis(hID)
		Expect(err).NotTo(HaveOccurred())
		Expect(hBack.GoalID).To(Equal("G-0001"))

		s3, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		_, err = s3.ActiveGoal()
		Expect(err).NotTo(HaveOccurred())
	})
})
