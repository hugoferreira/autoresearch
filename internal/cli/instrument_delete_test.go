package cli

import (
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

// registerExtraInstrument adds an unreferenced instrument named `name` to
// the store so tests can exercise delete without having to design a new
// goal around it.
func registerExtraInstrument(t testkit.T, s *store.Store, name string) {
	t.Helper()
	if err := s.RegisterInstrument(name, store.Instrument{
		Cmd: []string{"true"}, Parser: "builtin:passfail", Unit: "bool",
	}); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

var _ = ginkgo.Describe("TestInstrumentDelete_HappyPath", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		saveGlobals(t)
		dir, s := setupGoalStore(t)
		registerExtraInstrument(t, s, "extra")

		root := Root()
		root.SetArgs([]string{
			"-C", dir,
			"instrument", "delete", "extra",
			"--reason", "typo",
		})
		if err := root.Execute(); err != nil {
			t.Fatalf("delete: %v", err)
		}

		insts, err := s.ListInstruments()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := insts["extra"]; ok {
			t.Error("instrument still present after delete")
		}

		ev := findLastEvent(t, s, "instrument.delete")
		if ev == nil {
			t.Fatal("instrument.delete event not found")
		}
		payload := decodePayload(t, ev)
		if payload["name"] != "extra" {
			t.Errorf("name = %v, want extra", payload["name"])
		}
		if payload["reason"] != "typo" {
			t.Errorf("reason = %v, want typo", payload["reason"])
		}
		if payload["forced"] != false {
			t.Errorf("forced = %v, want false", payload["forced"])
		}
	})
})

var _ = ginkgo.Describe("TestInstrumentDelete_RefusesUnknown", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		saveGlobals(t)
		dir, _ := setupGoalStore(t)

		root := Root()
		root.SetArgs([]string{
			"-C", dir,
			"instrument", "delete", "nonexistent",
		})
		err := root.Execute()
		if err == nil {
			t.Fatal("expected error for unknown instrument")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("err = %v, want 'not found'", err)
		}
	})
})

var _ = ginkgo.Describe("TestInstrumentDelete_RefusesGoalObjective", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		saveGlobals(t)
		dir, _ := setupGoalStore(t)

		// setupGoalStore's objective instrument is "timing"
		root := Root()
		root.SetArgs([]string{
			"-C", dir,
			"instrument", "delete", "timing",
		})
		err := root.Execute()
		if err == nil {
			t.Fatal("expected error when deleting goal-objective instrument")
		}
		if !strings.Contains(err.Error(), "active goal objective") {
			t.Errorf("err = %v, want 'active goal objective'", err)
		}
	})
})

var _ = ginkgo.Describe("TestInstrumentDelete_ForceDoesNotOverrideObjective", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		saveGlobals(t)
		dir, _ := setupGoalStore(t)

		root := Root()
		root.SetArgs([]string{
			"-C", dir,
			"instrument", "delete", "timing",
			"--force",
		})
		err := root.Execute()
		if err == nil {
			t.Fatal("expected error: --force should not override objective ref")
		}
		if !strings.Contains(err.Error(), "active goal objective") {
			t.Errorf("err = %v, want 'active goal objective'", err)
		}
	})
})

var _ = ginkgo.Describe("TestInstrumentDelete_RefusesReferencedByHypothesis", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		saveGlobals(t)
		dir, s := setupGoalStore(t)

		// Create a hypothesis that predicts on "binary_size" (a constraint
		// instrument from setupGoalStore).
		h := &entity.Hypothesis{
			ID:     "H-0001",
			GoalID: "G-0001",
			Claim:  "shrink",
			Predicts: entity.Predicts{
				Instrument: "binary_size",
				Target:     "firmware",
				Direction:  "decrease",
				MinEffect:  0.05,
			},
			KillIf: []string{"tests fail"},
			Status: entity.StatusOpen,
			Author: "human",
		}
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}

		root := Root()
		root.SetArgs([]string{
			"-C", dir,
			"instrument", "delete", "binary_size",
		})
		err := root.Execute()
		if err == nil {
			t.Fatal("expected error when deleting instrument referenced by a hypothesis + constraint")
		}
		if !strings.Contains(err.Error(), "--force") {
			t.Errorf("err should suggest --force, got %v", err)
		}
	})
})

var _ = ginkgo.Describe("TestInstrumentDelete_ForceOrphans", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		saveGlobals(t)
		dir, s := setupGoalStore(t)

		// "host_test" is only a goal constraint — no hypothesis yet.
		root := Root()
		root.SetArgs([]string{
			"-C", dir,
			"instrument", "delete", "host_test",
			"--force",
			"--reason", "deprecated",
		})
		if err := root.Execute(); err != nil {
			t.Fatalf("force-delete: %v", err)
		}

		insts, err := s.ListInstruments()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := insts["host_test"]; ok {
			t.Error("host_test still present after --force delete")
		}

		ev := findLastEvent(t, s, "instrument.delete")
		if ev == nil {
			t.Fatal("instrument.delete event not found")
		}
		payload := decodePayload(t, ev)
		if payload["forced"] != true {
			t.Errorf("forced = %v, want true", payload["forced"])
		}
		orphaned, ok := payload["orphaned"].(map[string]any)
		if !ok {
			t.Fatalf("orphaned payload missing or wrong type: %v", payload["orphaned"])
		}
		constraints, _ := orphaned["goal_constraints"].([]any)
		if len(constraints) == 0 {
			t.Errorf("expected goal_constraints to list G-0001, got %v", orphaned)
		}
	})
})
