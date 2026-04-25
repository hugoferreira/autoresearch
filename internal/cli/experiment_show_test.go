package cli

import (
	"strings"

	"github.com/bytter/autoresearch/internal/worktree"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type cliExperimentShowProjectionJSON struct {
	Worktree    string `json:"worktree,omitempty"`
	Branch      string `json:"branch,omitempty"`
	BaselineSHA string `json:"baseline_sha,omitempty"`
}

var _ = Describe("experiment show", func() {
	It("prints terse shell projections for implemented experiments", func() {
		dir, scenario := setupTimingObserveScenario()
		baselineSHA, err := worktree.ResolveRef(dir, "HEAD")
		Expect(err).NotTo(HaveOccurred())
		branch := "autoresearch/" + scenario.ExpID

		Expect(runCLI(dir, "experiment", "show", scenario.ExpID, "--worktree")).To(Equal(scenario.Worktree + "\n"))
		Expect(runCLI(dir, "experiment", "show", scenario.ExpID, "--branch")).To(Equal(branch + "\n"))
		Expect(runCLI(dir, "experiment", "show", scenario.ExpID, "--baseline-sha")).To(Equal(baselineSHA + "\n"))

		env := runCLI(dir, "experiment", "show", scenario.ExpID, "--env")
		Expect(env).To(Equal(strings.Join([]string{
			"WORKTREE=" + shellQuote(scenario.Worktree),
			"BRANCH=" + shellQuote(branch),
			"BASELINE_SHA=" + shellQuote(baselineSHA),
		}, "\n") + "\n"))
	})

	It("emits structured JSON for shell projections", func() {
		dir, scenario := setupTimingObserveScenario()
		baselineSHA, err := worktree.ResolveRef(dir, "HEAD")
		Expect(err).NotTo(HaveOccurred())
		branch := "autoresearch/" + scenario.ExpID

		worktreeOut := runCLIJSON[cliExperimentShowProjectionJSON](dir, "experiment", "show", scenario.ExpID, "--worktree")
		Expect(worktreeOut.Worktree).To(Equal(scenario.Worktree))
		Expect(worktreeOut.Branch).To(BeEmpty())
		Expect(worktreeOut.BaselineSHA).To(BeEmpty())

		branchOut := runCLIJSON[cliExperimentShowProjectionJSON](dir, "experiment", "show", scenario.ExpID, "--branch")
		Expect(branchOut.Branch).To(Equal(branch))
		Expect(branchOut.Worktree).To(BeEmpty())
		Expect(branchOut.BaselineSHA).To(BeEmpty())

		baselineOut := runCLIJSON[cliExperimentShowProjectionJSON](dir, "experiment", "show", scenario.ExpID, "--baseline-sha")
		Expect(baselineOut.BaselineSHA).To(Equal(baselineSHA))
		Expect(baselineOut.Worktree).To(BeEmpty())
		Expect(baselineOut.Branch).To(BeEmpty())

		envOut := runCLIJSON[cliExperimentShowProjectionJSON](dir, "experiment", "show", scenario.ExpID, "--env")
		Expect(envOut.Worktree).To(Equal(scenario.Worktree))
		Expect(envOut.Branch).To(Equal(branch))
		Expect(envOut.BaselineSHA).To(Equal(baselineSHA))
	})

	It("rejects multiple shell projections", func() {
		dir, scenario := setupTimingObserveScenario()

		_, _, err := runCLIResult(dir, "experiment", "show", scenario.ExpID, "--worktree", "--branch")
		Expect(err).To(MatchError(ContainSubstring("mutually exclusive")))
	})

	It("rejects worktree, branch, and env projections for unimplemented experiments", func() {
		dir := setupObserveScenarioStore()
		registerScenarioInstruments(dir)
		runCLIJSON[cliIDResponse](dir,
			"goal", "set",
			"--objective-instrument", "timing",
			"--objective-target", "kernel",
			"--objective-direction", "decrease",
			"--constraint-max", "binary_size=1000",
		)
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
		baselineSHA, err := worktree.ResolveRef(dir, "HEAD")
		Expect(err).NotTo(HaveOccurred())

		_, _, err = runCLIResult(dir, "experiment", "show", exp.ID, "--worktree")
		Expect(err).To(MatchError(ContainSubstring("has no worktree")))
		_, _, err = runCLIResult(dir, "experiment", "show", exp.ID, "--branch")
		Expect(err).To(MatchError(ContainSubstring("has no branch")))
		_, _, err = runCLIResult(dir, "experiment", "show", exp.ID, "--env")
		Expect(err).To(MatchError(ContainSubstring("has no worktree")))

		Expect(runCLI(dir, "experiment", "show", exp.ID, "--baseline-sha")).To(Equal(baselineSHA + "\n"))
	})

	It("quotes env values for POSIX shell evaluation", func() {
		Expect(shellQuote("")).To(Equal("''"))
		Expect(shellQuote("/tmp/work tree/root's/E-0001")).To(Equal("'/tmp/work tree/root'\\''s/E-0001'"))
	})
})
