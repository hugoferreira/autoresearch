package cli

import (
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/worktree"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("review-packet command", func() {
	BeforeEach(saveGlobals)

	It("aggregates conclusion review evidence without mutating state", func() {
		dir := setupObserveScenarioStore()
		s, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		now := time.Now().UTC()
		baseSHA, err := worktree.ResolveRef(dir, "HEAD")
		Expect(err).NotTo(HaveOccurred())
		writeScenarioMetrics(dir, "90\n", "880\n")
		gitRun(dir, "add", "timing.txt", "size.txt")
		gitRun(dir, "commit", "-m", "candidate")
		candidateSHA, err := worktree.ResolveRef(dir, "HEAD")
		Expect(err).NotTo(HaveOccurred())

		maxSize := 1000.0
		Expect(s.WriteGoal(&entity.Goal{
			ID:        "G-0001",
			Status:    entity.GoalStatusActive,
			CreatedAt: &now,
			Objective: entity.Objective{Instrument: "timing", Direction: "decrease"},
			Constraints: []entity.Constraint{
				{Instrument: "binary_size", Max: &maxSize},
				{Instrument: "host_test", Require: "pass"},
			},
		})).To(Succeed())
		Expect(s.UpdateState(func(st *store.State) error {
			st.CurrentGoalID = "G-0001"
			st.Counters["G"] = 1
			return nil
		})).To(Succeed())
		Expect(s.WriteHypothesis(&entity.Hypothesis{
			ID:        "H-0001",
			GoalID:    "G-0001",
			Claim:     "tighten the hot loop",
			Predicts:  entity.Predicts{Instrument: "timing", Target: "kernel", Direction: "decrease", MinEffect: 0.1},
			KillIf:    []string{"tests fail"},
			Status:    entity.StatusUnreviewed,
			Author:    "agent:orchestrator",
			CreatedAt: now,
			Body:      "# Rationale\n\ncandidate should lower timing.\n",
		})).To(Succeed())
		body := entity.AppendMarkdownSection("", "Design notes", "measure timing and required constraints")
		body = entity.AppendMarkdownSection(body, "Implementation notes", "tightened the loop body")
		Expect(s.WriteExperiment(&entity.Experiment{
			ID:         "E-0001",
			GoalID:     "G-0001",
			Hypothesis: "H-0001",
			Status:     entity.ExpAnalyzed,
			Baseline:   entity.Baseline{Ref: "HEAD", SHA: baseSHA},
			Instruments: []string{
				"timing", "binary_size", "host_test",
			},
			Worktree:  dir,
			Branch:    "candidate/review-packet",
			Attempt:   1,
			Author:    "agent:orchestrator",
			CreatedAt: now,
			Body:      body,
		})).To(Succeed())
		Expect(s.WriteExperiment(&entity.Experiment{
			ID:         "E-0000",
			GoalID:     "G-0001",
			IsBaseline: true,
			Status:     entity.ExpMeasured,
			Baseline:   entity.Baseline{Ref: "HEAD", SHA: baseSHA},
			Instruments: []string{
				"timing", "binary_size", "host_test",
			},
			Author:    "system",
			CreatedAt: now,
		})).To(Succeed())
		artifact := writeArtifactFixture(s, "timing.json", []byte(`{"samples":[90,91,89]}`), "primary", "application/json")
		pass := true
		Expect(s.WriteObservation(&entity.Observation{
			ID:           "O-0001",
			Experiment:   "E-0001",
			Instrument:   "timing",
			MeasuredAt:   now,
			Value:        90,
			Unit:         "ns",
			Samples:      3,
			PerSample:    []float64{90, 91, 89},
			Artifacts:    []entity.Artifact{artifact},
			CandidateRef: "refs/heads/candidate/review-packet",
			CandidateSHA: candidateSHA,
			BaselineSHA:  baseSHA,
			Command:      "cat timing.txt",
			Author:       "agent:observer",
			Aux: map[string]any{
				entity.ObservationAuxPair: readmodel.PairedObservationMeta{
					PairID:              "P-0001",
					Mode:                "bracket",
					Arm:                 readmodel.PairArmCandidate,
					Segment:             readmodel.PairSegmentCandidate,
					Order:               2,
					Instrument:          "timing",
					CandidateExperiment: "E-0001",
					CandidateRef:        "refs/heads/candidate/review-packet",
					CandidateSHA:        candidateSHA,
					BaselineExperiment:  "E-0000",
					BaselineRef:         "HEAD",
					BaselineSHA:         baseSHA,
				},
			},
		})).To(Succeed())
		Expect(s.WriteObservation(&entity.Observation{
			ID:           "O-0002",
			Experiment:   "E-0001",
			Instrument:   "host_test",
			MeasuredAt:   now,
			Value:        1,
			Unit:         "bool",
			Samples:      1,
			Pass:         &pass,
			CandidateRef: "refs/heads/candidate/review-packet",
			CandidateSHA: candidateSHA,
			BaselineSHA:  baseSHA,
			Command:      "test -f PASS",
			Author:       "agent:observer",
		})).To(Succeed())
		Expect(s.WriteConclusion(&entity.Conclusion{
			ID:               "C-0001",
			Hypothesis:       "H-0001",
			Verdict:          entity.VerdictSupported,
			Observations:     []string{"O-0001", "O-0002"},
			CandidateExp:     "E-0001",
			CandidateAttempt: 1,
			CandidateRef:     "refs/heads/candidate/review-packet",
			CandidateSHA:     candidateSHA,
			BaselineExp:      "E-0000",
			BaselineAttempt:  1,
			BaselineRef:      "HEAD",
			BaselineSHA:      baseSHA,
			Effect: entity.Effect{
				Instrument: "timing",
				DeltaAbs:   -10,
				DeltaFrac:  -0.1,
				CILowFrac:  -0.12,
				CIHighFrac: -0.08,
				PValue:     0.01,
				CIMethod:   "bootstrap_bca_95",
				NCandidate: 3,
				NBaseline:  3,
			},
			SecondaryChecks: []entity.ClauseCheck{{Role: "require", Instrument: "host_test", Passed: true}},
			StatTest:        "mann_whitney_u",
			Strict:          entity.Strict{Passed: true},
			Author:          "agent:orchestrator",
			CreatedAt:       now,
		})).To(Succeed())

		got := runCLIJSON[readmodel.ReviewPacket](dir, "review-packet", "C-0001")
		Expect(got.Conclusion.ID).To(Equal("C-0001"))
		Expect(got.Goal.ID).To(Equal("G-0001"))
		Expect(got.Hypothesis.Claim).To(Equal("tighten the hot loop"))
		Expect(got.CandidateExperiment.ID).To(Equal("E-0001"))
		Expect(got.BaselineExperiment.ID).To(Equal("E-0000"))
		Expect(got.Observations).To(HaveLen(2))
		Expect(got.Observations[0].Pair.PairID).To(Equal("P-0001"))
		Expect(got.Observations[0].Artifacts[0].Readable).To(BeTrue())
		Expect(got.ConstraintChecks).To(ConsistOf(HaveField("Instrument", "host_test")))
		Expect(got.Analysis.Command).To(ContainElement("--candidate-ref"))
		Expect(got.Diff.Files).To(ContainElements("timing.txt", "size.txt"))
		Expect(got.ReadIssues).To(BeEmpty())

		runCLI(dir, "pause", "--reason", "gate review")
		text := runCLI(dir, "review-packet", "C-0001")
		expectText(text, "review_packet: C-0001", "claim:         tighten the hot loop", "constraint_checks:", "analysis:", "diff:", "pair=P-0001 arm=candidate", "artifact primary")
		expectNoText(text, "read_issues:")
	})

	It("surfaces missing observations and unreadable artifacts in JSON and text", func() {
		dir, s := createCLIStoreDir()
		now := time.Now().UTC()
		Expect(s.WriteObservation(&entity.Observation{
			ID:         "O-0001",
			Experiment: "E-0001",
			Instrument: "timing",
			MeasuredAt: now,
			Value:      100,
			Unit:       "ns",
			Samples:    1,
			Artifacts: []entity.Artifact{{
				Name:  "primary",
				SHA:   "abcdef123456",
				Path:  "artifacts/ab/missing/timing.json",
				Bytes: 10,
				Mime:  "application/json",
			}},
			Command: "cat timing.txt",
			Author:  "agent:observer",
		})).To(Succeed())
		Expect(s.WriteConclusion(&entity.Conclusion{
			ID:           "C-0001",
			Hypothesis:   "H-0001",
			Verdict:      entity.VerdictSupported,
			Observations: []string{"O-0001", "O-9999"},
			CandidateExp: "E-0001",
			Effect: entity.Effect{
				Instrument: "timing",
				DeltaFrac:  -0.1,
				CIMethod:   "bootstrap_bca_95",
			},
			StatTest:  "mann_whitney_u",
			Strict:    entity.Strict{Passed: true},
			Author:    "agent:orchestrator",
			CreatedAt: now,
		})).To(Succeed())

		got := runCLIJSON[readmodel.ReviewPacket](dir, "review-packet", "C-0001")
		var issues []string
		for _, issue := range got.ReadIssues {
			issues = append(issues, issue.Kind+" "+issue.Subject+" "+issue.Message)
		}
		Expect(strings.Join(issues, "\n")).To(ContainSubstring("artifact O-0001/primary"))
		Expect(strings.Join(issues, "\n")).To(ContainSubstring("observation O-9999 observation not found"))
		Expect(got.Observations[0].Artifacts[0].Readable).To(BeFalse())
		Expect(got.Observations[1].ReadIssue).To(Equal("observation not found"))

		text := runCLI(dir, "review-packet", "C-0001")
		expectText(text, "read_issues:", "artifact O-0001/primary", "observation O-9999: observation not found", "artifact primary")
	})
})
