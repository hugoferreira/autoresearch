package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("analyze command", func() {
	BeforeEach(saveGlobals)

	It("uses a stored candidate ref even after the branch is deleted", func() {
		dir, scenario := setupTimingObserveScenario()

		candidateRef := commitScenarioMetricsCandidate(scenario.Worktree, "candidate/analyze-deleted-ref", "candidate a", "90\n", "900\n")
		runCLIJSON[observeRecordJSON](dir,
			"observe", scenario.ExpID,
			"--instrument", "timing",
			"--candidate-ref", candidateRef,
		)

		fullRef := "refs/heads/" + candidateRef
		gitRun(scenario.Worktree, "branch", "-D", candidateRef)

		resp := runCLIJSON[cliAnalyzeResponse](dir,
			"analyze", scenario.ExpID,
			"--candidate-ref", fullRef,
		)
		Expect(resp.Rows).To(HaveLen(1))
		Expect(resp.Rows[0].Instrument).To(Equal("timing"))
	})

	It("rejects analyzing a baseline experiment with multiple recorded scopes", func() {
		dir, baselineID := setupAnalyzeAmbiguousBaseline()

		_, _, err := runCLIResult(dir, "analyze", baselineID)
		Expect(err).To(MatchError(ContainSubstring("experiment " + baselineID + " has observations for multiple recorded scopes")))
	})

	It("rejects an ambiguous baseline argument for candidate analysis", func() {
		dir, baselineID := setupAnalyzeAmbiguousBaseline()
		hyp := runCLIJSON[cliIDResponse](dir,
			"hypothesis", "add",
			"--claim", "tighten the hot loop",
			"--predicts-instrument", "timing",
			"--predicts-target", "kernel",
			"--predicts-direction", "decrease",
			"--predicts-min-effect", "0.1",
			"--kill-if", "tests fail",
		)
		exp := runCLIJSON[cliIDResponse](dir,
			"experiment", "design", hyp.ID,
			"--baseline", "HEAD",
			"--instruments", "timing",
		)
		impl := runCLIJSON[cliImplementResponse](dir, "experiment", "implement", exp.ID)

		candidateRef := commitScenarioMetricsCandidate(impl.Worktree, "candidate/analyze-ambiguous-baseline", "candidate a", "90\n", "900\n")
		runCLIJSON[observeRecordJSON](dir,
			"observe", exp.ID,
			"--instrument", "timing",
			"--candidate-ref", candidateRef,
		)

		_, _, err := runCLIResult(dir,
			"analyze", exp.ID,
			"--candidate-ref", candidateRef,
			"--baseline", baselineID,
		)
		Expect(err).To(MatchError(ContainSubstring("baseline experiment " + baselineID + " has observations for multiple recorded scopes")))
	})

	It("rejects a candidate ref that maps to multiple recorded scopes", func() {
		dir, s := createCLIStoreDir()
		now := time.Now().UTC()
		ref := "refs/heads/candidate/E-0001-a1"
		Expect(s.WriteExperiment(&entity.Experiment{
			ID:          "E-0001",
			GoalID:      "G-0001",
			Hypothesis:  "H-0001",
			Status:      entity.ExpMeasured,
			Baseline:    entity.Baseline{Ref: "HEAD"},
			Instruments: []string{"timing"},
			Author:      "test",
			CreatedAt:   now,
		})).To(Succeed())
		for _, o := range []*entity.Observation{
			{ID: "O-0001", Attempt: 1, CandidateSHA: "1111111111111111111111111111111111111111"},
			{ID: "O-0002", Attempt: 2, CandidateSHA: "2222222222222222222222222222222222222222"},
		} {
			o.Experiment = "E-0001"
			o.Instrument = "timing"
			o.MeasuredAt = now
			o.Value = 90
			o.Unit = "ns"
			o.Samples = 1
			o.CandidateRef = ref
			o.Author = "test"
			Expect(s.WriteObservation(o)).To(Succeed())
		}

		_, _, err := runCLIResult(dir, "analyze", "E-0001", "--candidate-ref", ref)
		Expect(err).To(MatchError(ContainSubstring("candidate ref " + ref + " maps to multiple recorded candidate scopes")))
	})

	It("resolves --baseline auto to the supported lineage predecessor", func() {
		dir, s := createCLIStoreDir()
		now := time.Now().UTC()
		Expect(s.WriteGoal(&entity.Goal{
			ID:        "G-0001",
			Status:    entity.GoalStatusActive,
			CreatedAt: &now,
			Objective: entity.Objective{Instrument: "timing", Direction: "decrease"},
		})).To(Succeed())
		Expect(s.WriteHypothesis(&entity.Hypothesis{
			ID:        "H-0001",
			GoalID:    "G-0001",
			Claim:     "first win",
			Status:    entity.StatusSupported,
			Author:    "test",
			CreatedAt: now,
			Predicts:  entity.Predicts{Instrument: "timing", Target: "kernel", Direction: "decrease", MinEffect: 0.05},
		})).To(Succeed())
		Expect(s.WriteHypothesis(&entity.Hypothesis{
			ID:        "H-0002",
			GoalID:    "G-0001",
			Parent:    "H-0001",
			Claim:     "second win",
			Status:    entity.StatusOpen,
			Author:    "test",
			CreatedAt: now,
			Predicts:  entity.Predicts{Instrument: "timing", Target: "kernel", Direction: "decrease", MinEffect: 0.05},
		})).To(Succeed())
		for _, exp := range []*entity.Experiment{
			{ID: "E-0001", GoalID: "G-0001", Hypothesis: "H-0001", Status: entity.ExpAnalyzed, Baseline: entity.Baseline{Ref: "HEAD"}, Instruments: []string{"timing"}, Author: "test", CreatedAt: now},
			{ID: "E-0002", GoalID: "G-0001", Hypothesis: "H-0002", Status: entity.ExpMeasured, Baseline: entity.Baseline{Ref: "HEAD"}, Instruments: []string{"timing"}, Author: "test", CreatedAt: now},
		} {
			Expect(s.WriteExperiment(exp)).To(Succeed())
		}
		Expect(s.WriteObservation(&entity.Observation{
			ID: "O-0001", Experiment: "E-0001", Instrument: "timing",
			MeasuredAt: now, Value: 100, Unit: "ns", PerSample: []float64{100, 101, 99}, Samples: 3, Author: "test",
		})).To(Succeed())
		ref := "refs/heads/candidate/E-0002"
		Expect(s.WriteObservation(&entity.Observation{
			ID: "O-0002", Experiment: "E-0002", Instrument: "timing",
			MeasuredAt: now, Value: 90, Unit: "ns", PerSample: []float64{90, 91, 89}, Samples: 3,
			CandidateRef: ref, CandidateSHA: "2222222222222222222222222222222222222222", Author: "test",
		})).To(Succeed())
		Expect(s.WriteConclusion(&entity.Conclusion{
			ID: "C-0001", Hypothesis: "H-0001", Verdict: entity.VerdictSupported,
			Observations: []string{"O-0001"}, CandidateExp: "E-0001",
			Effect:   entity.Effect{Instrument: "timing", DeltaFrac: -0.1},
			StatTest: "mann_whitney_u", Author: "test", ReviewedBy: "human:gate", CreatedAt: now,
		})).To(Succeed())

		resp := runCLIJSON[cliAnalyzeResponse](dir,
			"analyze", "E-0002",
			"--candidate-ref", ref,
			"--baseline", "auto",
		)

		Expect(resp.Baseline).To(Equal("E-0001"))
		Expect(resp.BaselineResolution).NotTo(BeNil())
		Expect(resp.BaselineResolution.ExperimentID).To(Equal("E-0001"))
		Expect(resp.BaselineResolution.Source).To(Equal(readmodel.BaselineSourceAncestorSupported))
		Expect(resp.BaselineResolution.AncestorHypothesis).To(Equal("H-0001"))
		Expect(resp.BaselineResolution.AncestorConclusion).To(Equal("C-0001"))
		Expect(resp.Rows).To(HaveLen(1))
		Expect(resp.Rows[0].Comparison).NotTo(BeNil())
	})
})

func setupAnalyzeAmbiguousBaseline() (string, string) {
	GinkgoHelper()

	dir := setupObserveScenarioStore()
	registerScenarioInstruments(dir)
	runCLIJSON[cliIDResponse](dir,
		"goal", "set",
		"--objective-instrument", "timing",
		"--objective-target", "kernel",
		"--objective-direction", "decrease",
		"--constraint-max", "binary_size=1000",
	)
	baseline := runCLIJSON[cliIDResponse](dir, "experiment", "baseline")
	addAnalyzeBaselineScope(dir, baseline.ID, 2, 95)
	return dir, baseline.ID
}

func addAnalyzeBaselineScope(dir, baselineID string, attempt int, value float64) {
	GinkgoHelper()

	s, err := store.Open(dir)
	Expect(err).NotTo(HaveOccurred())
	exp, err := s.ReadExperiment(baselineID)
	Expect(err).NotTo(HaveOccurred())
	id, err := s.AllocID(store.KindObservation)
	Expect(err).NotTo(HaveOccurred())
	Expect(s.WriteObservation(&entity.Observation{
		ID:           id,
		Experiment:   baselineID,
		Instrument:   "timing",
		MeasuredAt:   time.Now().UTC(),
		Value:        value,
		Unit:         "ns",
		Samples:      1,
		Command:      "sh -c cat timing.txt",
		ExitCode:     0,
		Attempt:      attempt,
		CandidateSHA: exp.Baseline.SHA,
		Author:       "test",
	})).To(Succeed())
}
