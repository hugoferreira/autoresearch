package cli

import (
	"errors"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
)

func setupApplyRegressionStore(t testkit.T, paused bool) string {
	t.Helper()

	dir := t.TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatalf("store.Create: %v", err)
	}

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
	if err := s.WriteHypothesis(h); err != nil {
		t.Fatalf("WriteHypothesis: %v", err)
	}

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
		if err := s.WriteExperiment(e); err != nil {
			t.Fatalf("WriteExperiment(%s): %v", e.ID, err)
		}
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
		if err := s.WriteConclusion(c); err != nil {
			t.Fatalf("WriteConclusion(%s): %v", c.ID, err)
		}
	}

	if paused {
		if err := s.UpdateState(func(st *store.State) error {
			st.Paused = true
			st.PauseReason = "review pending"
			return nil
		}); err != nil {
			t.Fatalf("UpdateState(paused): %v", err)
		}
	}

	return dir
}

var _ = testkit.Spec("TestHypothesisApplySelectsReviewedConclusionOverStrongerUnreviewedOne", func(t testkit.T) {
	saveGlobals(t)
	dir := setupApplyRegressionStore(t, false)

	stdout, stderr, err := runCLIResult(t, dir, "--dry-run", "hypothesis", "apply", "H-0001")
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("hypothesis apply error = %v, want dry-run after selecting reviewed conclusion\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "C-0001") || strings.Contains(stdout, "C-0002") {
		t.Fatalf("hypothesis apply should select reviewed C-0001 and ignore unreviewed C-0002\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
})

var _ = testkit.Spec("TestHypothesisApplyRejectsExplicitUnreviewedConclusion", func(t testkit.T) {
	saveGlobals(t)
	dir := setupApplyRegressionStore(t, false)

	stdout, stderr, err := runCLIResult(t, dir, "--dry-run", "hypothesis", "apply", "H-0001", "--conclusion", "C-0002")
	if err == nil {
		t.Fatalf("hypothesis apply unexpectedly accepted unreviewed conclusion\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if errors.Is(err, ErrDryRun) {
		t.Fatalf("hypothesis apply reached dry-run instead of rejecting unreviewed conclusion\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(err.Error(), "review") {
		t.Fatalf("hypothesis apply error = %v, want review gate error\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
})

var _ = testkit.Spec("TestHypothesisApplyHonorsPausedStore", func(t testkit.T) {
	saveGlobals(t)
	dir := setupApplyRegressionStore(t, true)

	stdout, stderr, err := runCLIResult(t, dir, "--dry-run", "hypothesis", "apply", "H-0001", "--conclusion", "C-0001")
	if !errors.Is(err, ErrPaused) {
		t.Fatalf("hypothesis apply error = %v, want ErrPaused\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
})
