package readmodel

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("status read models", func() {
	It("combines configured budgets with current usage", func() {
		started := time.Date(2026, 4, 18, 18, 30, 0, 0, time.UTC)
		now := time.Date(2026, 4, 18, 20, 0, 0, 0, time.UTC)
		cfg := &store.Config{
			Budgets: store.Budgets{
				MaxExperiments: 12,
				MaxWallTimeH:   8,
				FrontierStallK: 5,
			},
		}
		st := &store.State{
			Counters:          map[string]int{"E": 7},
			ResearchStartedAt: &started,
		}

		got := BuildBudgetSnapshot(cfg, st, now)
		Expect(got.Limits.MaxExperiments).To(Equal(12))
		Expect(got.Limits.MaxWallTimeH).To(Equal(8))
		Expect(got.Limits.FrontierStallK).To(Equal(5))
		Expect(got.Usage.Experiments).To(Equal(7))
		Expect(got.Usage.ElapsedH).To(Equal(1.5))
	})

	It("builds entity count maps with and without lessons", func() {
		Expect(BuildCounts(1, 2, 3, 4)).To(Equal(map[string]int{
			"hypotheses":   1,
			"experiments":  2,
			"observations": 3,
			"conclusions":  4,
		}))
		Expect(BuildCountsWithLessons(1, 2, 3, 4, 5)).To(Equal(map[string]int{
			"hypotheses":   1,
			"experiments":  2,
			"observations": 3,
			"conclusions":  4,
			"lessons":      5,
		}))
	})

	It("finds objective and constraint instruments without observations", func() {
		flashMax := 100.0
		goal := &entity.Goal{
			Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
			Constraints: []entity.Constraint{
				{Instrument: "size_flash", Max: &flashMax},
				{Instrument: "host_test", Require: "pass"},
			},
		}
		obs := []*entity.Observation{
			{Instrument: "host_test"},
		}

		Expect(FindUnobservedGoalInstruments(goal, obs)).To(Equal([]string{"host_timing", "size_flash"}))
	})
})
