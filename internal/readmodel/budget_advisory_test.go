package readmodel

import (
	"time"

	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("budget advisory read model", func() {
	It("keeps unset experiment and wall-time budgets advisory-only", func() {
		now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
		advisory := BuildBudgetAdvisory(BudgetAdvisoryInputs{
			Config: &store.Config{},
			State:  &store.State{Counters: map[string]int{"E": 999}},
			Now:    now,
		})

		Expect(advisory.ConfiguredLimits.MaxExperiments).To(Equal(0))
		Expect(advisory.EffectiveLimits.MaxExperiments).To(Equal(0))
		Expect(advisory.LimitSources.MaxExperiments).To(Equal("unlimited"))
		Expect(advisory.EffectiveLimits.MaxWallTimeH).To(Equal(0))
		Expect(advisory.LimitSources.MaxWallTimeH).To(Equal("unlimited"))
		Expect(advisory.EffectiveLimits.FrontierStallK).To(Equal(DefaultBudgetAdvisoryFrontierStallK))
		Expect(advisory.LimitSources.FrontierStallK).To(Equal("recommended"))
		Expect(advisory.EffectiveLimits.StaleExperimentMinutes).To(Equal(DefaultBudgetAdvisoryStaleExperimentMinutes))
		Expect(advisory.LimitSources.StaleExperimentMinutes).To(Equal("recommended"))
		Expect(advisory.Warnings).NotTo(ContainElement(HaveField("Code", ContainSubstring("max_experiments"))))
		Expect(advisory.Warnings).NotTo(ContainElement(HaveField("Code", ContainSubstring("max_wall_time"))))
	})

	It("counts observations since the last conclusion and warns at the advisory threshold", func() {
		now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
		events := []store.Event{
			{Ts: now.Add(-10 * time.Minute), Kind: "conclusion.write", Subject: "C-0001"},
			{Ts: now.Add(-9 * time.Minute), Kind: "observation.record", Subject: "O-0001"},
			{Ts: now.Add(-8 * time.Minute), Kind: "observation.record", Subject: "O-0002"},
			{Ts: now.Add(-7 * time.Minute), Kind: "observation.record", Subject: "O-0003"},
			{Ts: now.Add(-6 * time.Minute), Kind: "observation.record", Subject: "O-0004"},
			{Ts: now.Add(-5 * time.Minute), Kind: "observation.record", Subject: "O-0005"},
		}

		advisory := BuildBudgetAdvisory(BudgetAdvisoryInputs{
			Config: &store.Config{},
			State:  &store.State{Counters: map[string]int{}},
			Events: events,
			Now:    now,
		})

		Expect(advisory.Usage.ObservationsWithoutConclusion).To(Equal(5))
		Expect(advisory.Warnings).To(ContainElement(HaveField("Code", "observations_without_conclusion")))
	})
})
