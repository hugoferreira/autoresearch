package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("follow event filtering", func() {
	It("refreshes baseline goal scope after new baseline events arrive", func() {
		s := scopedFixtureStore()
		filter := newFollowEventFilter(s, goalScope{GoalID: "G-0001"}, func(store.Event) bool { return true })

		Expect(filter(store.Event{Kind: "experiment.baseline", Subject: "E-0001"})).To(BeTrue())

		now := time.Now().UTC()
		baseline := &entity.Experiment{
			ID:          "E-0004",
			GoalID:      "G-0001",
			IsBaseline:  true,
			Status:      entity.ExpMeasured,
			Baseline:    entity.Baseline{Ref: "HEAD", SHA: "ghi789"},
			Instruments: []string{"host_timing"},
			Author:      "system",
			CreatedAt:   now,
		}
		Expect(s.WriteExperiment(baseline)).To(Succeed())

		baselineEvent := store.Event{
			Ts:      now,
			Kind:    "experiment.baseline",
			Actor:   "system",
			Subject: baseline.ID,
			Data:    jsonRaw(map[string]any{"note": "goal provenance now lives on the experiment entity"}),
		}
		Expect(s.AppendEvent(baselineEvent)).To(Succeed())

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
		Expect(s.WriteObservation(obs)).To(Succeed())

		observationEvent := store.Event{
			Ts:      now,
			Kind:    "observation.record",
			Actor:   "system",
			Subject: obs.ID,
		}
		Expect(s.AppendEvent(observationEvent)).To(Succeed())

		Expect(filter(baselineEvent)).To(BeTrue())
		Expect(filter(observationEvent)).To(BeTrue())
	})
})
