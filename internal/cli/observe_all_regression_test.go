package cli

import (
	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("observe --all regressions", func() {
	BeforeEach(saveGlobals)

	It("enforces each instrument's strict min_samples", func() {
		dir := setupObserveScenarioStore()
		runCLI(dir,
			"instrument", "register", "timing",
			"--cmd", "sh",
			"--cmd", "-c",
			"--cmd", "cat timing.txt",
			"--parser", "builtin:scalar",
			"--pattern", "([0-9]+)",
			"--unit", "ns",
			"--min-samples", "5",
		)
		scenario := setupObserveScenarioExperiment(dir, "timing", "--constraint-max", "timing=1000")
		writeScenarioMetrics(scenario.Worktree, "80\n", "900\n")
		gitCommitAll(scenario.Worktree, "candidate")
		candidateRef := gitCreateCandidateRef(scenario.Worktree, "candidate/observe-all-min-samples")

		_, _, err := runCLIResult(dir,
			"observe", scenario.ExpID,
			"--all",
			"--candidate-ref", candidateRef,
			"--samples", "1",
		)
		Expect(err).To(MatchError(ContainSubstring("requires at least 5 samples")))
	})

	It("requires implemented experiment status before recording", func() {
		dir := setupObserveScenarioStore()
		registerScenarioTimingInstrument(dir)
		scenario := setupObserveScenarioExperiment(dir, "timing", "--constraint-max", "timing=1000")
		writeScenarioMetrics(scenario.Worktree, "80\n", "900\n")
		gitCommitAll(scenario.Worktree, "candidate")
		candidateRef := gitCreateCandidateRef(scenario.Worktree, "candidate/observe-all-status")

		s, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		exp, err := s.ReadExperiment(scenario.ExpID)
		Expect(err).NotTo(HaveOccurred())
		exp.Status = entity.ExpDesigned
		Expect(s.WriteExperiment(exp)).To(Succeed())

		_, _, err = runCLIResult(dir,
			"observe", scenario.ExpID,
			"--all",
			"--candidate-ref", candidateRef,
		)
		Expect(err).To(MatchError(And(ContainSubstring("status"), ContainSubstring("implemented"))))
	})
})
