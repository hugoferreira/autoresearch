package cli

import (
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("conclusion lint", func() {
	BeforeEach(saveGlobals)

	It("works while paused and warns about same-scope appended observations that are not cited", func() {
		dir, s := setupConclusionLintFixture()
		writeLintObservation(s, "O-0001", "E-0001", "timing", "refs/heads/candidate", "aaaaaaaa", true, nil, nil)
		writeLintObservation(s, "O-0002", "E-0001", "timing", "refs/heads/candidate", "aaaaaaaa", true, nil, nil)
		writeLintObservation(s, "O-0003", "E-0000", "timing", "refs/heads/baseline", "bbbbbbbb", true, nil, nil)
		writeLintConclusion(s, &entity.Conclusion{
			ID:           "C-0001",
			Hypothesis:   "H-0001",
			Verdict:      entity.VerdictSupported,
			Observations: []string{"O-0001"},
			CandidateExp: "E-0001",
			CandidateRef: "refs/heads/candidate",
			CandidateSHA: "aaaaaaaa",
			BaselineExp:  "E-0000",
			BaselineRef:  "refs/heads/baseline",
			BaselineSHA:  "bbbbbbbb",
			Effect:       entity.Effect{Instrument: "timing", DeltaFrac: -0.1, CIMethod: "bootstrap_bca_95"},
			Strict:       entity.Strict{Passed: true},
		})

		runCLI(dir, "pause", "--reason", "review")
		got := runCLIJSON[readmodel.ConclusionLintReport](dir, "conclusion", "lint", "C-0001")
		Expect(got.OK).To(BeFalse())
		Expect(lintCodes(got)).To(ContainElement("relevant_observation_not_cited"))
		text := runCLI(dir, "conclusion", "lint", "C-0001")
		expectText(text, "lint C-0001: issues", "relevant_observation_not_cited", "O-0002")
	})

	It("classifies no configured, failed, and unreadable mechanism evidence separately", func() {
		dir, s := setupConclusionLintFixture()
		writeLintObservation(s, "O-0001", "E-0001", "timing", "refs/heads/candidate", "aaaaaaaa", true, nil, nil)
		writeLintObservation(s, "O-0002", "E-0001", "timing", "refs/heads/candidate", "aaaaaaaa", true, nil, []entity.EvidenceFailure{{
			Name:     "profile",
			ExitCode: 2,
			Error:    "profile failed",
		}})
		writeLintObservation(s, "O-0003", "E-0001", "timing", "refs/heads/candidate", "aaaaaaaa", true, []entity.Artifact{{
			Name:  "evidence/profile",
			SHA:   "abcdef123456",
			Path:  "artifacts/ab/missing/profile.txt",
			Bytes: 12,
		}}, nil)
		writeLintObservation(s, "O-0004", "E-0000", "timing", "refs/heads/baseline", "bbbbbbbb", true, nil, nil)

		writeMechanismConclusion(s, "C-0001", []string{"O-0001"})
		writeMechanismConclusion(s, "C-0002", []string{"O-0002"})
		writeMechanismConclusion(s, "C-0003", []string{"O-0003"})

		Expect(lintCodes(runCLIJSON[readmodel.ConclusionLintReport](dir, "conclusion", "lint", "C-0001"))).To(ContainElement("mechanism_evidence_not_configured"))
		Expect(lintCodes(runCLIJSON[readmodel.ConclusionLintReport](dir, "conclusion", "lint", "C-0002"))).To(ContainElement("evidence_capture_failed"))
		Expect(lintCodes(runCLIJSON[readmodel.ConclusionLintReport](dir, "conclusion", "lint", "C-0003"))).To(ContainElement("evidence_artifact_unreadable"))
	})

	It("reports candidate and baseline ref mismatches plus missing required constraints", func() {
		dir, s := setupConclusionLintFixture()
		writeLintObservation(s, "O-0001", "E-0001", "timing", "refs/heads/other-candidate", "cccccccc", true, nil, nil)
		writeLintObservation(s, "O-0002", "E-0000", "timing", "refs/heads/other-baseline", "dddddddd", true, nil, nil)
		writeLintConclusion(s, &entity.Conclusion{
			ID:           "C-0001",
			Hypothesis:   "H-0001",
			Verdict:      entity.VerdictSupported,
			Observations: []string{"O-0001"},
			CandidateExp: "E-0001",
			CandidateRef: "refs/heads/candidate",
			CandidateSHA: "aaaaaaaa",
			BaselineExp:  "E-0000",
			BaselineRef:  "refs/heads/baseline",
			BaselineSHA:  "bbbbbbbb",
			Effect:       entity.Effect{Instrument: "timing", DeltaFrac: -0.1, CIMethod: "bootstrap_bca_95"},
			Strict:       entity.Strict{Passed: true},
		})

		got := runCLIJSON[readmodel.ConclusionLintReport](dir, "conclusion", "lint", "C-0001")
		Expect(lintCodes(got)).To(ContainElements(
			"candidate_ref_mismatch",
			"candidate_sha_mismatch",
			"baseline_ref_mismatch",
			"baseline_sha_mismatch",
			"required_instrument_missing",
		))
	})

	It("includes a lint report in conclude JSON and text output", func() {
		dir := setupObserveScenarioStore()
		registerScenarioTimingInstrument(dir)
		runCLIJSON[cliIDResponse](dir,
			"goal", "set",
			"--objective-instrument", "timing",
			"--objective-target", "kernel",
			"--objective-direction", "decrease",
			"--constraint-max", "timing=1000",
		)
		hyp := runCLIJSON[cliIDResponse](dir,
			"hypothesis", "add",
			"--claim", "reduce profile hits",
			"--predicts-instrument", "timing",
			"--predicts-target", "kernel",
			"--predicts-direction", "decrease",
			"--predicts-min-effect", "0",
			"--kill-if", "tests fail",
		)
		exp := runCLIJSON[cliIDResponse](dir,
			"experiment", "design", hyp.ID,
			"--baseline", "HEAD",
			"--instruments", "timing",
		)
		impl := runCLIJSON[cliImplementResponse](dir, "experiment", "implement", exp.ID)
		ref := commitScenarioMetricsCandidate(impl.Worktree, "candidate/lint-json", "candidate", "90\n", "900\n")
		obs := runCLIJSON[cliObserveAllResponse](dir, "observe", exp.ID, "--all", "--candidate-ref", ref)

		json := runCLIJSON[struct {
			Lint readmodel.ConclusionLintReport `json:"lint"`
		}](dir,
			"conclude", hyp.ID,
			"--verdict", "supported",
			"--observations", strings.Join(obs.Observations, ","),
			"--interpretation", "profile hits dropped without a stored profile artifact",
		)
		Expect(lintCodes(json.Lint)).To(ContainElement("mechanism_evidence_not_configured"))

		hyp2 := runCLIJSON[cliIDResponse](dir,
			"hypothesis", "add",
			"--claim", "reduce profile hits again",
			"--predicts-instrument", "timing",
			"--predicts-target", "kernel",
			"--predicts-direction", "decrease",
			"--predicts-min-effect", "0",
			"--kill-if", "tests fail",
		)
		exp2 := runCLIJSON[cliIDResponse](dir,
			"experiment", "design", hyp2.ID,
			"--baseline", "HEAD",
			"--instruments", "timing",
		)
		impl2 := runCLIJSON[cliImplementResponse](dir, "experiment", "implement", exp2.ID)
		ref2 := commitScenarioMetricsCandidate(impl2.Worktree, "candidate/lint-text", "candidate 2", "80\n", "900\n")
		obs2 := runCLIJSON[cliObserveAllResponse](dir, "observe", exp2.ID, "--all", "--candidate-ref", ref2)
		text := runCLI(dir,
			"conclude", hyp2.ID,
			"--verdict", "supported",
			"--observations", strings.Join(obs2.Observations, ","),
			"--interpretation", "profile hits dropped without a stored profile artifact",
		)
		expectText(text, "LINT WARNINGS", "mechanism_evidence_not_configured")
	})
})

func setupConclusionLintFixture() (string, *store.Store) {
	GinkgoHelper()
	dir, s := setupGoalStore()
	now := time.Now().UTC()
	Expect(s.WriteHypothesis(&entity.Hypothesis{
		ID:        "H-0001",
		GoalID:    "G-0001",
		Claim:     "reduce timing",
		Predicts:  entity.Predicts{Instrument: "timing", Target: "kernel", Direction: "decrease", MinEffect: 0.1},
		KillIf:    []string{"tests fail"},
		Status:    entity.StatusUnreviewed,
		Author:    "agent:orchestrator",
		CreatedAt: now,
	})).To(Succeed())
	for _, exp := range []*entity.Experiment{
		{ID: "E-0001", GoalID: "G-0001", Hypothesis: "H-0001", Status: entity.ExpAnalyzed, Instruments: []string{"timing", "host_test"}, Author: "agent:orchestrator", CreatedAt: now},
		{ID: "E-0000", GoalID: "G-0001", IsBaseline: true, Status: entity.ExpMeasured, Instruments: []string{"timing"}, Author: "system", CreatedAt: now},
	} {
		Expect(s.WriteExperiment(exp)).To(Succeed())
	}
	return dir, s
}

func writeLintObservation(s *store.Store, id, exp, instrument, ref, sha string, pass bool, artifacts []entity.Artifact, failures []entity.EvidenceFailure) {
	GinkgoHelper()
	now := time.Now().UTC()
	var passPtr *bool
	if instrument == "host_test" {
		passPtr = &pass
	}
	Expect(s.WriteObservation(&entity.Observation{
		ID:               id,
		Experiment:       exp,
		Instrument:       instrument,
		MeasuredAt:       now,
		Value:            1,
		Unit:             "unit",
		Samples:          1,
		Pass:             passPtr,
		Artifacts:        artifacts,
		EvidenceFailures: failures,
		CandidateRef:     ref,
		CandidateSHA:     sha,
		Command:          "true",
		Author:           "agent:observer",
	})).To(Succeed())
}

func writeLintConclusion(s *store.Store, c *entity.Conclusion) {
	GinkgoHelper()
	c.StatTest = "mann_whitney_u"
	c.Author = "agent:orchestrator"
	c.CreatedAt = time.Now().UTC()
	Expect(s.WriteConclusion(c)).To(Succeed())
}

func writeMechanismConclusion(s *store.Store, id string, observations []string) {
	GinkgoHelper()
	writeLintConclusion(s, &entity.Conclusion{
		ID:           id,
		Hypothesis:   "H-0001",
		Verdict:      entity.VerdictSupported,
		Observations: observations,
		CandidateExp: "E-0001",
		CandidateRef: "refs/heads/candidate",
		CandidateSHA: "aaaaaaaa",
		BaselineExp:  "E-0000",
		BaselineRef:  "refs/heads/baseline",
		BaselineSHA:  "bbbbbbbb",
		Effect:       entity.Effect{Instrument: "timing", DeltaFrac: -0.1, CIMethod: "bootstrap_bca_95"},
		Strict:       entity.Strict{Passed: true},
		Body:         "# Interpretation\n\nprofile hits and attempts dropped.\n",
	})
}

func lintCodes(report readmodel.ConclusionLintReport) []string {
	GinkgoHelper()
	return readmodel.ConclusionLintCodes(report.Issues)
}
