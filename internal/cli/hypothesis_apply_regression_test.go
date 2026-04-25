package cli

import (
	"errors"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func setupApplyRegressionStore(paused bool) string {
	GinkgoHelper()

	dir := GinkgoT().TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	Expect(err).NotTo(HaveOccurred())

	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	h := &entity.Hypothesis{
		ID:        "H-0001",
		GoalID:    "G-0001",
		Claim:     "tighten the hot loop",
		Status:    entity.StatusSupported,
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
	Expect(s.WriteHypothesis(h)).To(Succeed())

	exps := []*entity.Experiment{
		{
			ID:         "E-0001",
			GoalID:     h.GoalID,
			Hypothesis: h.ID,
			Status:     entity.ExpAnalyzed,
			Baseline:   entity.Baseline{Ref: "HEAD", SHA: strings.Repeat("a", 40)},
			Branch:     "autoresearch/E-0001",
			Attempt:    1,
			Author:     "agent:orchestrator",
			CreatedAt:  now,
		},
		{
			ID:         "E-0002",
			GoalID:     h.GoalID,
			Hypothesis: h.ID,
			Status:     entity.ExpAnalyzed,
			Baseline:   entity.Baseline{Ref: "HEAD", SHA: strings.Repeat("a", 40)},
			Branch:     "autoresearch/E-0002",
			Attempt:    1,
			Author:     "agent:orchestrator",
			CreatedAt:  now,
		},
	}
	for _, e := range exps {
		Expect(s.WriteExperiment(e)).To(Succeed())
	}

	concls := []*entity.Conclusion{
		{
			ID:           "C-0001",
			Hypothesis:   h.ID,
			Verdict:      entity.VerdictSupported,
			CandidateExp: "E-0001",
			CandidateSHA: strings.Repeat("b", 40),
			Effect:       entity.Effect{Instrument: "timing", DeltaFrac: -0.10},
			StatTest:     "mann_whitney_u",
			Strict:       entity.Strict{Passed: true},
			Author:       "agent:analyst",
			ReviewedBy:   "human:gate",
			CreatedAt:    now,
		},
		{
			ID:           "C-0002",
			Hypothesis:   h.ID,
			Verdict:      entity.VerdictSupported,
			CandidateExp: "E-0002",
			CandidateSHA: strings.Repeat("c", 40),
			Effect:       entity.Effect{Instrument: "timing", DeltaFrac: -0.30},
			StatTest:     "mann_whitney_u",
			Strict:       entity.Strict{Passed: true},
			Author:       "agent:analyst",
			CreatedAt:    now.Add(time.Minute),
		},
	}
	for _, c := range concls {
		Expect(s.WriteConclusion(c)).To(Succeed())
	}

	if paused {
		Expect(s.UpdateState(func(st *store.State) error {
			st.Paused = true
			st.PauseReason = "review pending"
			return nil
		})).To(Succeed())
	}

	return dir
}

var _ = Describe("hypothesis apply review gate", func() {
	BeforeEach(saveGlobals)

	It("selects a reviewed conclusion over a stronger unreviewed one", func() {
		dir := setupApplyRegressionStore(false)

		stdout, _, err := runCLIResult(dir, "--dry-run", "hypothesis", "apply", "H-0001")
		Expect(errors.Is(err, ErrDryRun)).To(BeTrue(), "expected dry-run after selecting reviewed conclusion, got %v", err)
		Expect(stdout).To(ContainSubstring("C-0001"))
		Expect(stdout).NotTo(ContainSubstring("C-0002"))
	})

	It("rejects an explicitly selected unreviewed conclusion", func() {
		dir := setupApplyRegressionStore(false)

		_, _, err := runCLIResult(dir, "--dry-run", "hypothesis", "apply", "H-0001", "--conclusion", "C-0002")
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, ErrDryRun)).To(BeFalse())
		Expect(err).To(MatchError(ContainSubstring("review")))
	})

	It("honors the pause gate", func() {
		dir := setupApplyRegressionStore(true)

		_, _, err := runCLIResult(dir, "--dry-run", "hypothesis", "apply", "H-0001", "--conclusion", "C-0001")
		Expect(errors.Is(err, ErrPaused)).To(BeTrue(), "expected ErrPaused, got %v", err)
	})
})
