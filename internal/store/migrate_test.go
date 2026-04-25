package store_test

import (
	"encoding/json"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("store migrations", func() {
	It("backfills experiment goal IDs from hypotheses and legacy baseline events", func() {
		s, dir := mustCreate()
		now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
		goal := &entity.Goal{
			ID:        "G-0001",
			Status:    entity.GoalStatusActive,
			CreatedAt: &now,
			Objective: entity.Objective{Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease"},
		}
		Expect(s.WriteGoal(goal)).To(Succeed())

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
		Expect(s.WriteHypothesis(h)).To(Succeed())

		hypExp := &entity.Experiment{
			ID:         "E-0001",
			Hypothesis: h.ID,
			Status:     entity.ExpDesigned,
			Baseline:   entity.Baseline{Ref: "HEAD", SHA: "abc123"},
			Author:     "agent:designer",
			CreatedAt:  now,
		}
		Expect(s.WriteExperiment(hypExp)).To(Succeed())

		baseExp := &entity.Experiment{
			ID:         "E-0002",
			IsBaseline: true,
			Status:     entity.ExpMeasured,
			Baseline:   entity.Baseline{Ref: "HEAD", SHA: "abc123"},
			Author:     "system",
			CreatedAt:  now,
		}
		Expect(s.WriteExperiment(baseExp)).To(Succeed())
		Expect(s.AppendEvent(store.Event{
			Ts:      now,
			Kind:    "experiment.baseline",
			Actor:   "system",
			Subject: baseExp.ID,
			Data:    mustJSONRaw(map[string]any{"goal": goal.ID}),
		})).To(Succeed())
		Expect(s.UpdateState(func(st *store.State) error {
			st.SchemaVersion = 2
			return nil
		})).To(Succeed())

		s2, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		st, err := s2.State()
		Expect(err).NotTo(HaveOccurred())
		Expect(st.SchemaVersion).To(Equal(store.StateSchemaVersion))

		hypBack, err := s2.ReadExperiment(hypExp.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(hypBack.GoalID).To(Equal(goal.ID))
		baseBack, err := s2.ReadExperiment(baseExp.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(baseBack.GoalID).To(Equal(goal.ID))

		events := eventsByKind(s2, "experiment.goal_backfilled")
		Expect(events).To(HaveLen(2))
		got := map[string]map[string]any{}
		for _, ev := range events {
			got[ev.Subject] = decodeEventPayload(&ev)
		}
		Expect(got[hypExp.ID]).To(HaveKeyWithValue("goal_id", goal.ID))
		Expect(got[hypExp.ID]).To(HaveKeyWithValue("source", "hypothesis"))
		Expect(got[baseExp.ID]).To(HaveKeyWithValue("goal_id", goal.ID))
		Expect(got[baseExp.ID]).To(HaveKeyWithValue("source", "legacy_baseline_event"))
	})

	It("fails v2 to v3 migration when legacy baseline ownership lacks a goal", func() {
		s, dir := mustCreate()
		now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
		baseExp := &entity.Experiment{
			ID:         "E-0001",
			IsBaseline: true,
			Status:     entity.ExpMeasured,
			Baseline:   entity.Baseline{Ref: "HEAD", SHA: "abc123"},
			Author:     "system",
			CreatedAt:  now,
		}
		Expect(s.WriteExperiment(baseExp)).To(Succeed())
		Expect(s.AppendEvent(store.Event{
			Ts:      now,
			Kind:    "experiment.baseline",
			Actor:   "system",
			Subject: baseExp.ID,
			Data:    mustJSONRaw(map[string]any{"baseline": "abc123"}),
		})).To(Succeed())
		Expect(s.UpdateState(func(st *store.State) error {
			st.SchemaVersion = 2
			return nil
		})).To(Succeed())

		_, err := store.Open(dir)
		Expect(err).To(MatchError(ContainSubstring("missing goal ownership")))
	})

	It("ignores malformed legacy baseline payloads when experiments already have goal IDs", func() {
		s, dir := mustCreate()
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
		Expect(s.WriteExperiment(baseExp)).To(Succeed())
		Expect(s.AppendEvent(store.Event{
			Ts:      now,
			Kind:    "experiment.baseline",
			Actor:   "system",
			Subject: baseExp.ID,
			Data:    json.RawMessage(`123`),
		})).To(Succeed())
		Expect(s.UpdateState(func(st *store.State) error {
			st.SchemaVersion = 2
			return nil
		})).To(Succeed())

		s2, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		st, err := s2.State()
		Expect(err).NotTo(HaveOccurred())
		Expect(st.SchemaVersion).To(Equal(store.StateSchemaVersion))

		back, err := s2.ReadExperiment(baseExp.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(back.GoalID).To(Equal(baseExp.GoalID))
		Expect(eventsByKind(s2, "experiment.goal_backfilled")).To(BeEmpty())
	})
})

func mustJSONRaw(v any) json.RawMessage {
	GinkgoHelper()
	data, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	return data
}

func eventsByKind(s *store.Store, kind string) []store.Event {
	GinkgoHelper()
	events, err := s.Events(0)
	Expect(err).NotTo(HaveOccurred())
	var out []store.Event
	for _, ev := range events {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

func decodeEventPayload(e *store.Event) map[string]any {
	GinkgoHelper()
	var payload map[string]any
	Expect(json.Unmarshal(e.Data, &payload)).To(Succeed())
	return payload
}
