package readmodel

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("TestBuildBudgetSnapshot", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

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
		if got.Limits.MaxExperiments != 12 || got.Limits.MaxWallTimeH != 8 || got.Limits.FrontierStallK != 5 {
			t.Fatalf("unexpected limits: %+v", got.Limits)
		}
		if got.Usage.Experiments != 7 {
			t.Fatalf("usage.experiments = %d, want 7", got.Usage.Experiments)
		}
		if got.Usage.ElapsedH != 1.5 {
			t.Fatalf("usage.elapsed_h = %v, want 1.5", got.Usage.ElapsedH)
		}
	})
})

var _ = ginkgo.Describe("TestBuildCounts", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		got := BuildCounts(1, 2, 3, 4)
		want := map[string]int{
			"hypotheses":   1,
			"experiments":  2,
			"observations": 3,
			"conclusions":  4,
		}
		if len(got) != len(want) {
			t.Fatalf("len(counts) = %d, want %d", len(got), len(want))
		}
		for key, wantVal := range want {
			if got[key] != wantVal {
				t.Fatalf("counts[%q] = %d, want %d", key, got[key], wantVal)
			}
		}
	})
})

var _ = ginkgo.Describe("TestBuildCountsWithLessons", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		got := BuildCountsWithLessons(1, 2, 3, 4, 5)
		if got["lessons"] != 5 {
			t.Fatalf("counts[lessons] = %d, want 5", got["lessons"])
		}
	})
})

var _ = ginkgo.Describe("TestFindUnobservedGoalInstruments", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

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

		got := FindUnobservedGoalInstruments(goal, obs)
		if got[0] != "host_timing" || got[1] != "size_flash" || len(got) != 2 {
			t.Fatalf("unexpected unobserved instruments: %+v", got)
		}
	})
})
