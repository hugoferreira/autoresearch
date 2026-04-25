package cli

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type cliExperimentListRow struct {
	ID               string `json:"id"`
	Classification   string `json:"classification"`
	HypothesisStatus string `json:"hypothesis_status,omitempty"`
}

type cliFrontierRow struct {
	Conclusion       string `json:"conclusion"`
	Candidate        string `json:"candidate_experiment"`
	Classification   string `json:"classification"`
	HypothesisStatus string `json:"hypothesis_status,omitempty"`
}

type cliFrontierResponse struct {
	ScopeGoalID    string `json:"scope_goal_id,omitempty"`
	GoalID         string `json:"goal_id"`
	StalledFor     int    `json:"stalled_for"`
	GoalAssessment struct {
		Met             bool   `json:"met"`
		MetByConclusion string `json:"met_by_conclusion,omitempty"`
	} `json:"goal_assessment"`
	Frontier []cliFrontierRow `json:"frontier"`
}

type cliDashboardResponse struct {
	ScopeGoalID string           `json:"scope_goal_id,omitempty"`
	StalledFor  int              `json:"stalled_for"`
	Counts      map[string]int   `json:"counts"`
	Frontier    []cliFrontierRow `json:"frontier"`
}

type cliStatusResponse struct {
	ScopeGoalID       string         `json:"scope_goal_id,omitempty"`
	MainCheckoutDirty bool           `json:"main_checkout_dirty"`
	Counts            map[string]int `json:"counts"`
}

type cliLifecycleFixture struct {
	GoalID     string
	BaselineID string
}

type cliLifecycleCandidate struct {
	HypothesisID string
	ExperimentID string
	CandidateRef string
}

type cliLifecycleCandidateSpec struct {
	Claim       string
	MinEffect   string
	Instruments string
	RefName     string
	Message     string
	Timing      string
	Size        string
	Mechanism   string
}

var _ = Describe("CLI lifecycle scenarios", func() {
	BeforeEach(saveGlobals)

	It("keeps read surfaces consistent after an accepted win and a later stall", func() {
		dir := setupObserveScenarioStore()
		registerScenarioInstruments(dir)
		fixture := setupLifecycleFixture(dir)

		win := setupLifecycleCandidate(dir, cliLifecycleCandidateSpec{
			Claim: "tighten the hot loop", MinEffect: "0.1",
			RefName: "candidate/lifecycle-e1", Message: "improve timing",
			Timing: "80\n", Size: "900\n",
		})
		obs1 := observeLifecycleCandidate(dir, win)
		analyze1 := runCLIJSON[cliAnalyzeResponse](dir, "analyze", win.ExperimentID, "--candidate-ref", win.CandidateRef, "--baseline", fixture.BaselineID)
		Expect(analyze1.Rows).To(ConsistOf(
			HaveField("Instrument", "timing"),
			HaveField("Instrument", "binary_size"),
			HaveField("Instrument", "host_test"),
		))
		Expect(analyzeComparisonDeltaFrac(analyze1, "timing")).To(BeNumerically("<", 0))
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
			Claim: "a smaller tweak might still help", MinEffect: "0.05",
			RefName: "candidate/lifecycle-e2", Message: "small tweak",
			Timing: "95\n", Size: "900\n",
		})
		obs2 := observeLifecycleCandidate(dir, stall)
		runCLIJSON[cliIDResponse](dir,
			"conclude", stall.HypothesisID,
			"--verdict", "inconclusive",
			"--baseline-experiment", fixture.BaselineID,
			"--observations", observeResultID(obs2, "timing"),
		)

		dead := runCLIJSON[[]cliExperimentListRow](dir, "experiment", "list", "--goal", fixture.GoalID, "--classification", experimentClassificationDead)
		Expect(dead).To(HaveLen(1))
		Expect(dead[0].ID).To(Equal(win.ExperimentID))
		Expect(dead[0].HypothesisStatus).To(Equal("supported"))

		expectLifecycleReadSurfacesAgree(dir, fixture.GoalID, win.ExperimentID, concl1.ID, 1)
	})

	It("keeps observation evidence artifacts visible through the conclusion audit chain", func() {
		dir := setupObserveScenarioStore()
		writeScenarioMechanism(dir, "baseline trace\n")
		gitRun(dir, "add", "mechanism.txt")
		gitRun(dir, "commit", "-m", "add mechanism trace")

		registerScenarioTimingInstrument(dir, "mechanism=cat mechanism.txt")
		registerScenarioSupportInstruments(dir)
		fixture := setupLifecycleFixture(dir)
		candidate := setupLifecycleCandidate(dir, cliLifecycleCandidateSpec{
			Claim: "tighten the hot loop", MinEffect: "0.1",
			RefName: "candidate/evidence-e1", Message: "improve timing",
			Timing: "80\n", Size: "900\n", Mechanism: "candidate trace\n",
		})

		obs := observeLifecycleCandidate(dir, candidate)
		timingObsID := observeResultID(obs, "timing")
		concl := runCLIJSON[cliIDResponse](dir,
			"conclude", candidate.HypothesisID,
			"--verdict", "supported",
			"--baseline-experiment", fixture.BaselineID,
			"--observations", timingObsID,
		)
		show := runCLIJSON[conclusionShowJSON](dir, "conclusion", "show", concl.ID)
		Expect(show.ObservationArtifacts).To(HaveKeyWithValue(timingObsID, ContainElement(SatisfyAll(
			HaveField("Name", "evidence/mechanism"),
			HaveField("SHA", Not(BeEmpty())),
			HaveField("Path", Not(BeEmpty())),
			HaveField("Bytes", BeNumerically(">", 0)),
		))))

		registerScenarioTimingInstrument(dir, "broken=echo nope >&2; exit 7")

		second := setupLifecycleCandidate(dir, cliLifecycleCandidateSpec{
			Claim: "a smaller tweak might still help", MinEffect: "0.05",
			Instruments: "timing",
			RefName:     "candidate/evidence-e2", Message: "small tweak",
			Timing: "95\n", Size: "900\n",
		})
		obs2 := runCLIJSON[cliIDResponse](dir, "observe", second.ExperimentID, "--instrument", "timing", "--candidate-ref", second.CandidateRef)
		concl2 := runCLIJSON[cliIDResponse](dir,
			"conclude", second.HypothesisID,
			"--verdict", "inconclusive",
			"--baseline-experiment", fixture.BaselineID,
			"--observations", obs2.ID,
		)

		show2 := runCLIJSON[conclusionShowJSON](dir, "conclusion", "show", concl2.ID)
		Expect(show2.ObservationEvidenceFailures).To(HaveKeyWithValue(obs2.ID, ConsistOf(SatisfyAll(
			HaveField("Name", "broken"),
			HaveField("ExitCode", 7),
		))))
	})
})

func expectLifecycleReadSurfacesAgree(dir, goalID, candidateID, conclusionID string, stalledFor int) {
	GinkgoHelper()
	frontier := runCLIJSON[cliFrontierResponse](dir, "frontier", "--goal", goalID)
	dashboard := runCLIJSON[cliDashboardResponse](dir, "dashboard", "--goal", goalID)
	status := runCLIJSON[cliStatusResponse](dir, "status", "--goal", goalID)

	Expect(frontier.ScopeGoalID).To(Equal(goalID))
	Expect(frontier.GoalID).To(Equal(goalID))
	Expect(dashboard.ScopeGoalID).To(Equal(goalID))
	Expect(status.ScopeGoalID).To(Equal(goalID))
	Expect(status.MainCheckoutDirty).To(BeFalse())
	Expect(frontier.GoalAssessment.Met).To(BeTrue())
	Expect(frontier.GoalAssessment.MetByConclusion).To(Equal(conclusionID))
	Expect(frontier.StalledFor).To(Equal(stalledFor))
	Expect(dashboard.StalledFor).To(Equal(frontier.StalledFor))

	expectedEntry := cliFrontierRow{
		Candidate:        candidateID,
		Conclusion:       conclusionID,
		Classification:   experimentClassificationDead,
		HypothesisStatus: "supported",
	}
	Expect(frontier.Frontier).To(ConsistOf(expectedEntry))
	Expect(dashboard.Frontier).To(ConsistOf(frontier.Frontier))

	for key, value := range status.Counts {
		Expect(dashboard.Counts).To(HaveKeyWithValue(key, value))
	}
}

func setupLifecycleFixture(dir string) cliLifecycleFixture {
	GinkgoHelper()
	goal := runCLIJSON[cliIDResponse](dir,
		"goal", "set",
		"--objective-instrument", "timing",
		"--objective-target", "kernel",
		"--objective-direction", "decrease",
		"--success-threshold", "0.1",
		"--on-success", "stop",
		"--constraint-max", "binary_size=1000",
		"--constraint-require", "host_test=pass",
	)
	baseline := runCLIJSON[cliIDResponse](dir, "experiment", "baseline")
	return cliLifecycleFixture{
		GoalID:     goal.ID,
		BaselineID: baseline.ID,
	}
}

func setupLifecycleCandidate(dir string, spec cliLifecycleCandidateSpec) cliLifecycleCandidate {
	GinkgoHelper()
	if spec.Instruments == "" {
		spec.Instruments = "timing,binary_size,host_test"
	}
	hyp := runCLIJSON[cliIDResponse](dir,
		"hypothesis", "add",
		"--claim", spec.Claim,
		"--predicts-instrument", "timing",
		"--predicts-target", "kernel",
		"--predicts-direction", "decrease",
		"--predicts-min-effect", spec.MinEffect,
		"--kill-if", "tests fail",
	)
	exp := runCLIJSON[cliIDResponse](dir,
		"experiment", "design", hyp.ID,
		"--baseline", "HEAD",
		"--instruments", spec.Instruments,
	)
	impl := runCLIJSON[cliImplementResponse](dir, "experiment", "implement", exp.ID)
	writeScenarioMetrics(impl.Worktree, spec.Timing, spec.Size)
	addArgs := []string{"add", "timing.txt", "size.txt"}
	if spec.Mechanism != "" {
		writeScenarioMechanism(impl.Worktree, spec.Mechanism)
		addArgs = append(addArgs, "mechanism.txt")
	}
	gitRun(impl.Worktree, addArgs...)
	gitRun(impl.Worktree, "commit", "-m", spec.Message)
	candidateRef := gitCreateCandidateRef(impl.Worktree, spec.RefName)
	return cliLifecycleCandidate{
		HypothesisID: hyp.ID,
		ExperimentID: exp.ID,
		CandidateRef: candidateRef,
	}
}

func observeLifecycleCandidate(dir string, candidate cliLifecycleCandidate) cliObserveAllResponse {
	GinkgoHelper()
	return runCLIJSON[cliObserveAllResponse](dir, "observe", candidate.ExperimentID, "--all", "--candidate-ref", candidate.CandidateRef)
}

func observeResultID(resp cliObserveAllResponse, inst string) string {
	GinkgoHelper()
	for _, r := range resp.Results {
		if r.Inst == inst {
			return r.ID
		}
	}
	Fail("observe result for " + inst + " not found")
	return ""
}

func analyzeComparisonDeltaFrac(resp cliAnalyzeResponse, inst string) float64 {
	GinkgoHelper()
	for _, row := range resp.Rows {
		if row.Instrument == inst {
			Expect(row.Comparison).NotTo(BeNil(), "analyze row %q missing comparison", inst)
			return row.Comparison.DeltaFrac
		}
	}
	Fail("analyze row " + inst + " not found")
	return 0
}
