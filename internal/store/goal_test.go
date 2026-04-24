package store_test

import (
	"os"
	"path/filepath"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
)

// TestGoalSequenceWriteReadList exercises the basic multi-goal surface:
// allocate a G-id, write, read back, list, and verify the active-goal
// pointer follows state.current_goal_id.
var _ = testkit.Spec("TestGoalSequenceWriteReadList", func(t testkit.T) {
	s, _ := mustCreate(t)

	gID, err := s.AllocID(store.KindGoal)
	if err != nil {
		t.Fatal(err)
	}
	if gID != "G-0001" {
		t.Errorf("first goal id: got %q, want G-0001", gID)
	}
	now := time.Now().UTC()
	flash := 65536.0
	g := &entity.Goal{
		ID:        gID,
		Status:    entity.GoalStatusActive,
		CreatedAt: &now,
		Objective: entity.Objective{
			Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease",
		},
		Completion: &entity.Completion{Threshold: 0.15, OnThreshold: entity.GoalOnThresholdAskHuman},
		Constraints: []entity.Constraint{
			{Instrument: "size_flash", Max: &flash},
		},
		Body: "# Steering\n\nfocus on loops\n",
	}
	if err := s.WriteGoal(g); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateState(func(st *store.State) error {
		st.CurrentGoalID = gID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	back, err := s.ReadGoal(gID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Objective.Instrument != "qemu_cycles" {
		t.Errorf("round trip instrument: %q", back.Objective.Instrument)
	}
	if back.Status != entity.GoalStatusActive {
		t.Errorf("status round trip: %q", back.Status)
	}

	active, err := s.ActiveGoal()
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != gID {
		t.Errorf("ActiveGoal id: %q", active.ID)
	}

	// Conclude the first, start a second derived from it.
	closed := time.Now().UTC()
	back.Status = entity.GoalStatusConcluded
	back.ClosedAt = &closed
	if err := s.WriteGoal(back); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateState(func(st *store.State) error {
		st.CurrentGoalID = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	gID2, err := s.AllocID(store.KindGoal)
	if err != nil {
		t.Fatal(err)
	}
	if gID2 != "G-0002" {
		t.Errorf("second goal id: %q", gID2)
	}
	g2 := &entity.Goal{
		ID: gID2, Status: entity.GoalStatusActive, DerivedFrom: gID,
		CreatedAt: &now,
		Objective: entity.Objective{Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease"},
		Constraints: []entity.Constraint{
			{Instrument: "size_flash", Max: &flash},
		},
	}
	if err := s.WriteGoal(g2); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateState(func(st *store.State) error {
		st.CurrentGoalID = gID2
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListGoals()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("ListGoals: got %d, want 2", len(all))
	}
	if all[1].DerivedFrom != gID {
		t.Errorf("second goal should derive from %s, got %q", gID, all[1].DerivedFrom)
	}

	active2, err := s.ActiveGoal()
	if err != nil {
		t.Fatal(err)
	}
	if active2.ID != gID2 {
		t.Errorf("active goal after switch: %q", active2.ID)
	}
})

// TestOpenMigratesLegacyGoal builds a v1 store shape on disk — a
// .research/goal.md plus a hypothesis without goal_id — and verifies that
// store.Open() runs the v1->v2 migration end-to-end: the legacy file is
// removed, the new .research/goals/G-0001.md exists with status=active,
// state.current_goal_id points at it, and the hypothesis is stamped.
var _ = testkit.Spec("TestOpenMigratesLegacyGoal", func(t testkit.T) {
	s, dir := mustCreate(t)

	// Fake v1 shape: drop a legacy goal.md, downgrade schema_version to 1,
	// write a hypothesis without goal_id, and remove the goals/ dir so the
	// migration has room to reconstruct it.
	legacyData := []byte(`---
schema_version: 1
objective:
  instrument: qemu_cycles
  target: dsp_fir
  direction: decrease
  target_effect: 0.15
constraints:
  - instrument: size_flash
    max: 65536
---

# Steering

start with unrolling
`)
	if err := os.WriteFile(filepath.Join(dir, ".research", "goal.md"), legacyData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a hypothesis without a goal_id — as a v1 store would have it.
	hID, err := s.AllocID(store.KindHypothesis)
	if err != nil {
		t.Fatal(err)
	}
	h := &entity.Hypothesis{
		ID: hID, Claim: "unroll dsp_fir",
		Predicts:  entity.Predicts{Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease", MinEffect: 0.1},
		KillIf:    []string{"flash grows"},
		Status:    entity.StatusOpen,
		Author:    "human:alice",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.WriteHypothesis(h); err != nil {
		t.Fatal(err)
	}

	// Downgrade schema_version so Open treats this as a v1 store that
	// needs migrating.
	if err := s.UpdateState(func(st *store.State) error {
		st.SchemaVersion = 1
		st.CurrentGoalID = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Remove the goals/ directory so it is recreated by the migration.
	_ = os.RemoveAll(filepath.Join(dir, ".research", "goals"))

	// Re-open. maybeMigrate runs inside Open().
	s2, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open after legacy prep: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".research", "goal.md")); !os.IsNotExist(err) {
		t.Errorf("legacy goal.md should be removed after migration: %v", err)
	}

	st, err := s2.State()
	if err != nil {
		t.Fatal(err)
	}
	if st.SchemaVersion != store.StateSchemaVersion {
		t.Errorf("schema_version after migrate: got %d, want %d", st.SchemaVersion, store.StateSchemaVersion)
	}
	if st.CurrentGoalID != "G-0001" {
		t.Errorf("current_goal_id after migrate: %q", st.CurrentGoalID)
	}

	g, err := s2.ReadGoal("G-0001")
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != entity.GoalStatusActive {
		t.Errorf("migrated goal status: %q", g.Status)
	}
	if g.CreatedAt == nil {
		t.Error("migrated goal created_at should be populated from the legacy mtime")
	}
	if g.Objective.Instrument != "qemu_cycles" {
		t.Errorf("migrated goal objective lost: %+v", g.Objective)
	}
	if g.Completion == nil || g.Completion.Threshold != 0.15 || g.Completion.OnThreshold != entity.GoalOnThresholdAskHuman {
		t.Errorf("migrated goal should preserve legacy target_effect as completion, got %+v", g.Completion)
	}

	hBack, err := s2.ReadHypothesis(hID)
	if err != nil {
		t.Fatal(err)
	}
	if hBack.GoalID != "G-0001" {
		t.Errorf("hypothesis goal_id after migrate: %q", hBack.GoalID)
	}

	// Re-opening the already-migrated store must be a no-op.
	s3, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s3.ActiveGoal(); err != nil {
		t.Errorf("ActiveGoal on re-open: %v", err)
	}
})
