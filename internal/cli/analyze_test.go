package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("analyze command", func() {
	BeforeEach(saveGlobals)

	It("uses a stored candidate ref even after the branch is deleted", func() {
		dir := setupObserveScenarioStore()
		registerScenarioInstruments(dir)
		scenario := setupObserveScenarioExperiment(dir, "timing", "--constraint-max", "binary_size=1000")

		writeScenarioMetrics(scenario.Worktree, "90\n", "900\n")
		gitCommitAll(scenario.Worktree, "candidate a")
		candidateRef := gitCreateCandidateRef(scenario.Worktree, "candidate/analyze-deleted-ref")
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

		writeScenarioMetrics(impl.Worktree, "90\n", "900\n")
		gitCommitAll(impl.Worktree, "candidate a")
		candidateRef := gitCreateCandidateRef(impl.Worktree, "candidate/analyze-ambiguous-baseline")
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

	It("rejects filtering observations that mix candidate attempts", func() {
		obs := []*entity.Observation{
			{
				ID:           "O-0001",
				Attempt:      1,
				CandidateRef: "refs/heads/candidate/E-0001-a1",
				CandidateSHA: "0123456789abcdef0123456789abcdef01234567",
			},
			{
				ID:           "O-0002",
				Attempt:      2,
				CandidateRef: "refs/heads/candidate/E-0001-a1",
				CandidateSHA: "0123456789abcdef0123456789abcdef01234567",
			},
		}

		_, err := filterAnalyzeObservationsByCandidateRef(obs, "refs/heads/candidate/E-0001-a1")
		Expect(err).To(MatchError(ContainSubstring("multiple recorded candidate scopes")))
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
