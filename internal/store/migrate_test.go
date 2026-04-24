package store_test

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("TestOpenMigratesExperimentGoalIDProvenance", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		s, dir := mustCreate(t)

		now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
		goal := &entity.Goal{
			ID:        "G-0001",
			Status:    entity.GoalStatusActive,
			CreatedAt: &now,
			Objective: entity.Objective{Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease"},
		}
		if err := s.WriteGoal(goal); err != nil {
			t.Fatal(err)
		}

		h := &entity.Hypothesis{
			ID:        "H-0001",
			GoalID:    goal.ID,
			Claim:     "unroll dsp_fir",
			Status:    entity.StatusOpen,
			Author:    "human:alice",
			CreatedAt: now,
			Predicts: entity.Predicts{
				Instrument: "qemu_cycles",
				Target:     "dsp_fir",
				Direction:  "decrease",
				MinEffect:  0.1,
			},
			KillIf: []string{"flash grows"},
		}
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}

		hypExp := &entity.Experiment{
			ID:         "E-0001",
			Hypothesis: h.ID,
			Status:     entity.ExpDesigned,
			Baseline:   entity.Baseline{Ref: "HEAD", SHA: "abc123"},
			Author:     "agent:designer",
			CreatedAt:  now,
		}
		if err := s.WriteExperiment(hypExp); err != nil {
			t.Fatal(err)
		}

		baseExp := &entity.Experiment{
			ID:         "E-0002",
			IsBaseline: true,
			Status:     entity.ExpMeasured,
			Baseline:   entity.Baseline{Ref: "HEAD", SHA: "abc123"},
			Author:     "system",
			CreatedAt:  now,
		}
		if err := s.WriteExperiment(baseExp); err != nil {
			t.Fatal(err)
		}

		if err := s.AppendEvent(store.Event{
			Ts:      now,
			Kind:    "experiment.baseline",
			Actor:   "system",
			Subject: baseExp.ID,
			Data:    mustJSONRaw(t, map[string]any{"goal": goal.ID}),
		}); err != nil {
			t.Fatal(err)
		}

		if err := s.UpdateState(func(st *store.State) error {
			st.SchemaVersion = 2
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		s2, err := store.Open(dir)
		if err != nil {
			t.Fatalf("Open after schema downgrade: %v", err)
		}

		st, err := s2.State()
		if err != nil {
			t.Fatal(err)
		}
		if st.SchemaVersion != store.StateSchemaVersion {
			t.Fatalf("schema_version after migrate: got %d, want %d", st.SchemaVersion, store.StateSchemaVersion)
		}

		hypBack, err := s2.ReadExperiment(hypExp.ID)
		if err != nil {
			t.Fatal(err)
		}
		if hypBack.GoalID != goal.ID {
			t.Fatalf("hypothesis-backed experiment goal_id: got %q, want %q", hypBack.GoalID, goal.ID)
		}

		baseBack, err := s2.ReadExperiment(baseExp.ID)
		if err != nil {
			t.Fatal(err)
		}
		if baseBack.GoalID != goal.ID {
			t.Fatalf("baseline experiment goal_id: got %q, want %q", baseBack.GoalID, goal.ID)
		}

		events := eventsByKind(t, s2, "experiment.goal_backfilled")
		if len(events) != 2 {
			t.Fatalf("goal backfill events: got %d, want 2", len(events))
		}

		got := map[string]map[string]any{}
		for _, ev := range events {
			got[ev.Subject] = decodeEventPayload(t, &ev)
		}
		if payload := got[hypExp.ID]; payload == nil {
			t.Fatalf("missing backfill event for %s", hypExp.ID)
		} else {
			if payload["goal_id"] != goal.ID {
				t.Fatalf("hypothesis event goal_id = %v, want %q", payload["goal_id"], goal.ID)
			}
			if payload["source"] != "hypothesis" {
				t.Fatalf("hypothesis event source = %v, want %q", payload["source"], "hypothesis")
			}
		}
		if payload := got[baseExp.ID]; payload == nil {
			t.Fatalf("missing backfill event for %s", baseExp.ID)
		} else {
			if payload["goal_id"] != goal.ID {
				t.Fatalf("baseline event goal_id = %v, want %q", payload["goal_id"], goal.ID)
			}
			if payload["source"] != "legacy_baseline_event" {
				t.Fatalf("baseline event source = %v, want %q", payload["source"], "legacy_baseline_event")
			}
		}
	})
})

var _ = ginkgo.Describe("TestOpenV2ToV3FailsOnMissingLegacyBaselineGoal", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		s, dir := mustCreate(t)

		now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
		baseExp := &entity.Experiment{
			ID:         "E-0001",
			IsBaseline: true,
			Status:     entity.ExpMeasured,
			Baseline:   entity.Baseline{Ref: "HEAD", SHA: "abc123"},
			Author:     "system",
			CreatedAt:  now,
		}
		if err := s.WriteExperiment(baseExp); err != nil {
			t.Fatal(err)
		}

		if err := s.AppendEvent(store.Event{
			Ts:      now,
			Kind:    "experiment.baseline",
			Actor:   "system",
			Subject: baseExp.ID,
			Data:    mustJSONRaw(t, map[string]any{"baseline": "abc123"}),
		}); err != nil {
			t.Fatal(err)
		}

		if err := s.UpdateState(func(st *store.State) error {
			st.SchemaVersion = 2
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		_, err := store.Open(dir)
		if err == nil {
			t.Fatal("Open should fail when legacy baseline ownership is malformed")
		}
		if !strings.Contains(err.Error(), "missing goal ownership") {
			t.Fatalf("Open error = %v, want missing goal ownership", err)
		}
	})
})

var _ = ginkgo.Describe("TestOpenV2ToV3IgnoresMalformedLegacyBaselinePayloadWhenGoalIDPresent", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		s, dir := mustCreate(t)

		now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
		baseExp := &entity.Experiment{
			ID:         "E-0001",
			GoalID:     "G-0001",
			IsBaseline: true,
			Status:     entity.ExpMeasured,
			Baseline:   entity.Baseline{Ref: "HEAD", SHA: "abc123"},
			Author:     "system",
			CreatedAt:  now,
		}
		if err := s.WriteExperiment(baseExp); err != nil {
			t.Fatal(err)
		}

		if err := s.AppendEvent(store.Event{
			Ts:      now,
			Kind:    "experiment.baseline",
			Actor:   "system",
			Subject: baseExp.ID,
			Data:    json.RawMessage(`123`),
		}); err != nil {
			t.Fatal(err)
		}

		if err := s.UpdateState(func(st *store.State) error {
			st.SchemaVersion = 2
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		s2, err := store.Open(dir)
		if err != nil {
			t.Fatalf("Open should succeed when durable goal_id makes legacy replay unnecessary: %v", err)
		}

		st, err := s2.State()
		if err != nil {
			t.Fatal(err)
		}
		if st.SchemaVersion != store.StateSchemaVersion {
			t.Fatalf("schema_version after migrate: got %d, want %d", st.SchemaVersion, store.StateSchemaVersion)
		}

		back, err := s2.ReadExperiment(baseExp.ID)
		if err != nil {
			t.Fatal(err)
		}
		if back.GoalID != baseExp.GoalID {
			t.Fatalf("goal_id after migrate: got %q, want %q", back.GoalID, baseExp.GoalID)
		}

		if events := eventsByKind(t, s2, "experiment.goal_backfilled"); len(events) != 0 {
			t.Fatalf("unexpected experiment.goal_backfilled events: %+v", events)
		}
	})
})

func mustJSONRaw(t testkit.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

func eventsByKind(t testkit.T, s *store.Store, kind string) []store.Event {
	t.Helper()
	events, err := s.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Event
	for _, ev := range events {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

func decodeEventPayload(t testkit.T, e *store.Event) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(e.Data, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return payload
}
