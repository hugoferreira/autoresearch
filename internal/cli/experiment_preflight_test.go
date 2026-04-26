package cli

import (
	"github.com/bytter/autoresearch/internal/readmodel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("experiment preflight", func() {
	BeforeEach(saveGlobals)

	It("passes a complete experiment design without mutating state", func() {
		dir := setupObserveScenarioStore()
		registerScenarioInstruments(dir)
		fixture := setupLifecycleFixture(dir)
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
			"--design-notes", "measure timing and guard the goal constraints",
		)

		report := runCLIJSON[readmodel.ExperimentPreflightReport](dir, "experiment", "preflight", exp.ID)

		Expect(report.OK).To(BeTrue())
		Expect(report.Errors).To(Equal(0))
		Expect(report.Issues).To(BeEmpty())
		Expect(report.Baseline).NotTo(BeNil())
		Expect(report.Baseline.ExperimentID).To(Equal(fixture.BaselineID))

		text := runCLI(dir, "experiment", "preflight", exp.ID)
		expectText(text, "preflight "+exp.ID+": ok", "ready for implementation")
	})

	It("reports missing measurement contracts and mechanism evidence warnings", func() {
		dir := setupObserveScenarioStore()
		registerScenarioInstruments(dir)
		setupLifecycleFixture(dir)
		hyp := runCLIJSON[cliIDResponse](dir,
			"hypothesis", "add",
			"--claim", "profile counters identify a tighter hot loop",
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
			"--design-notes", "Use profile counters to explain why timing moves.",
		)

		report := runCLIJSON[readmodel.ExperimentPreflightReport](dir, "experiment", "preflight", exp.ID)
		codes := preflightIssueCodes(report.Issues)

		Expect(report.OK).To(BeFalse())
		Expect(report.Errors).To(Equal(2))
		Expect(codes).To(ContainElement("missing_constraint_instrument"))
		Expect(codes).To(ContainElement("mechanism_evidence_unconfigured"))

		text := runCLI(dir, "experiment", "preflight", exp.ID)
		expectText(text, "missing_constraint_instrument", "mechanism_evidence_unconfigured")
	})
})

func preflightIssueCodes(issues []readmodel.ExperimentPreflightIssue) []string {
	codes := make([]string, 0, len(issues))
	for _, issue := range issues {
		codes = append(codes, issue.Code)
	}
	return codes
}
