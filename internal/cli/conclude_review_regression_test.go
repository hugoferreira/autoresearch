package cli

import (
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func setupConcludeReviewedRegressionStore() (string, string) {
	GinkgoHelper()

	dir := GinkgoT().TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	Expect(err).NotTo(HaveOccurred())

	now := time.Date(2026, 4, 24, 13, 0, 0, 0, time.UTC)
	goal := &entity.Goal{
		ID:        "G-0001",
		Status:    entity.GoalStatusActive,
		CreatedAt: &now,
		Objective: entity.Objective{Instrument: "timing", Direction: "decrease"},
		Constraints: []entity.Constraint{
			{Instrument: "host_test", Require: "pass"},
		},
	}
	Expect(s.WriteGoal(goal)).To(Succeed())
	Expect(s.UpdateState(func(st *store.State) error {
		st.CurrentGoalID = goal.ID
		return nil
	})).To(Succeed())

	hyp := &entity.Hypothesis{
		ID:        "H-0001",
		GoalID:    goal.ID,
		Claim:     "tighten the hot loop",
		Status:    entity.StatusOpen,
		Author:    "agent:analyst",
		CreatedAt: now,
		Predicts: entity.Predicts{
			Instrument: "timing",
			Target:     "kernel",
			Direction:  "decrease",
			MinEffect:  0.1,
		},
		KillIf: []string{"tests fail"},
	}
	Expect(s.WriteHypothesis(hyp)).To(Succeed())

	base := &entity.Experiment{
		ID:          "E-0001",
		GoalID:      goal.ID,
		IsBaseline:  true,
		Status:      entity.ExpMeasured,
		Baseline:    entity.Baseline{Ref: "HEAD", SHA: strings.Repeat("a", 40)},
		Instruments: []string{"timing"},
		Attempt:     1,
		Author:      "system",
		CreatedAt:   now,
	}
	cand := &entity.Experiment{
		ID:          "E-0002",
		GoalID:      goal.ID,
		Hypothesis:  hyp.ID,
		Status:      entity.ExpMeasured,
		Baseline:    entity.Baseline{Ref: "HEAD", SHA: strings.Repeat("a", 40), Experiment: base.ID},
		Instruments: []string{"timing"},
		Attempt:     1,
		Author:      "agent:orchestrator",
		CreatedAt:   now,
	}
	for _, e := range []*entity.Experiment{base, cand} {
		Expect(s.WriteExperiment(e)).To(Succeed())
	}

	baseObs := &entity.Observation{
		ID:           "O-0001",
		Experiment:   base.ID,
		Instrument:   "timing",
		MeasuredAt:   now,
		Value:        100,
		Unit:         "ns",
		Samples:      5,
		PerSample:    []float64{100, 100, 100, 100, 100},
		Attempt:      1,
		CandidateSHA: strings.Repeat("b", 40),
		Author:       "agent:observer",
	}
	candObs := &entity.Observation{
		ID:           "O-0002",
		Experiment:   cand.ID,
		Instrument:   "timing",
		MeasuredAt:   now.Add(time.Minute),
		Value:        70,
		Unit:         "ns",
		Samples:      5,
		PerSample:    []float64{70, 70, 70, 70, 70},
		Attempt:      1,
		CandidateRef: "refs/heads/candidate/reviewed",
		CandidateSHA: strings.Repeat("c", 40),
		BaselineSHA:  strings.Repeat("a", 40),
		Author:       "agent:observer",
	}
	for _, o := range []*entity.Observation{baseObs, candObs} {
		Expect(s.WriteObservation(o)).To(Succeed())
	}

	return dir, candObs.ID
}

var _ = Describe("conclude --reviewed-by", func() {
	BeforeEach(saveGlobals)

	It("promotes the hypothesis consistently when the conclusion is already reviewed", func() {
		dir, obsID := setupConcludeReviewedRegressionStore()

		resp := runCLIJSON[concludeJSONResponse](dir,
			"conclude", "H-0001",
			"--verdict", "supported",
			"--baseline-experiment", "E-0001",
			"--observations", obsID,
			"--reviewed-by", "human:gate",
		)
		Expect(resp.Conclusion.ReviewedBy).To(Equal("human:gate"))

		s, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		hyp, err := s.ReadHypothesis("H-0001")
		Expect(err).NotTo(HaveOccurred())
		Expect(hyp.Status).To(Equal(entity.StatusSupported))
	})
})
