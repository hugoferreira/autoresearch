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
		Tier:        entity.TierHost,
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
		Tier:        entity.TierHost,
		Instruments: []string{"ghost_instrument"},
		Baseline:    entity.Baseline{Ref: "HEAD"},
	}
	if err := firewall.ValidateExperiment(e, cfgWith("qemu_cycles")); err == nil {
		t.Error("unregistered instrument should be rejected")
	}
}

func TestCheckTierGate(t *testing.T) {
	if err := firewall.CheckTierGate(entity.TierHost, false, false); err != nil {
		t.Errorf("host tier always allowed, got %v", err)
	}
	if err := firewall.CheckTierGate(entity.TierQemu, false, false); err == nil {
		t.Error("qemu without prior host should be rejected")
	}
	if err := firewall.CheckTierGate(entity.TierQemu, true, false); err != nil {
		t.Errorf("qemu with prior host should be allowed, got %v", err)
	}
	if err := firewall.CheckTierGate(entity.TierQemu, false, true); err != nil {
		t.Errorf("qemu with --force should be allowed, got %v", err)
	}
	if err := firewall.CheckTierGate(entity.TierHardware, true, false); err == nil {
		t.Error("hardware without --force should always be rejected")
	}
	if err := firewall.CheckTierGate(entity.TierHardware, false, true); err != nil {
		t.Errorf("hardware with --force should be allowed, got %v", err)
	}
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
