package cli

import (
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

func setupConcludeReviewedRegressionStore(t testkit.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatalf("store.Create: %v", err)
	}

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
	if err := s.WriteGoal(goal); err != nil {
		t.Fatalf("WriteGoal: %v", err)
	}
	if err := s.UpdateState(func(st *store.State) error {
		st.CurrentGoalID = goal.ID
		return nil
	}); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

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
	if err := s.WriteHypothesis(hyp); err != nil {
		t.Fatalf("WriteHypothesis: %v", err)
	}

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
		if err := s.WriteExperiment(e); err != nil {
			t.Fatalf("WriteExperiment(%s): %v", e.ID, err)
		}
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
		if err := s.WriteObservation(o); err != nil {
			t.Fatalf("WriteObservation(%s): %v", o.ID, err)
		}
	}

	return dir, candObs.ID
}

var _ = ginkgo.Describe("TestConcludeReviewedByPromotesHypothesisConsistently", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		saveGlobals(t)
		dir, obsID := setupConcludeReviewedRegressionStore(t)

		resp := runCLIJSON[concludeJSONResponse](t, dir,
			"conclude", "H-0001",
			"--verdict", "supported",
			"--baseline-experiment", "E-0001",
			"--observations", obsID,
			"--reviewed-by", "human:gate",
		)
		if resp.Conclusion.ReviewedBy != "human:gate" {
			t.Fatalf("conclusion reviewed_by = %q, want human:gate", resp.Conclusion.ReviewedBy)
		}

		s, err := store.Open(dir)
		if err != nil {
			t.Fatalf("store.Open: %v", err)
		}
		hyp, err := s.ReadHypothesis("H-0001")
		if err != nil {
			t.Fatalf("ReadHypothesis: %v", err)
		}
		if got, want := hyp.Status, entity.StatusSupported; got != want {
			t.Fatalf("hypothesis status after reviewed conclusion = %q, want %q", got, want)
		}
	})
})
