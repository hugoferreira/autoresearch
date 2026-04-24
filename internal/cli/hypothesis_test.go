package cli

import (
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
)

var _ = testkit.Spec("TestHypothesisAdd_RejectsPredictedInstrumentOutsideGoal", func(t testkit.T) {
	dir := t.TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
		Instruments: map[string]store.Instrument{
			"timing":      {Unit: "s"},
			"binary_size": {Unit: "bytes"},
			"compile":     {Unit: "bool"},
			"qemu_cycles": {Unit: "cycles"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	max := 131072.0
	goal := &entity.Goal{
		ID:        "G-0001",
		Status:    entity.GoalStatusActive,
		CreatedAt: &now,
		Objective: entity.Objective{
			Instrument: "timing",
			Direction:  "decrease",
		},
		Constraints: []entity.Constraint{
			{Instrument: "binary_size", Max: &max},
			{Instrument: "compile", Require: "pass"},
		},
	}
	if err := s.WriteGoal(goal); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateState(func(st *store.State) error {
		st.CurrentGoalID = goal.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	oldJSON, oldProjectDir, oldDryRun := globalJSON, globalProjectDir, globalDryRun
	t.Cleanup(func() {
		globalJSON = oldJSON
		globalProjectDir = oldProjectDir
		globalDryRun = oldDryRun
	})

	root := Root()
	root.SetArgs([]string{
		"-C", dir,
		"hypothesis", "add",
		"--claim", "improve qemu cycle count",
		"--predicts-instrument", "qemu_cycles",
		"--predicts-target", "firmware",
		"--predicts-direction", "decrease",
		"--predicts-min-effect", "0.1",
		"--kill-if", "tests fail",
	})

	err = root.Execute()
	if err == nil {
		t.Fatal("expected hypothesis add to reject a predicted instrument outside the active goal")
	}
	if !strings.Contains(err.Error(), "goal objective or an explicit constraint instrument") {
		t.Fatalf("unexpected error: %v", err)
	}
})
