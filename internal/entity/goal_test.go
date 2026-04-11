package entity_test

import (
	"testing"

	"github.com/bytter/autoresearch/internal/entity"
)

func TestGoalRoundTrip(t *testing.T) {
	flash := 65536.0
	ram := 16384.0
	g := &entity.Goal{
		Objective: entity.Objective{
			Instrument:   "qemu_cycles",
			Target:       "dsp_fir_bench",
			Direction:    "decrease",
			TargetEffect: 0.15,
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
	if back.Steering() == "" {
		t.Errorf("steering extraction empty")
	}
}
