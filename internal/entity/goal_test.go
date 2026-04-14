package entity_test

import (
	"testing"

	"github.com/bytter/autoresearch/internal/entity"
)

func TestGoalRoundTrip(t *testing.T) {
	flash := 65536.0
	ram := 16384.0
	g := &entity.Goal{
		SchemaVersion: entity.GoalSchemaVersion,
		Objective: entity.Objective{
			Instrument: "qemu_cycles",
			Target:     "dsp_fir_bench",
			Direction:  "decrease",
		},
		Completion: &entity.Completion{
			Threshold:   0.15,
			OnThreshold: entity.GoalOnThresholdAskHuman,
		},
		Constraints: []entity.Constraint{
			{Instrument: "size_flash", Max: &flash},
			{Instrument: "size_ram", Max: &ram},
			{Instrument: "host_test", Require: "pass"},
		},
		Body: "# Steering\n\nFocus on the hot inner loop.\n",
	}
	data, err := g.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := entity.ParseGoal(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Objective.Instrument != "qemu_cycles" {
		t.Errorf("objective instrument: %q", back.Objective.Instrument)
	}
	if len(back.Constraints) != 3 {
		t.Fatalf("constraints: got %d, want 3", len(back.Constraints))
	}
	if back.Constraints[0].Max == nil || *back.Constraints[0].Max != 65536 {
		t.Errorf("flash constraint max: %+v", back.Constraints[0].Max)
	}
	if back.Constraints[2].Require != "pass" {
		t.Errorf("host_test require: %q", back.Constraints[2].Require)
	}
	if back.Completion == nil || back.Completion.Threshold != 0.15 || back.Completion.OnThreshold != entity.GoalOnThresholdAskHuman {
		t.Errorf("completion round-trip mismatch: %+v", back.Completion)
	}
	if back.Steering() == "" {
		t.Errorf("steering extraction empty")
	}
}

func TestGoalAcceptsLegacyTargetEffect(t *testing.T) {
	data := []byte(`---
objective:
  instrument: qemu_cycles
  target: dsp_fir_bench
  direction: decrease
  target_effect: 0.15
constraints:
  - instrument: size_flash
    max: 65536
---

# Steering

focus on the hot inner loop
`)
	back, err := entity.ParseGoal(data)
	if err != nil {
		t.Fatalf("ParseGoal failed: %v", err)
	}
	if back.Completion == nil || back.Completion.Threshold != 0.15 || back.Completion.OnThreshold != entity.GoalOnThresholdAskHuman {
		t.Fatalf("legacy target_effect should map to ask_human completion, got %+v", back.Completion)
	}
}

func TestGoalRejectsLegacyTargetEffectAlongsideCompletion(t *testing.T) {
	data := []byte(`---
objective:
  instrument: qemu_cycles
  target: dsp_fir_bench
  direction: decrease
  target_effect: 0.15
completion:
  threshold: 0.2
  on_threshold: stop
constraints:
  - instrument: size_flash
    max: 65536
---

# Steering

focus on the hot inner loop
`)
	if _, err := entity.ParseGoal(data); err == nil {
		t.Fatal("expected mixed legacy and new completion fields to be rejected")
	}
}

func TestGoalDefaultsThresholdPolicyWhenOmitted(t *testing.T) {
	data := []byte(`---
objective:
  instrument: qemu_cycles
  target: dsp_fir_bench
  direction: decrease
completion:
  threshold: 0.15
constraints:
  - instrument: size_flash
    max: 65536
---

# Steering

focus on the hot inner loop
`)
	back, err := entity.ParseGoal(data)
	if err != nil {
		t.Fatalf("ParseGoal failed: %v", err)
	}
	if back.Completion == nil || back.Completion.OnThreshold != entity.GoalOnThresholdAskHuman {
		t.Fatalf("completion.on_threshold = %+v, want %q", back.Completion, entity.GoalOnThresholdAskHuman)
	}
}
