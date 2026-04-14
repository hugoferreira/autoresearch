package cli

import (
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

func TestNewFollowEventFilter_RefreshesBaselineGoalScope(t *testing.T) {
	s := scopedFixtureStore(t)
	filter := newFollowEventFilter(s, goalScope{GoalID: "G-0001"}, func(store.Event) bool { return true })

	// Warm the historical baseline cache the same way `log --follow` does
	// before the tail loop starts.
	if !filter(store.Event{Kind: "experiment.baseline", Subject: "E-0001"}) {
		t.Fatal("historical baseline should match goal scope")
	}

	now := time.Now().UTC()
	baseline := &entity.Experiment{
		ID:          "E-0004",
		IsBaseline:  true,
		Status:      entity.ExpMeasured,
		Baseline:    entity.Baseline{Ref: "HEAD", SHA: "ghi789"},
		Instruments: []string{"host_timing"},
		Author:      "system",
		CreatedAt:   now,
	}
	if err := s.WriteExperiment(baseline); err != nil {
		t.Fatal(err)
	}

	baselineEvent := store.Event{
		Ts:      now,
		Kind:    "experiment.baseline",
		Actor:   "system",
		Subject: baseline.ID,
		Data:    jsonRaw(map[string]any{"goal": "G-0001"}),
	}
	if err := s.AppendEvent(baselineEvent); err != nil {
		t.Fatal(err)
	}

	obs := &entity.Observation{
		ID:         "O-0003",
		Experiment: baseline.ID,
		Instrument: "host_timing",
		MeasuredAt: now,
		Value:      0.9,
		Unit:       "s",
		Samples:    3,
		Author:     "system",
	}
	if err := s.WriteObservation(obs); err != nil {
		t.Fatal(err)
	}

	observationEvent := store.Event{
		Ts:      now,
		Kind:    "observation.record",
		Actor:   "system",
		Subject: obs.ID,
	}
	if err := s.AppendEvent(observationEvent); err != nil {
		t.Fatal(err)
	}

	if !filter(baselineEvent) {
		t.Fatal("new baseline event should match goal scope after follow starts")
	}
	if !filter(observationEvent) {
		t.Fatal("observation for new baseline should match goal scope after follow starts")
	}
}
