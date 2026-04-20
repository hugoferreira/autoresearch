package readmodel

import (
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
)

func observationIndexForTest(m map[string][]*entity.Observation) *ObservationIndex {
	var all []*entity.Observation
	for expID, obs := range m {
		for _, o := range obs {
			if o == nil {
				continue
			}
			if o.Experiment == expID {
				all = append(all, o)
				continue
			}
			copyObs := *o
			copyObs.Experiment = expID
			all = append(all, &copyObs)
		}
	}
	return NewObservationIndex(all)
}

func TestAssessGoalCompletion_CountsReviewedSupportedCandidatesAfterAcceptance(t *testing.T) {
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
			HypothesisStatus: entity.StatusSupported,
		},
	}

	got := AssessGoalCompletion(goal, concls, observationIndexForTest(obsByExp), expClassByID)
	if !got.Met || got.MetByConclusion != "C-3000" || got.RecommendedAction != "stop" {
		t.Fatalf("unexpected assessment: %+v", got)
	}
}

func TestComputeFrontierFromObservations_StalledForCountsLaterConclusionsAfterAcceptedWin(t *testing.T) {
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
			Verdict:      entity.VerdictInconclusive,
			CandidateExp: "E-0002",
			Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.09},
			CreatedAt:    base.Add(1 * time.Minute),
		},
		{
			ID:           "C-0003",
			Hypothesis:   "H-0003",
			Verdict:      entity.VerdictRefuted,
			CandidateExp: "E-0003",
			Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.20},
			CreatedAt:    base.Add(2 * time.Minute),
		},
	}
	obsByExp := map[string][]*entity.Observation{
		"E-0001": {{Instrument: "host_timing", Value: 100}},
		"E-0002": {{Instrument: "host_timing", Value: 101}},
		"E-0003": {{Instrument: "host_timing", Value: 90}},
	}

	rows, stalled := ComputeFrontierFromObservations(goal, concls, observationIndexForTest(obsByExp), map[string]ExperimentReadClass{
		"E-0001": {
			Classification:   ExperimentClassificationDead,
			HypothesisStatus: entity.StatusSupported,
		},
	})
	if got, want := stalled, 2; got != want {
		t.Fatalf("stalled_for = %d, want %d", got, want)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("rows len = %d, want %d", got, want)
	}
	if got, want := rows[0].Candidate, "E-0001"; got != want {
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
	expClassByID := map[string]ExperimentReadClass{
		"E-0001": {
			Classification:   ExperimentClassificationDead,
			HypothesisStatus: entity.StatusSupported,
		},
	}

	got := BuildFrontierSnapshot(goal, concls, NewObservationIndex(obs), expClassByID)
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
	if got.Rows[0].Classification != ExperimentClassificationDead {
		t.Fatalf("row classification = %q, want %q", got.Rows[0].Classification, ExperimentClassificationDead)
	}
}

func TestComputeFrontierFromObservations_UsesConclusionCandidateScope(t *testing.T) {
	goal := &entity.Goal{
		Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
	}
	concls := []*entity.Conclusion{
		{
			ID:               "C-0001",
			Hypothesis:       "H-0001",
			Verdict:          entity.VerdictSupported,
			Observations:     []string{"O-a1"},
			CandidateExp:     "E-0001",
			CandidateAttempt: 1,
			CandidateRef:     "refs/heads/candidate/E-0001-a1",
			CandidateSHA:     "1111111111111111111111111111111111111111",
			Effect:           entity.Effect{Instrument: "host_timing", DeltaFrac: -0.10},
		},
		{
			ID:               "C-0002",
			Hypothesis:       "H-0002",
			Verdict:          entity.VerdictSupported,
			Observations:     []string{"O-a2"},
			CandidateExp:     "E-0001",
			CandidateAttempt: 2,
			CandidateRef:     "refs/heads/candidate/E-0001-a2",
			CandidateSHA:     "2222222222222222222222222222222222222222",
			Effect:           entity.Effect{Instrument: "host_timing", DeltaFrac: -0.20},
		},
	}
	obs := NewObservationIndex([]*entity.Observation{
		{
			ID:           "O-a2",
			Experiment:   "E-0001",
			Instrument:   "host_timing",
			Value:        80,
			Attempt:      2,
			CandidateRef: "refs/heads/candidate/E-0001-a2",
			CandidateSHA: "2222222222222222222222222222222222222222",
		},
		{
			ID:           "O-a1",
			Experiment:   "E-0001",
			Instrument:   "host_timing",
			Value:        100,
			Attempt:      1,
			CandidateRef: "refs/heads/candidate/E-0001-a1",
			CandidateSHA: "1111111111111111111111111111111111111111",
		},
	})
	rows, _ := ComputeFrontierFromObservations(goal, concls, obs, nil)
	if got, want := len(rows), 2; got != want {
		t.Fatalf("rows len = %d, want %d", got, want)
	}
	if got, want := rows[0].Conclusion, "C-0002"; got != want {
		t.Fatalf("best row conclusion = %q, want %q", got, want)
	}
	if got, want := rows[0].Value, 80.0; got != want {
		t.Fatalf("best row value = %v, want %v", got, want)
	}
	if got, want := rows[1].Value, 100.0; got != want {
		t.Fatalf("second row value = %v, want %v", got, want)
	}
}

func TestAssessGoalCompletion_UsesReviewedConclusionScope(t *testing.T) {
	goal := &entity.Goal{
		Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
		Completion: &entity.Completion{
			Threshold:   0.15,
			OnThreshold: entity.GoalOnThresholdStop,
		},
	}
	concls := []*entity.Conclusion{
		{
			ID:               "C-0001",
			Hypothesis:       "H-0001",
			Verdict:          entity.VerdictSupported,
			ReviewedBy:       "agent:gate",
			Observations:     []string{"O-a1"},
			CandidateExp:     "E-0001",
			CandidateAttempt: 1,
			CandidateRef:     "refs/heads/candidate/E-0001-a1",
			CandidateSHA:     "1111111111111111111111111111111111111111",
			Effect:           entity.Effect{Instrument: "host_timing", DeltaFrac: -0.10},
		},
		{
			ID:               "C-0002",
			Hypothesis:       "H-0002",
			Verdict:          entity.VerdictSupported,
			ReviewedBy:       "agent:gate",
			Observations:     []string{"O-a2"},
			CandidateExp:     "E-0001",
			CandidateAttempt: 2,
			CandidateRef:     "refs/heads/candidate/E-0001-a2",
			CandidateSHA:     "2222222222222222222222222222222222222222",
			Effect:           entity.Effect{Instrument: "host_timing", DeltaFrac: -0.20},
		},
	}
	obs := NewObservationIndex([]*entity.Observation{
		{
			ID:           "O-a1",
			Experiment:   "E-0001",
			Instrument:   "host_timing",
			Value:        100,
			Attempt:      1,
			CandidateRef: "refs/heads/candidate/E-0001-a1",
			CandidateSHA: "1111111111111111111111111111111111111111",
		},
		{
			ID:           "O-a2",
			Experiment:   "E-0001",
			Instrument:   "host_timing",
			Value:        80,
			Attempt:      2,
			CandidateRef: "refs/heads/candidate/E-0001-a2",
			CandidateSHA: "2222222222222222222222222222222222222222",
		},
	})
	got := AssessGoalCompletion(goal, concls, obs, nil)
	if !got.Met || got.MetByConclusion != "C-0002" || got.RecommendedAction != "stop" {
		t.Fatalf("unexpected assessment: %+v", got)
	}
}
