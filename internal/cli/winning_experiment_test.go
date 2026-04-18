package cli

import (
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

func setupWinningExperimentStore(t *testing.T, hypStatus string) *store.Store {
	t.Helper()
	s := mustCreateCLIStore(t)
	now := time.Now().UTC()

	h := &entity.Hypothesis{
		ID:        "H-0001",
		Claim:     "tighten loop",
		Predicts:  entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
		KillIf:    []string{"tests fail"},
		Status:    hypStatus,
		Author:    "agent:analyst",
		CreatedAt: now,
	}
	if err := s.WriteHypothesis(h); err != nil {
		t.Fatal(err)
	}
	for _, e := range []*entity.Experiment{
		{ID: "E-0001", Hypothesis: h.ID, Status: entity.ExpAnalyzed, Branch: "autoresearch/E-0001", CreatedAt: now.Add(-1 * time.Minute)},
		{ID: "E-0002", Hypothesis: h.ID, Status: entity.ExpMeasured, Branch: "autoresearch/E-0002", CreatedAt: now},
	} {
		if err := s.WriteExperiment(e); err != nil {
			t.Fatal(err)
		}
	}
	c := &entity.Conclusion{
		ID:           "C-0001",
		Hypothesis:   h.ID,
		Verdict:      entity.VerdictSupported,
		CandidateExp: "E-0001",
		Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.2},
		ReviewedBy:   "agent:gate",
		Author:       "agent:analyst",
		CreatedAt:    now.Add(-30 * time.Second),
	}
	if err := s.WriteConclusion(c); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestResolveWinningExperiment_UsesSupportedWinnerWhenHypothesisIsCurrentlySupported(t *testing.T) {
	s := setupWinningExperimentStore(t, entity.StatusSupported)

	concl, exp, err := resolveWinningExperiment(s, "H-0001", "")
	if err != nil {
		t.Fatalf("resolveWinningExperiment: %v", err)
	}
	if concl == nil || concl.ID != "C-0001" {
		t.Fatalf("conclusion = %+v, want C-0001", concl)
	}
	if exp == nil || exp.ID != "E-0001" {
		t.Fatalf("experiment = %+v, want E-0001", exp)
	}
}

func TestResolveWinningExperiment_FallsBackToLatestExperimentWhenHypothesisIsNotCurrentlySupported(t *testing.T) {
	for _, status := range []string{entity.StatusRefuted, entity.StatusKilled, entity.StatusInconclusive, entity.StatusUnreviewed} {
		t.Run(status, func(t *testing.T) {
			s := setupWinningExperimentStore(t, status)

			concl, exp, err := resolveWinningExperiment(s, "H-0001", "")
			if err != nil {
				t.Fatalf("resolveWinningExperiment: %v", err)
			}
			if concl != nil {
				t.Fatalf("conclusion = %+v, want nil", concl)
			}
			if exp == nil || exp.ID != "E-0002" {
				t.Fatalf("experiment = %+v, want latest E-0002", exp)
			}
		})
	}
}

func TestResolveWinningExperiment_ExplicitHistoricalConclusionStillWorks(t *testing.T) {
	s := setupWinningExperimentStore(t, entity.StatusRefuted)

	concl, exp, err := resolveWinningExperiment(s, "H-0001", "C-0001")
	if err != nil {
		t.Fatalf("resolveWinningExperiment: %v", err)
	}
	if concl == nil || concl.ID != "C-0001" {
		t.Fatalf("conclusion = %+v, want C-0001", concl)
	}
	if exp == nil || exp.ID != "E-0001" {
		t.Fatalf("experiment = %+v, want E-0001", exp)
	}
}
