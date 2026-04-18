package readmodel

import (
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
)

func TestAssessGoalCompletion_IgnoresDeadReviewedCandidates(t *testing.T) {
	goal := &entity.Goal{
		Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
		Completion: &entity.Completion{
			Threshold:   0.2,
			OnThreshold: entity.GoalOnThresholdStop,
		},
	}
	concls := []*entity.Conclusion{
		{
			ID:           "C-3000",
			Hypothesis:   "H-3000",
			Verdict:      entity.VerdictSupported,
			ReviewedBy:   "agent:gate",
			CandidateExp: "E-3000",
			Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.3},
		},
	}
	obsByExp := map[string][]*entity.Observation{
		"E-3000": {{Instrument: "host_timing", Value: 0.70}},
	}
	expClassByID := map[string]ExperimentReadClass{
		"E-3000": {
			Classification:   ExperimentClassificationDead,
			HypothesisStatus: entity.StatusKilled,
		},
	}

	got := AssessGoalCompletion(goal, concls, obsByExp, expClassByID)
	if got.Met || got.MetByConclusion != "" || got.RecommendedAction != "continue" {
		t.Fatalf("unexpected assessment: %+v", got)
	}
}

func TestComputeFrontierFromObservations_StalledForIgnoresDeadRows(t *testing.T) {
	goal := &entity.Goal{
		Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
	}
	base := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	concls := []*entity.Conclusion{
		{
			ID:           "C-0001",
			Hypothesis:   "H-0001",
			Verdict:      entity.VerdictSupported,
			CandidateExp: "E-0001",
			Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.10},
			CreatedAt:    base,
		},
		{
			ID:           "C-0002",
			Hypothesis:   "H-0002",
			Verdict:      entity.VerdictSupported,
			CandidateExp: "E-0002",
			Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.09},
			CreatedAt:    base.Add(1 * time.Minute),
		},
		{
			ID:           "C-0003",
			Hypothesis:   "H-0003",
			Verdict:      entity.VerdictSupported,
			CandidateExp: "E-0003",
			Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.20},
			CreatedAt:    base.Add(2 * time.Minute),
		},
		{
			ID:           "C-0004",
			Hypothesis:   "H-0004",
			Verdict:      entity.VerdictSupported,
			CandidateExp: "E-0004",
			Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.08},
			CreatedAt:    base.Add(3 * time.Minute),
		},
	}
	obsByExp := map[string][]*entity.Observation{
		"E-0001": {{Instrument: "host_timing", Value: 100}},
		"E-0002": {{Instrument: "host_timing", Value: 101}},
		"E-0003": {{Instrument: "host_timing", Value: 90}},
		"E-0004": {{Instrument: "host_timing", Value: 102}},
	}

	rows, stalled := ComputeFrontierFromObservations(goal, concls, obsByExp, map[string]ExperimentReadClass{
		"E-0003": {
			Classification:   ExperimentClassificationDead,
			HypothesisStatus: entity.StatusRefuted,
		},
	})
	if got, want := stalled, 2; got != want {
		t.Fatalf("stalled_for = %d, want %d", got, want)
	}
	if got, want := len(rows), 4; got != want {
		t.Fatalf("rows len = %d, want %d", got, want)
	}
	if got, want := rows[0].Candidate, "E-0003"; got != want {
		t.Fatalf("best row candidate = %q, want %q", got, want)
	}
	if got, want := rows[0].Classification, ExperimentClassificationDead; got != want {
		t.Fatalf("best row classification = %q, want %q", got, want)
	}
}

func TestBuildFrontierSnapshot_ComposesRowsAssessmentAndStall(t *testing.T) {
	goal := &entity.Goal{
		Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
		Completion: &entity.Completion{
			Threshold:   0.1,
			OnThreshold: entity.GoalOnThresholdStop,
		},
	}
	concls := []*entity.Conclusion{
		{
			ID:           "C-0001",
			Hypothesis:   "H-0001",
			Verdict:      entity.VerdictSupported,
			ReviewedBy:   "agent:gate",
			CandidateExp: "E-0001",
			Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.15},
		},
	}
	obs := []*entity.Observation{
		{Experiment: "E-0001", Instrument: "host_timing", Value: 85},
	}

	got := BuildFrontierSnapshot(goal, concls, GroupObservationsByExperiment(obs), nil)
	if got.StalledFor != 0 {
		t.Fatalf("stalled_for = %d, want 0", got.StalledFor)
	}
	if len(got.Rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(got.Rows))
	}
	if got.Rows[0].Candidate != "E-0001" {
		t.Fatalf("best row candidate = %q, want %q", got.Rows[0].Candidate, "E-0001")
	}
	if !got.Assessment.Met || got.Assessment.MetByConclusion != "C-0001" {
		t.Fatalf("unexpected assessment: %+v", got.Assessment)
	}
}
