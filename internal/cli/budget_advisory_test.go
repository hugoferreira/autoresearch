package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("budget advisory surfaces", func() {
	BeforeEach(saveGlobals)

	It("renders stale active experiments in status text using the advisory threshold", func() {
		dir, s := setupGoalStore()
		old := time.Now().UTC().Add(-2 * time.Hour)
		Expect(s.WriteHypothesis(&entity.Hypothesis{
			ID:        "H-0001",
			GoalID:    "G-0001",
			Claim:     "try the stale candidate",
			Predicts:  entity.Predicts{Instrument: "timing", Target: "kernel", Direction: "decrease"},
			KillIf:    []string{"no improvement"},
			Status:    entity.StatusOpen,
			Author:    "agent:test",
			CreatedAt: old,
		})).To(Succeed())
		Expect(s.WriteExperiment(&entity.Experiment{
			ID:          "E-0001",
			GoalID:      "G-0001",
			Hypothesis:  "H-0001",
			Status:      entity.ExpImplemented,
			Baseline:    entity.Baseline{Ref: "HEAD"},
			Instruments: []string{"timing"},
			Author:      "agent:test",
			CreatedAt:   old,
		})).To(Succeed())
		Expect(s.AppendEvent(store.Event{
			Ts:      old,
			Kind:    "experiment.implement",
			Actor:   "agent:test",
			Subject: "E-0001",
		})).To(Succeed())

		out := runCLI(dir, "status", "--goal", "G-0001")

		expectText(out,
			"budget advisory warnings:",
			"stale_experiment",
			"stale experiments (>60m recommended threshold since last activity):",
			"E-0001",
		)
		status := runCLIJSON[cliStatusResponse](dir, "status", "--goal", "G-0001")
		Expect(status.StaleExperiments).To(ContainElement(HaveField("ID", "E-0001")))
		Expect(status.BudgetAdvisory.StaleExperiments).To(ContainElement(HaveField("ID", "E-0001")))
	})

	It("reports frontier stalls in cycle-context budget_advisory", func() {
		dir := setupObserveScenarioStore()
		registerScenarioInstruments(dir)
		fixture := setupLifecycleFixture(dir)
		s, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(s.UpdateConfig(func(cfg *store.Config) error {
			cfg.Budgets.FrontierStallK = 1
			return nil
		})).To(Succeed())

		win := setupLifecycleCandidate(dir, cliLifecycleCandidateSpec{
			Claim: "tighten the hot loop", MinEffect: "0.1",
			RefName: "candidate/advisory-win", Message: "improve timing",
			Timing: "80\n", Size: "900\n",
		})
		obs1 := observeLifecycleCandidate(dir, win)
		concl1 := runCLIJSON[cliIDResponse](dir,
			"conclude", win.HypothesisID,
			"--verdict", "supported",
			"--baseline-experiment", fixture.BaselineID,
			"--observations", observeResultID(obs1, "timing"),
		)
		runCLIJSON[cliIDResponse](dir,
			"conclusion", "accept", concl1.ID,
			"--reviewed-by", "human:gate",
			"--rationale", "Stats confirmed. Code matches the mechanism. No gaming or metric manipulation was detected.",
		)

		stall := setupLifecycleCandidate(dir, cliLifecycleCandidateSpec{
			Claim: "a smaller tweak might help", MinEffect: "0.05",
			RefName: "candidate/advisory-stall", Message: "small tweak",
			Timing: "95\n", Size: "900\n",
		})
		obs2 := observeLifecycleCandidate(dir, stall)
		runCLIJSON[cliIDResponse](dir,
			"conclude", stall.HypothesisID,
			"--verdict", "inconclusive",
			"--baseline-experiment", fixture.BaselineID,
			"--observations", observeResultID(obs2, "timing"),
		)

		ctx := runCLIJSON[cliCycleContextResponse](dir, "cycle-context", "--goal", fixture.GoalID)

		Expect(ctx.BudgetAdvisory.Frontier.Applicable).To(BeTrue())
		Expect(ctx.BudgetAdvisory.Frontier.StalledFor).To(Equal(1))
		Expect(ctx.BudgetAdvisory.Frontier.Limit).To(Equal(1))
		Expect(ctx.BudgetAdvisory.Frontier.StallReached).To(BeTrue())
		Expect(budgetWarningCodes(ctx.BudgetAdvisory.Warnings)).To(ContainElement("frontier_stalled"))
	})
})

func budgetWarningCodes(warnings []readmodel.BudgetWarning) []string {
	out := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		out = append(out, warning.Code)
	}
	return out
}
