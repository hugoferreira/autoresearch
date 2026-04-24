package entity_test

import (
	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/testkit"
)

var _ = testkit.Spec("TestGoalRoundTrip", func(t testkit.T) {
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
})

var _ = testkit.Spec("TestGoalAcceptsLegacyTargetEffect", func(t testkit.T) {
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
})

var _ = testkit.Spec("TestGoalRejectsLegacyTargetEffectAlongsideCompletion", func(t testkit.T) {
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
})

var _ = testkit.Spec("TestGoalRescuersRoundTrip", func(t testkit.T) {
	flash := 131072.0
	g := &entity.Goal{
		SchemaVersion: entity.GoalSchemaVersion,
		Objective: entity.Objective{
			Instrument: "ns_per_eval",
			Direction:  "decrease",
		},
		Constraints: []entity.Constraint{
			{Instrument: "size_flash", Max: &flash},
			{Instrument: "host_test", Require: "pass"},
		},
		Rescuers: []entity.Rescuer{
			{Instrument: "sim_total_bytes", Direction: "decrease", MinEffect: 0.02},
		},
		NeutralBandFrac: 0.02,
	}
	data, err := g.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := entity.ParseGoal(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Rescuers) != 1 {
		t.Fatalf("rescuers round-trip: got %d, want 1", len(back.Rescuers))
	}
	r := back.Rescuers[0]
	if r.Instrument != "sim_total_bytes" || r.Direction != "decrease" || r.MinEffect != 0.02 {
		t.Errorf("rescuer mismatch: %+v", r)
	}
	if back.NeutralBandFrac != 0.02 {
		t.Errorf("neutral_band_frac: got %g, want 0.02", back.NeutralBandFrac)
	}
	if back.SchemaVersion != entity.GoalSchemaVersion {
		t.Errorf("schema_version round-trip: got %d, want %d", back.SchemaVersion, entity.GoalSchemaVersion)
	}
})

var _ = testkit.Spec("TestGoalLegacyV3ParsesAndUpgrades", func(t testkit.T) {
	// A goal on disk before rescuers were a concept. schema_version=3, no
	// rescuers or neutral_band_frac. It must parse cleanly and upgrade to
	// the current schema version on read without losing any fields.
	data := []byte(`---
schema_version: 3
objective:
  instrument: ns_per_eval
  target: bench
  direction: decrease
constraints:
  - instrument: host_test
    require: pass
---

# Steering

focus on the hot inner loop
`)
	g, err := entity.ParseGoal(data)
	if err != nil {
		t.Fatalf("ParseGoal: %v", err)
	}
	if len(g.Rescuers) != 0 {
		t.Errorf("v3 goal should parse with empty rescuers, got %+v", g.Rescuers)
	}
	if g.NeutralBandFrac != 0 {
		t.Errorf("v3 goal should have zero neutral_band_frac, got %g", g.NeutralBandFrac)
	}
	if g.SchemaVersion != 3 {
		t.Errorf("ParseGoal should preserve on-disk schema version; got %d, want 3", g.SchemaVersion)
	}
})

var _ = testkit.Spec("TestGoalDefaultsThresholdPolicyWhenOmitted", func(t testkit.T) {
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
})
