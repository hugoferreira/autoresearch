package cli

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type cliIDResponse struct {
	ID string `json:"id"`
}

type cliImplementResponse struct {
	ID       string `json:"id"`
	Worktree string `json:"worktree"`
	Branch   string `json:"branch"`
}

type cliObserveAllResponse struct {
	Action             string   `json:"action"`
	Observations       []string `json:"observations"`
	NewObservations    []string `json:"new_observations"`
	ReusedObservations []string `json:"reused_observations"`
	Results            []struct {
		ID     string   `json:"id"`
		IDs    []string `json:"ids"`
		Inst   string   `json:"instrument"`
		Action string   `json:"action"`
	} `json:"results"`
}

type cliAnalyzeResponse struct {
	Rows []struct {
		Instrument string `json:"instrument"`
		Comparison *struct {
			DeltaFrac float64 `json:"delta_frac"`
		} `json:"comparison,omitempty"`
	} `json:"rows"`
}

type cliExperimentListRow struct {
	ID               string `json:"id"`
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
	Frontier []struct {
		Conclusion       string `json:"conclusion"`
		Candidate        string `json:"candidate_experiment"`
		Classification   string `json:"classification"`
		HypothesisStatus string `json:"hypothesis_status,omitempty"`
	} `json:"frontier"`
}

type cliDashboardResponse struct {
	ScopeGoalID string         `json:"scope_goal_id,omitempty"`
	StalledFor  int            `json:"stalled_for"`
	Counts      map[string]int `json:"counts"`
	Frontier    []struct {
		Conclusion       string `json:"conclusion"`
		Candidate        string `json:"candidate_experiment"`
		Classification   string `json:"classification"`
		HypothesisStatus string `json:"hypothesis_status,omitempty"`
	} `json:"frontier"`
}

type cliStatusResponse struct {
	ScopeGoalID       string         `json:"scope_goal_id,omitempty"`
	MainCheckoutDirty bool           `json:"main_checkout_dirty"`
	Counts            map[string]int `json:"counts"`
}

var _ = Describe("CLI lifecycle scenarios", func() {
	BeforeEach(saveGlobals)

	It("keeps read surfaces consistent after an accepted win and a later stall", func() {
		dir := gitInitScenarioRepo()
		_, err := store.Create(dir, store.Config{
			Build:     store.CommandSpec{Command: "true"},
			Test:      store.CommandSpec{Command: "true"},
			Worktrees: store.WorktreesConfig{Root: filepath.Join(GinkgoT().TempDir(), "worktrees")},
		})
		Expect(err).NotTo(HaveOccurred())

		registerScenarioInstruments(dir)

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

		hyp1 := runCLIJSON[cliIDResponse](dir,
			"hypothesis", "add",
			"--claim", "tighten the hot loop",
			"--predicts-instrument", "timing",
			"--predicts-target", "kernel",
			"--predicts-direction", "decrease",
			"--predicts-min-effect", "0.1",
			"--kill-if", "tests fail",
		)
		exp1 := runCLIJSON[cliIDResponse](dir,
			"experiment", "design", hyp1.ID,
			"--baseline", "HEAD",
			"--instruments", "timing,binary_size,host_test",
		)
		impl1 := runCLIJSON[cliImplementResponse](dir, "experiment", "implement", exp1.ID)
		writeScenarioMetrics(impl1.Worktree, "80\n", "900\n")
		gitCommitAll(impl1.Worktree, "improve timing")
		candidateRef1 := gitCreateCandidateRef(impl1.Worktree, "candidate/lifecycle-e1")

		obs1 := runCLIJSON[cliObserveAllResponse](dir, "observe", exp1.ID, "--all", "--candidate-ref", candidateRef1)
		analyze1 := runCLIJSON[cliAnalyzeResponse](dir, "analyze", exp1.ID, "--candidate-ref", candidateRef1, "--baseline", baseline.ID)
		Expect(analyze1.Rows).To(HaveLen(3))
		Expect(analyzeComparisonDeltaFrac(analyze1, "timing")).To(BeNumerically("<", 0))
		concl1 := runCLIJSON[cliIDResponse](dir,
			"conclude", hyp1.ID,
			"--verdict", "supported",
			"--baseline-experiment", baseline.ID,
			"--observations", observeResultID(obs1, "timing"),
		)
		runCLIJSON[cliIDResponse](dir,
			"conclusion", "accept", concl1.ID,
			"--reviewed-by", "human:gate",
			"--rationale", "Stats confirmed. Code matches the mechanism. No gaming or metric manipulation was detected.",
		)

		hyp2 := runCLIJSON[cliIDResponse](dir,
			"hypothesis", "add",
			"--claim", "a smaller tweak might still help",
			"--predicts-instrument", "timing",
			"--predicts-target", "kernel",
			"--predicts-direction", "decrease",
			"--predicts-min-effect", "0.05",
			"--kill-if", "tests fail",
		)
		exp2 := runCLIJSON[cliIDResponse](dir,
			"experiment", "design", hyp2.ID,
			"--baseline", "HEAD",
			"--instruments", "timing,binary_size,host_test",
		)
		impl2 := runCLIJSON[cliImplementResponse](dir, "experiment", "implement", exp2.ID)
		writeScenarioMetrics(impl2.Worktree, "95\n", "900\n")
		gitCommitAll(impl2.Worktree, "small tweak")
		candidateRef2 := gitCreateCandidateRef(impl2.Worktree, "candidate/lifecycle-e2")

		obs2 := runCLIJSON[cliObserveAllResponse](dir, "observe", exp2.ID, "--all", "--candidate-ref", candidateRef2)
		runCLIJSON[cliIDResponse](dir,
			"conclude", hyp2.ID,
			"--verdict", "inconclusive",
			"--baseline-experiment", baseline.ID,
			"--observations", observeResultID(obs2, "timing"),
		)

		dead := runCLIJSON[[]cliExperimentListRow](dir, "experiment", "list", "--goal", goal.ID, "--classification", experimentClassificationDead)
		Expect(dead).To(HaveLen(1))
		Expect(dead[0].ID).To(Equal(exp1.ID))
		Expect(dead[0].HypothesisStatus).To(Equal("supported"))

		frontier := runCLIJSON[cliFrontierResponse](dir, "frontier", "--goal", goal.ID)
		Expect(frontier.ScopeGoalID).To(Equal(goal.ID))
		Expect(frontier.GoalID).To(Equal(goal.ID))
		Expect(frontier.GoalAssessment.Met).To(BeTrue())
		Expect(frontier.GoalAssessment.MetByConclusion).To(Equal(concl1.ID))
		Expect(frontier.StalledFor).To(Equal(1))
		Expect(frontier.Frontier).To(HaveLen(1))
		Expect(frontier.Frontier[0].Candidate).To(Equal(exp1.ID))
		Expect(frontier.Frontier[0].Conclusion).To(Equal(concl1.ID))
		Expect(frontier.Frontier[0].Classification).To(Equal(experimentClassificationDead))
		Expect(frontier.Frontier[0].HypothesisStatus).To(Equal("supported"))

		dashboard := runCLIJSON[cliDashboardResponse](dir, "dashboard", "--goal", goal.ID)
		Expect(dashboard.ScopeGoalID).To(Equal(goal.ID))
		Expect(dashboard.StalledFor).To(Equal(frontier.StalledFor))
		Expect(dashboard.Counts).To(HaveKeyWithValue("hypotheses", 2))
		Expect(dashboard.Counts).To(HaveKeyWithValue("experiments", 3))
		Expect(dashboard.Counts).To(HaveKeyWithValue("observations", 9))
		Expect(dashboard.Counts).To(HaveKeyWithValue("conclusions", 2))
		Expect(dashboard.Frontier).To(HaveLen(1))
		Expect(dashboard.Frontier[0].Candidate).To(Equal(exp1.ID))
		Expect(dashboard.Frontier[0].Conclusion).To(Equal(concl1.ID))
		Expect(dashboard.Frontier[0].Classification).To(Equal(experimentClassificationDead))

		status := runCLIJSON[cliStatusResponse](dir, "status", "--goal", goal.ID)
		Expect(status.ScopeGoalID).To(Equal(goal.ID))
		Expect(status.MainCheckoutDirty).To(BeFalse())
		Expect(status.Counts).To(HaveKeyWithValue("hypotheses", 2))
		Expect(status.Counts).To(HaveKeyWithValue("experiments", 3))
		Expect(status.Counts).To(HaveKeyWithValue("observations", 9))
		Expect(status.Counts).To(HaveKeyWithValue("conclusions", 2))
	})

	It("keeps observation evidence artifacts visible through the conclusion audit chain", func() {
		dir := gitInitScenarioRepo()
		_, err := store.Create(dir, store.Config{
			Build:     store.CommandSpec{Command: "true"},
			Test:      store.CommandSpec{Command: "true"},
			Worktrees: store.WorktreesConfig{Root: filepath.Join(GinkgoT().TempDir(), "worktrees")},
		})
		Expect(err).NotTo(HaveOccurred())
		writeScenarioMechanism(dir, "baseline trace\n")
		gitRun(dir, "add", "mechanism.txt")
		gitRun(dir, "commit", "-m", "add mechanism trace")

		registerScenarioTimingInstrument(dir, "mechanism=cat mechanism.txt")
		registerScenarioSupportInstruments(dir)

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
			"--instruments", "timing,binary_size,host_test",
		)
		impl := runCLIJSON[cliImplementResponse](dir, "experiment", "implement", exp.ID)
		writeScenarioMetrics(impl.Worktree, "80\n", "900\n")
		writeScenarioMechanism(impl.Worktree, "candidate trace\n")
		gitRun(impl.Worktree, "add", "timing.txt", "size.txt", "mechanism.txt")
		gitRun(impl.Worktree, "commit", "-m", "improve timing")
		candidateRef := gitCreateCandidateRef(impl.Worktree, "candidate/evidence-e1")

		obs := runCLIJSON[cliObserveAllResponse](dir, "observe", exp.ID, "--all", "--candidate-ref", candidateRef)
		timingObsID := observeResultID(obs, "timing")
		concl := runCLIJSON[cliIDResponse](dir,
			"conclude", hyp.ID,
			"--verdict", "supported",
			"--baseline-experiment", baseline.ID,
			"--observations", timingObsID,
		)
		show := runCLIJSON[conclusionShowJSON](dir, "conclusion", "show", concl.ID)
		arts, ok := show.ObservationArtifacts[timingObsID]
		Expect(ok).To(BeTrue(), "observation_artifacts missing timing observation %s: %+v", timingObsID, show.ObservationArtifacts)
		Expect(arts).To(HaveLen(2))
		foundEvidence := false
		for _, art := range arts {
			if art.Name == "evidence/mechanism" {
				foundEvidence = true
				Expect(art.SHA).NotTo(BeEmpty())
				Expect(art.Path).NotTo(BeEmpty())
				Expect(art.Bytes).To(BeNumerically(">", 0))
			}
		}
		Expect(foundEvidence).To(BeTrue(), "evidence artifact missing from conclusion show: %+v", arts)

		s, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		firstObs, err := s.ReadObservation(timingObsID)
		Expect(err).NotTo(HaveOccurred())
		Expect(firstObs.EvidenceFailures).To(BeEmpty())

		registerScenarioTimingInstrument(dir, "broken=echo nope >&2; exit 7")

		hyp2 := runCLIJSON[cliIDResponse](dir,
			"hypothesis", "add",
			"--claim", "a smaller tweak might still help",
			"--predicts-instrument", "timing",
			"--predicts-target", "kernel",
			"--predicts-direction", "decrease",
			"--predicts-min-effect", "0.05",
			"--kill-if", "tests fail",
		)
		exp2 := runCLIJSON[cliIDResponse](dir,
			"experiment", "design", hyp2.ID,
			"--baseline", "HEAD",
			"--instruments", "timing",
		)
		impl2 := runCLIJSON[cliImplementResponse](dir, "experiment", "implement", exp2.ID)
		writeScenarioMetrics(impl2.Worktree, "95\n", "900\n")
		gitCommitAll(impl2.Worktree, "small tweak")
		candidateRef2 := gitCreateCandidateRef(impl2.Worktree, "candidate/evidence-e2")
		obs2 := runCLIJSON[cliIDResponse](dir, "observe", exp2.ID, "--instrument", "timing", "--candidate-ref", candidateRef2)
		concl2 := runCLIJSON[cliIDResponse](dir,
			"conclude", hyp2.ID,
			"--verdict", "inconclusive",
			"--baseline-experiment", baseline.ID,
			"--observations", obs2.ID,
		)

		secondObs, err := s.ReadObservation(obs2.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(secondObs.EvidenceFailures).To(HaveLen(1))
		Expect(secondObs.EvidenceFailures[0].Name).To(Equal("broken"))
		Expect(secondObs.EvidenceFailures[0].ExitCode).To(Equal(7))
		concl2Entity, err := s.ReadConclusion(concl2.ID)
		Expect(err).NotTo(HaveOccurred())
		concl2Entity.Observations = append(concl2Entity.Observations, "O-9999")
		Expect(s.WriteConclusion(concl2Entity)).To(Succeed())
		show2 := runCLIJSON[conclusionShowJSON](dir, "conclusion", "show", concl2.ID)
		Expect(show2.ObservationEvidenceFailures[obs2.ID]).To(HaveLen(1))
		Expect(show2.ObservationEvidenceFailures[obs2.ID][0].Name).To(Equal("broken"))
		Expect(show2.ObservationEvidenceFailures[obs2.ID][0].ExitCode).To(Equal(7))
		Expect(show2.ObservationReadIssues).To(HaveKeyWithValue("O-9999", "observation not found"))
		e := findLastEvent(s, "observation.record")
		Expect(e).NotTo(BeNil())
		payload := decodePayload(e)
		failures, ok := payload["evidence_failures"].([]any)
		Expect(ok).To(BeTrue())
		Expect(failures).To(HaveLen(1))
		failure, ok := failures[0].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(failure).To(HaveKeyWithValue("name", "broken"))
		Expect(failure).To(HaveKeyWithValue("exit_code", float64(7)))
		Expect(goal.ID).NotTo(BeEmpty())
	})
})

func registerScenarioInstruments(dir string) {
	GinkgoHelper()
	registerScenarioTimingInstrument(dir)
	registerScenarioSupportInstruments(dir)
}

func registerScenarioTimingInstrument(dir string, evidence ...string) {
	GinkgoHelper()
	args := []string{
		"instrument", "register", "timing",
		"--cmd", "sh",
		"--cmd", "-c",
		"--cmd", "cat timing.txt",
		"--parser", "builtin:scalar",
		"--pattern", "([0-9]+)",
		"--unit", "ns",
	}
	for _, ev := range evidence {
		args = append(args, "--evidence", ev)
	}
	runCLI(dir, args...)
}

func registerScenarioSupportInstruments(dir string) {
	GinkgoHelper()
	runCLI(dir,
		"instrument", "register", "binary_size",
		"--cmd", "sh",
		"--cmd", "-c",
		"--cmd", "cat size.txt",
		"--parser", "builtin:scalar",
		"--pattern", "([0-9]+)",
		"--unit", "bytes",
	)
	runCLI(dir,
		"instrument", "register", "host_test",
		"--cmd", "sh",
		"--cmd", "-c",
		"--cmd", "test -f PASS",
		"--parser", "builtin:passfail",
		"--unit", "bool",
	)
}

func gitInitScenarioRepo() string {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		gitRun(dir, args...)
	}
	writeScenarioMetrics(dir, "100\n", "900\n")
	Expect(os.WriteFile(filepath.Join(dir, "PASS"), []byte("ok\n"), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(dir, "README.md"), []byte("baseline\n"), 0o644)).To(Succeed())
	gitRun(dir, "add", "timing.txt", "size.txt", "PASS", "README.md")
	gitRun(dir, "commit", "-m", "init")
	return dir
}

func writeScenarioMetrics(dir, timing, size string) {
	GinkgoHelper()
	Expect(os.WriteFile(filepath.Join(dir, "timing.txt"), []byte(timing), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(dir, "size.txt"), []byte(size), 0o644)).To(Succeed())
}

func writeScenarioMechanism(dir, content string) {
	GinkgoHelper()
	Expect(os.WriteFile(filepath.Join(dir, "mechanism.txt"), []byte(content), 0o644)).To(Succeed())
}

func gitCommitAll(dir, msg string) {
	GinkgoHelper()
	gitRun(dir, "add", "timing.txt", "size.txt")
	gitRun(dir, "commit", "-m", msg)
}

func gitCreateCandidateRef(dir, ref string) string {
	GinkgoHelper()
	gitRun(dir, "branch", ref, "HEAD")
	return ref
}

func gitRun(dir string, args ...string) {
	GinkgoHelper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "git %v failed:\n%s", args, out)
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
