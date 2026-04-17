package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

// findLastEvent returns the last event whose Kind matches, or nil.
func findLastEvent(t *testing.T, s *store.Store, kind string) *store.Event {
	t.Helper()
	events, err := s.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == kind {
			return &events[i]
		}
	}
	return nil
}

func decodePayload(t *testing.T, e *store.Event) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(e.Data, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return payload
}

// saveGlobals snapshots the package-level cobra globals so a test can restore
// them on cleanup. The CLI commands mutate these through flag parsing.
func saveGlobals(t *testing.T) {
	t.Helper()
	j, p, d := globalJSON, globalProjectDir, globalDryRun
	t.Cleanup(func() {
		globalJSON = j
		globalProjectDir = p
		globalDryRun = d
	})
}

// setupGoalStore creates a fresh .research/ with a minimal active goal.
func setupGoalStore(t *testing.T) (string, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
		Instruments: map[string]store.Instrument{
			"timing":      {Unit: "s"},
			"binary_size": {Unit: "bytes"},
			"host_test":   {Unit: "bool"},
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
		Objective: entity.Objective{Instrument: "timing", Direction: "decrease"},
		Constraints: []entity.Constraint{
			{Instrument: "binary_size", Max: &max},
			{Instrument: "host_test", Require: "pass"},
		},
	}
	if err := s.WriteGoal(goal); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateState(func(st *store.State) error {
		st.CurrentGoalID = goal.ID
		st.Counters["G"] = 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return dir, s
}

func TestEventPayload_HypothesisKillRecordsFromTo(t *testing.T) {
	saveGlobals(t)
	dir, s := setupGoalStore(t)

	root := Root()
	root.SetArgs([]string{
		"-C", dir,
		"hypothesis", "add",
		"--claim", "tighten loop",
		"--predicts-instrument", "timing",
		"--predicts-target", "fir",
		"--predicts-direction", "decrease",
		"--predicts-min-effect", "0.1",
		"--kill-if", "tests fail",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("hypothesis add: %v", err)
	}

	root = Root()
	root.SetArgs([]string{
		"-C", dir,
		"hypothesis", "kill", "H-0001",
		"--reason", "obsolete",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("hypothesis kill: %v", err)
	}

	e := findLastEvent(t, s, "hypothesis.kill")
	if e == nil {
		t.Fatal("hypothesis.kill event not found")
	}
	payload := decodePayload(t, e)
	if got := payload["from"]; got != entity.StatusOpen {
		t.Errorf("data.from = %v, want %q", got, entity.StatusOpen)
	}
	if got := payload["to"]; got != entity.StatusKilled {
		t.Errorf("data.to = %v, want %q", got, entity.StatusKilled)
	}
	if got := payload["reason"]; got != "obsolete" {
		t.Errorf("data.reason = %v, want %q", got, "obsolete")
	}
}

func TestEventPayload_HypothesisReopenRecordsFromTo(t *testing.T) {
	saveGlobals(t)
	dir, s := setupGoalStore(t)

	// Seed a killed hypothesis directly so we don't need to run kill first.
	now := time.Now().UTC()
	h := &entity.Hypothesis{
		ID:     "H-0001",
		GoalID: "G-0001",
		Claim:  "tighten loop",
		Predicts: entity.Predicts{
			Instrument: "timing", Target: "fir", Direction: "decrease", MinEffect: 0.1,
		},
		KillIf:    []string{"tests fail"},
		Status:    entity.StatusKilled,
		Author:    "human",
		CreatedAt: now,
	}
	if err := s.WriteHypothesis(h); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateState(func(st *store.State) error {
		st.Counters["H"] = 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	root := Root()
	root.SetArgs([]string{
		"-C", dir,
		"hypothesis", "reopen", "H-0001",
		"--reason", "new evidence",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("hypothesis reopen: %v", err)
	}

	e := findLastEvent(t, s, "hypothesis.reopen")
	if e == nil {
		t.Fatal("hypothesis.reopen event not found")
	}
	payload := decodePayload(t, e)
	if got := payload["from"]; got != entity.StatusKilled {
		t.Errorf("data.from = %v, want %q", got, entity.StatusKilled)
	}
	if got := payload["to"]; got != entity.StatusOpen {
		t.Errorf("data.to = %v, want %q", got, entity.StatusOpen)
	}
}

func TestEventPayload_InstrumentRegisterEmitsFieldMap(t *testing.T) {
	saveGlobals(t)
	dir := t.TempDir()
	if _, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	}); err != nil {
		t.Fatal(err)
	}

	root := Root()
	root.SetArgs([]string{
		"-C", dir,
		"instrument", "register", "host_test",
		"--cmd", "go,test,./...",
		"--parser", "builtin:passfail",
		"--unit", "bool",
		"--min-samples", "1",
		"--requires", "build=pass",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("instrument register: %v", err)
	}

	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := findLastEvent(t, s, "instrument.register")
	if e == nil {
		t.Fatal("instrument.register event not found")
	}
	payload := decodePayload(t, e)

	// Must have lowercase field-map keys (not capitalized struct field names).
	wantKeys := []string{"cmd", "parser", "unit", "min_samples", "requires"}
	for _, k := range wantKeys {
		if _, ok := payload[k]; !ok {
			t.Errorf("payload missing key %q; got %v", k, payload)
		}
	}
	// Must NOT carry the raw-struct shape (capitalized Go field names).
	for _, k := range []string{"Cmd", "Parser", "Unit", "MinSamples", "Requires"} {
		if _, ok := payload[k]; ok {
			t.Errorf("payload leaked raw struct key %q", k)
		}
	}
	if got := payload["parser"]; got != "builtin:passfail" {
		t.Errorf("data.parser = %v, want builtin:passfail", got)
	}
	if got := payload["unit"]; got != "bool" {
		t.Errorf("data.unit = %v, want bool", got)
	}
	// requires should be a list-shaped value carrying "build=pass".
	reqs, ok := payload["requires"].([]any)
	if !ok {
		t.Fatalf("data.requires not a list: %T", payload["requires"])
	}
	if len(reqs) != 1 || reqs[0] != "build=pass" {
		t.Errorf("data.requires = %v, want [build=pass]", reqs)
	}
}
