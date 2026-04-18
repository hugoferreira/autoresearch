package cli

import (
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
)

func TestBuildGoalFromFlags_DefaultsThresholdPolicy(t *testing.T) {
	g, err := buildGoalFromFlags(
		"host_timing",
		"dsp_fir",
		"decrease",
		0.2,
		"",
		[]string{"size_flash=131072"},
		nil,
		nil,
		nil,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("buildGoalFromFlags failed: %v", err)
	}
	if g.Completion == nil {
		t.Fatal("expected completion block to be populated")
	}
	if got, want := g.Completion.OnThreshold, entity.GoalOnThresholdAskHuman; got != want {
		t.Fatalf("completion.on_threshold = %q, want %q", got, want)
	}
}

func TestBuildGoalFromFlags_RejectsOnSuccessWithoutThreshold(t *testing.T) {
	_, err := buildGoalFromFlags(
		"host_timing",
		"dsp_fir",
		"decrease",
		0,
		entity.GoalOnThresholdStop,
		[]string{"size_flash=131072"},
		nil,
		nil,
		nil,
		0,
		"",
	)
	if err == nil {
		t.Fatal("expected missing threshold to be rejected")
	}
}

func TestBuildGoalFromFlags_RejectsNegativeThreshold(t *testing.T) {
	_, err := buildGoalFromFlags(
		"host_timing",
		"dsp_fir",
		"decrease",
		-0.1,
		"",
		[]string{"size_flash=131072"},
		nil,
		nil,
		nil,
		0,
		"",
	)
	if err == nil {
		t.Fatal("expected negative threshold to be rejected")
	}
}

func TestBuildGoalFromFlags_RescuersRequireNeutralBand(t *testing.T) {
	_, err := buildGoalFromFlags(
		"host_timing", "dsp_fir", "decrease",
		0, "",
		[]string{"size_flash=131072"}, nil, nil,
		[]string{"sim_total_bytes:decrease:0.02"},
		0,
		"",
	)
	if err == nil {
		t.Fatal("expected rescuer without --neutral-band-frac to be rejected")
	}
}

func TestBuildGoalFromFlags_NeutralBandWithoutRescuerRejected(t *testing.T) {
	_, err := buildGoalFromFlags(
		"host_timing", "dsp_fir", "decrease",
		0, "",
		[]string{"size_flash=131072"}, nil, nil,
		nil,
		0.02,
		"",
	)
	if err == nil {
		t.Fatal("expected --neutral-band-frac without rescuer to be rejected")
	}
}

func TestBuildGoalFromFlags_RescuerAccepted(t *testing.T) {
	g, err := buildGoalFromFlags(
		"host_timing", "dsp_fir", "decrease",
		0, "",
		[]string{"size_flash=131072"}, nil, nil,
		[]string{"sim_total_bytes:decrease:0.02"},
		0.02,
		"",
	)
	if err != nil {
		t.Fatalf("buildGoalFromFlags: %v", err)
	}
	if len(g.Rescuers) != 1 || g.Rescuers[0].Instrument != "sim_total_bytes" || g.Rescuers[0].Direction != "decrease" || g.Rescuers[0].MinEffect != 0.02 {
		t.Errorf("rescuer populated incorrectly: %+v", g.Rescuers)
	}
	if g.NeutralBandFrac != 0.02 {
		t.Errorf("neutral_band_frac = %g, want 0.02", g.NeutralBandFrac)
	}
}

func TestFrontierRowBetter_RescuerTiebreak(t *testing.T) {
	goal := &entity.Goal{
		Objective:       entity.Objective{Instrument: "ns_per_eval", Direction: "decrease"},
		NeutralBandFrac: 0.02,
		Rescuers: []entity.Rescuer{
			{Instrument: "sim_total_bytes", Direction: "decrease", MinEffect: 0.02},
		},
	}
	// Same primary value, but a has smaller size → a should win.
	a := frontierRow{Value: 100.0, TiebreakValues: []float64{512}}
	b := frontierRow{Value: 100.5, TiebreakValues: []float64{600}} // within 1% band
	if !frontierRowBetter(goal, a, b) {
		t.Errorf("a should beat b via rescuer tiebreak")
	}
	if frontierRowBetter(goal, b, a) {
		t.Errorf("b should not beat a")
	}
	// If primary gap exceeds neutral band, primary wins regardless of size.
	c := frontierRow{Value: 80.0, TiebreakValues: []float64{9999}}
	d := frontierRow{Value: 100.0, TiebreakValues: []float64{10}}
	if !frontierRowBetter(goal, c, d) {
		t.Errorf("primary-dominant candidate should win over size-dominant one when outside the neutral band")
	}
}

func TestAssessGoalCompletion(t *testing.T) {
	t.Run("open ended goals continue", func(t *testing.T) {
		goal := &entity.Goal{
			Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
		}
		got := assessGoalCompletion(goal, nil, nil, nil)
		if got.Mode != "open_ended" || got.Met || got.RecommendedAction != "continue" {
			t.Fatalf("unexpected assessment: %+v", got)
		}
	})

	t.Run("thresholded decrease goal met by reviewed supported frontier candidate", func(t *testing.T) {
		goal := &entity.Goal{
			Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
			Completion: &entity.Completion{
				Threshold:   0.2,
				OnThreshold: entity.GoalOnThresholdAskHuman,
			},
		}
		concls := []*entity.Conclusion{
			{
				ID:           "C-0001",
				Hypothesis:   "H-0001",
				Verdict:      entity.VerdictSupported,
				ReviewedBy:   "agent:gate",
				CandidateExp: "E-0001",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.25},
			},
		}
		obsByExp := map[string][]*entity.Observation{
			"E-0001": {{Instrument: "host_timing", Value: 0.75}},
		}
		got := assessGoalCompletion(goal, concls, obsByExp, nil)
		if !got.Met || got.MetByConclusion != "C-0001" || got.RecommendedAction != "ask_human" {
			t.Fatalf("unexpected assessment: %+v", got)
		}
	})

	t.Run("best reviewed candidate wins even when an unreviewed candidate is better", func(t *testing.T) {
		goal := &entity.Goal{
			Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
			Completion: &entity.Completion{
				Threshold:   0.15,
				OnThreshold: entity.GoalOnThresholdStop,
			},
		}
		concls := []*entity.Conclusion{
			{
				ID:           "C-0001",
				Hypothesis:   "H-0001",
				Verdict:      entity.VerdictSupported,
				ReviewedBy:   "",
				CandidateExp: "E-0001",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.30},
			},
			{
				ID:           "C-0002",
				Hypothesis:   "H-0002",
				Verdict:      entity.VerdictSupported,
				ReviewedBy:   "agent:gate",
				CandidateExp: "E-0002",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.18},
			},
		}
		obsByExp := map[string][]*entity.Observation{
			"E-0001": {{Instrument: "host_timing", Value: 0.70}},
			"E-0002": {{Instrument: "host_timing", Value: 0.82}},
		}
		got := assessGoalCompletion(goal, concls, obsByExp, nil)
		if !got.Met || got.MetByConclusion != "C-0002" || got.RecommendedAction != "stop" {
			t.Fatalf("unexpected assessment: %+v", got)
		}
	})

	t.Run("goal met only by reviewed candidate satisfying all constraints", func(t *testing.T) {
		flashMax := 100.0
		qualityMin := 0.99
		pass := true
		goal := &entity.Goal{
			Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
			Completion: &entity.Completion{
				Threshold:   0.2,
				OnThreshold: entity.GoalOnThresholdAskHuman,
			},
			Constraints: []entity.Constraint{
				{Instrument: "size_flash", Max: &flashMax},
				{Instrument: "quality_score", Min: &qualityMin},
				{Instrument: "host_test", Require: "pass"},
			},
		}
		concls := []*entity.Conclusion{
			{
				ID:           "C-0100",
				Hypothesis:   "H-0100",
				Verdict:      entity.VerdictSupported,
				ReviewedBy:   "agent:gate",
				CandidateExp: "E-0100",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.30},
			},
			{
				ID:           "C-0101",
				Hypothesis:   "H-0101",
				Verdict:      entity.VerdictSupported,
				ReviewedBy:   "agent:gate",
				CandidateExp: "E-0101",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.25},
			},
			{
				ID:           "C-0102",
				Hypothesis:   "H-0102",
				Verdict:      entity.VerdictSupported,
				ReviewedBy:   "agent:gate",
				CandidateExp: "E-0102",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.22},
			},
		}
		obsByExp := map[string][]*entity.Observation{
			"E-0100": {
				{Instrument: "host_timing", Value: 0.70},
				{Instrument: "size_flash", Value: 120},
				{Instrument: "quality_score", Value: 0.995},
				{Instrument: "host_test", Pass: &pass},
			},
			"E-0101": {
				{Instrument: "host_timing", Value: 0.75},
				{Instrument: "size_flash", Value: 90},
				{Instrument: "quality_score", Value: 0.98},
				{Instrument: "host_test", Pass: &pass},
			},
			"E-0102": {
				{Instrument: "host_timing", Value: 0.78},
				{Instrument: "size_flash", Value: 95},
				{Instrument: "quality_score", Value: 0.995},
				{Instrument: "host_test", Pass: &pass},
			},
		}
		got := assessGoalCompletion(goal, concls, obsByExp, nil)
		if !got.Met || got.MetByConclusion != "C-0102" || got.RecommendedAction != "ask_human" {
			t.Fatalf("unexpected assessment: %+v", got)
		}
	})

	t.Run("increase goals use positive delta_frac threshold", func(t *testing.T) {
		goal := &entity.Goal{
			Objective: entity.Objective{Instrument: "throughput", Direction: "increase"},
			Completion: &entity.Completion{
				Threshold:   0.1,
				OnThreshold: entity.GoalOnThresholdContinueUntilStall,
			},
		}
		concls := []*entity.Conclusion{
			{
				ID:           "C-1000",
				Hypothesis:   "H-1000",
				Verdict:      entity.VerdictSupported,
				ReviewedBy:   "agent:gate",
				CandidateExp: "E-1000",
				Effect:       entity.Effect{Instrument: "throughput", DeltaFrac: 0.12},
			},
		}
		obsByExp := map[string][]*entity.Observation{
			"E-1000": {{Instrument: "throughput", Value: 112}},
		}
		got := assessGoalCompletion(goal, concls, obsByExp, nil)
		if !got.Met || got.RecommendedAction != "continue" {
			t.Fatalf("unexpected assessment: %+v", got)
		}
	})

	t.Run("unreviewed or non-supported conclusions do not satisfy the goal", func(t *testing.T) {
		goal := &entity.Goal{
			Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
			Completion: &entity.Completion{
				Threshold:   0.2,
				OnThreshold: entity.GoalOnThresholdAskHuman,
			},
		}
		concls := []*entity.Conclusion{
			{
				ID:           "C-2000",
				Hypothesis:   "H-2000",
				Verdict:      entity.VerdictInconclusive,
				ReviewedBy:   "agent:gate",
				CandidateExp: "E-2000",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.3},
			},
			{
				ID:           "C-2001",
				Hypothesis:   "H-2001",
				Verdict:      entity.VerdictSupported,
				ReviewedBy:   "",
				CandidateExp: "E-2001",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.3},
			},
		}
		obsByExp := map[string][]*entity.Observation{
			"E-2000": {{Instrument: "host_timing", Value: 0.70}},
			"E-2001": {{Instrument: "host_timing", Value: 0.70}},
		}
		got := assessGoalCompletion(goal, concls, obsByExp, nil)
		if got.Met || got.RecommendedAction != "continue" {
			t.Fatalf("unexpected assessment: %+v", got)
		}
	})

	t.Run("dead reviewed candidates do not satisfy threshold", func(t *testing.T) {
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
		expClassByID := map[string]experimentReadClass{
			"E-3000": {
				Classification:   experimentClassificationDead,
				HypothesisStatus: entity.StatusKilled,
			},
		}
		got := assessGoalCompletion(goal, concls, obsByExp, expClassByID)
		if got.Met || got.MetByConclusion != "" || got.RecommendedAction != "continue" {
			t.Fatalf("unexpected assessment: %+v", got)
		}
	})
}

func TestComputeFrontierFromObservations_CarriesExperimentClassification(t *testing.T) {
	goal := &entity.Goal{
		Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
	}
	concls := []*entity.Conclusion{
		{
			ID:           "C-0001",
			Hypothesis:   "H-0001",
			Verdict:      entity.VerdictSupported,
			CandidateExp: "E-0001",
			Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.25},
		},
	}
	obsByExp := map[string][]*entity.Observation{
		"E-0001": {{Instrument: "host_timing", Value: 0.75}},
	}
	rows, _ := computeFrontierFromObservations(goal, concls, obsByExp, map[string]experimentReadClass{
		"E-0001": {
			Classification:   experimentClassificationDead,
			HypothesisStatus: entity.StatusSupported,
		},
	})
	if got, want := len(rows), 1; got != want {
		t.Fatalf("rows len = %d, want %d", got, want)
	}
	if got, want := rows[0].Classification, experimentClassificationDead; got != want {
		t.Fatalf("row classification = %q, want %q", got, want)
	}
	if got, want := rows[0].HypothesisStatus, entity.StatusSupported; got != want {
		t.Fatalf("row hypothesis_status = %q, want %q", got, want)
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
	rows, stalled := computeFrontierFromObservations(goal, concls, obsByExp, map[string]experimentReadClass{
		"E-0003": {
			Classification:   experimentClassificationDead,
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
	if got, want := rows[0].Classification, experimentClassificationDead; got != want {
		t.Fatalf("best row classification = %q, want %q", got, want)
	}
}
