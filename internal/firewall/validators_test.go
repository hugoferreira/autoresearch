package firewall_test

import (
	"strings"
	"testing"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/store"
)

func cfgWith(names ...string) *store.Config {
	cfg := &store.Config{Instruments: map[string]store.Instrument{}}
	for _, n := range names {
		cfg.Instruments[n] = store.Instrument{Unit: "x"}
	}
	return cfg
}

func TestRequireActiveGoal(t *testing.T) {
	if err := firewall.RequireActiveGoal(nil); err == nil {
		t.Error("nil state should be rejected")
	}
	if err := firewall.RequireActiveGoal(&store.State{}); err == nil {
		t.Error("empty current_goal_id should be rejected")
	}
	if err := firewall.RequireActiveGoal(&store.State{CurrentGoalID: "G-0001"}); err != nil {
		t.Errorf("active goal should pass, got %v", err)
	}
}

func TestRequireNoActiveGoal(t *testing.T) {
	if err := firewall.RequireNoActiveGoal(nil); err != nil {
		t.Errorf("nil state counts as no active goal, got %v", err)
	}
	if err := firewall.RequireNoActiveGoal(&store.State{}); err != nil {
		t.Errorf("empty current_goal_id counts as no active goal, got %v", err)
	}
	err := firewall.RequireNoActiveGoal(&store.State{CurrentGoalID: "G-0007"})
	if err == nil || !strings.Contains(err.Error(), "G-0007") {
		t.Errorf("existing active goal should be rejected with id, got %v", err)
	}
}

func TestValidateGoal_FeatureStyle(t *testing.T) {
	g := &entity.Goal{}
	err := firewall.ValidateGoal(g, cfgWith())
	if err == nil || !strings.Contains(err.Error(), "not build features") {
		t.Errorf("feature-style goal should name the scope boundary, got %v", err)
	}
}

func TestValidateGoal_UnregisteredInstrument(t *testing.T) {
	g := &entity.Goal{Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"}}
	err := firewall.ValidateGoal(g, cfgWith())
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Errorf("unregistered instrument should be rejected, got %v", err)
	}
}

func TestValidateGoal_NoConstraints(t *testing.T) {
	g := &entity.Goal{Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"}}
	err := firewall.ValidateGoal(g, cfgWith("qemu_cycles"))
	if err == nil || !strings.Contains(err.Error(), "at least one constraint") {
		t.Errorf("missing constraints should be rejected, got %v", err)
	}
}

func TestValidateGoal_Happy(t *testing.T) {
	max := 65536.0
	g := &entity.Goal{
		Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"},
		Constraints: []entity.Constraint{
			{Instrument: "size_flash", Max: &max},
		},
	}
	if err := firewall.ValidateGoal(g, cfgWith("qemu_cycles", "size_flash")); err != nil {
		t.Errorf("valid goal rejected: %v", err)
	}
}

func TestValidateHypothesis_MissingFields(t *testing.T) {
	h := &entity.Hypothesis{}
	if err := firewall.ValidateHypothesis(h, cfgWith("qemu_cycles")); err == nil {
		t.Error("empty hypothesis should be rejected")
	}
}

func TestValidateHypothesis_NoKillIf(t *testing.T) {
	h := &entity.Hypothesis{
		Claim: "x",
		Predicts: entity.Predicts{
			Instrument: "qemu_cycles", Target: "y", Direction: "decrease", MinEffect: 0.1,
		},
	}
	if err := firewall.ValidateHypothesis(h, cfgWith("qemu_cycles")); err == nil || !strings.Contains(err.Error(), "kill_if") {
		t.Errorf("hypothesis without kill_if should be rejected, got %v", err)
	}
}

func TestValidateExperiment_Happy(t *testing.T) {
	e := &entity.Experiment{
		Hypothesis:  "H-0001",
		Instruments: []string{"qemu_cycles"},
		Baseline:    entity.Baseline{Ref: "HEAD"},
	}
	if err := firewall.ValidateExperiment(e, cfgWith("qemu_cycles")); err != nil {
		t.Errorf("valid experiment rejected: %v", err)
	}
}

func TestValidateExperiment_UnregisteredInstrument(t *testing.T) {
	e := &entity.Experiment{
		Hypothesis:  "H-0001",
		Instruments: []string{"ghost_instrument"},
		Baseline:    entity.Baseline{Ref: "HEAD"},
	}
	if err := firewall.ValidateExperiment(e, cfgWith("qemu_cycles")); err == nil {
		t.Error("unregistered instrument should be rejected")
	}
}

func TestCheckInstrumentDependencies(t *testing.T) {
	cfg := &store.Config{Instruments: map[string]store.Instrument{
		"host_test":   {Unit: "bool"},
		"qemu_cycles": {Unit: "cycles", Requires: []string{"host_test=pass"}},
	}}

	t.Run("no observations returns error", func(t *testing.T) {
		err := firewall.CheckInstrumentDependencies("qemu_cycles", cfg, nil)
		if err == nil {
			t.Error("expected error when no observations satisfy the prerequisite")
		}
	})

	t.Run("passing host_test observation returns nil", func(t *testing.T) {
		pass := true
		obs := []*entity.Observation{
			{Instrument: "host_test", Pass: &pass},
		}
		if err := firewall.CheckInstrumentDependencies("qemu_cycles", cfg, obs); err != nil {
			t.Errorf("expected nil with passing prerequisite, got %v", err)
		}
	})

	t.Run("non-passing observation still returns error", func(t *testing.T) {
		fail := false
		obs := []*entity.Observation{
			{Instrument: "host_test", Pass: &fail},
		}
		err := firewall.CheckInstrumentDependencies("qemu_cycles", cfg, obs)
		if err == nil {
			t.Error("expected error when prerequisite observation is not passing")
		}
	})
}

func TestValidateHypothesis_Happy(t *testing.T) {
	h := &entity.Hypothesis{
		Claim: "unroll dsp_fir 4x cuts cycles >10%",
		Predicts: entity.Predicts{
			Instrument: "qemu_cycles", Target: "dsp_fir_bench", Direction: "decrease", MinEffect: 0.10,
		},
		KillIf: []string{"flash delta > 1024 bytes"},
	}
	if err := firewall.ValidateHypothesis(h, cfgWith("qemu_cycles")); err != nil {
		t.Errorf("valid hypothesis rejected: %v", err)
	}
}

func TestValidateLesson(t *testing.T) {
	t.Run("happy hypothesis-scope", func(t *testing.T) {
		l := &entity.Lesson{
			Claim: "unroll past 8× is cache-bound",
			Scope: entity.LessonScopeHypothesis, Subjects: []string{"C-0003"},
			Status: entity.LessonStatusActive,
		}
		if err := firewall.ValidateLesson(l); err != nil {
			t.Errorf("valid lesson rejected: %v", err)
		}
	})
	t.Run("happy system-scope", func(t *testing.T) {
		l := &entity.Lesson{
			Claim: "fixture cache is sticky",
			Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive,
		}
		if err := firewall.ValidateLesson(l); err != nil {
			t.Errorf("valid system lesson rejected: %v", err)
		}
	})
	t.Run("empty claim", func(t *testing.T) {
		l := &entity.Lesson{Scope: entity.LessonScopeSystem}
		if err := firewall.ValidateLesson(l); err == nil {
			t.Error("empty claim should fail")
		}
	})
	t.Run("invalid scope", func(t *testing.T) {
		l := &entity.Lesson{Claim: "x", Scope: "galactic"}
		if err := firewall.ValidateLesson(l); err == nil ||
			!strings.Contains(err.Error(), "scope must be") {
			t.Errorf("bad scope should fail with scope hint, got %v", err)
		}
	})
	t.Run("hypothesis scope requires subjects", func(t *testing.T) {
		l := &entity.Lesson{Claim: "x", Scope: entity.LessonScopeHypothesis}
		if err := firewall.ValidateLesson(l); err == nil ||
			!strings.Contains(err.Error(), "at least one subject") {
			t.Errorf("hypothesis scope without subjects should fail, got %v", err)
		}
	})
	t.Run("invalid subject prefix", func(t *testing.T) {
		l := &entity.Lesson{
			Claim: "x", Scope: entity.LessonScopeHypothesis, Subjects: []string{"X-0001"},
		}
		if err := firewall.ValidateLesson(l); err == nil ||
			!strings.Contains(err.Error(), "H-/E-/C-") {
			t.Errorf("bad subject prefix should fail, got %v", err)
		}
	})
	t.Run("supersedes must be L-", func(t *testing.T) {
		l := &entity.Lesson{
			Claim: "x", Scope: entity.LessonScopeSystem, SupersedesID: "H-0001",
		}
		if err := firewall.ValidateLesson(l); err == nil ||
			!strings.Contains(err.Error(), "L- id") {
			t.Errorf("non-L supersedes target should fail, got %v", err)
		}
	})
}
