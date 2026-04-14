package cli

import (
	"testing"

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
		"",
	)
	if err == nil {
		t.Fatal("expected negative threshold to be rejected")
	}
}

func TestAssessGoalCompletion(t *testing.T) {
	t.Run("open ended goals continue", func(t *testing.T) {
		goal := &entity.Goal{
			Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
		}
		got := assessGoalCompletion(goal, nil, nil)
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
		got := assessGoalCompletion(goal, concls, obsByExp)
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
		got := assessGoalCompletion(goal, concls, obsByExp)
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
		got := assessGoalCompletion(goal, concls, obsByExp)
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
		got := assessGoalCompletion(goal, concls, obsByExp)
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
		got := assessGoalCompletion(goal, concls, obsByExp)
		if got.Met || got.RecommendedAction != "continue" {
			t.Fatalf("unexpected assessment: %+v", got)
		}
	})
}
