package readmodel

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("experiment read classifications", func() {
	DescribeTable("classifies hypothesis statuses by loop actionability",
		func(status, class, wantStatus string, actionable bool) {
			got := ClassifyHypothesisStatusForExperimentRead(status)
			Expect(got.Classification).To(Equal(class))
			Expect(got.HypothesisStatus).To(Equal(wantStatus))
			Expect(got.LoopActionable()).To(Equal(actionable))
		},
		Entry("missing status defaults live", "", ExperimentClassificationLive, "", true),
		Entry("open stays live", entity.StatusOpen, ExperimentClassificationLive, "", true),
		Entry("inconclusive stays live", entity.StatusInconclusive, ExperimentClassificationLive, "", true),
		Entry("unreviewed is dead", entity.StatusUnreviewed, ExperimentClassificationDead, entity.StatusUnreviewed, false),
		Entry("supported is dead", entity.StatusSupported, ExperimentClassificationDead, entity.StatusSupported, false),
		Entry("refuted is dead", entity.StatusRefuted, ExperimentClassificationDead, entity.StatusRefuted, false),
		Entry("killed is dead", entity.StatusKilled, ExperimentClassificationDead, entity.StatusKilled, false),
	)

	It("applies hypothesis classifications to experiment rows", func() {
		hyps := []*entity.Hypothesis{
			{ID: "H-0001", Status: entity.StatusSupported},
			{ID: "H-0002", Status: entity.StatusOpen},
		}
		exps := []*entity.Experiment{
			{ID: "E-0001", Hypothesis: "H-0001"},
			{ID: "E-0002", Hypothesis: "H-0002"},
			{ID: "E-0003"},
		}

		got := ClassifyExperimentsForReadFromHypotheses(exps, hyps)
		Expect(got["E-0001"].Classification).To(Equal(ExperimentClassificationDead))
		Expect(got["E-0001"].HypothesisStatus).To(Equal(entity.StatusSupported))
		Expect(got["E-0002"].Classification).To(Equal(ExperimentClassificationLive))
		Expect(got["E-0003"].Classification).To(Equal(ExperimentClassificationLive))
	})
})

var _ = Describe("experiment activity read models", func() {
	It("excludes experiments whose hypotheses are no longer loop-actionable from stale work", func() {
		now := time.Date(2026, 4, 18, 20, 0, 0, 0, time.UTC)
		exps := []*entity.Experiment{
			{ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpMeasured},
			{ID: "E-0002", Hypothesis: "H-0002", Status: entity.ExpMeasured},
			{ID: "E-0003", Hypothesis: "H-0003", Status: entity.ExpMeasured},
		}
		classByID := map[string]ExperimentReadClass{
			"E-0001": ClassifyHypothesisStatusForExperimentRead(entity.StatusSupported),
			"E-0002": ClassifyHypothesisStatusForExperimentRead(entity.StatusUnreviewed),
			"E-0003": ClassifyHypothesisStatusForExperimentRead(entity.StatusOpen),
		}
		events := []store.Event{
			{Ts: now.Add(-10 * time.Minute), Kind: "experiment.measure", Subject: "E-0001"},
			{Ts: now.Add(-10 * time.Minute), Kind: "experiment.measure", Subject: "E-0002"},
			{Ts: now.Add(-10 * time.Minute), Kind: "experiment.measure", Subject: "E-0003"},
		}

		stale := FindStaleExperimentsForRead(exps, classByID, events, 5*time.Minute, now)
		Expect(stale).To(HaveLen(1))
		Expect(stale[0].ID).To(Equal("E-0003"))
	})

	It("counts recent observation records as experiment activity", func() {
		now := time.Date(2026, 4, 18, 20, 0, 0, 0, time.UTC)
		exps := []*entity.Experiment{
			{ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpMeasured},
		}
		classByID := map[string]ExperimentReadClass{
			"E-0001": ClassifyHypothesisStatusForExperimentRead(entity.StatusOpen),
		}
		events := []store.Event{
			{Ts: now.Add(-10 * time.Minute), Kind: "experiment.implement", Subject: "E-0001"},
			{
				Ts:      now.Add(-1 * time.Minute),
				Kind:    "observation.record",
				Subject: "O-0001",
				Data:    []byte(`{"experiment":"E-0001"}`),
			},
		}

		stale := FindStaleExperimentsForRead(exps, classByID, events, 5*time.Minute, now)
		Expect(stale).To(BeEmpty())
	})

	It("filters and orders in-flight experiments by recent implementation activity", func() {
		now := time.Date(2026, 4, 18, 20, 0, 0, 0, time.UTC)
		exps := []*entity.Experiment{
			{
				ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpMeasured,
				Instruments:            []string{"host_timing"},
				ReferencedAsBaselineBy: []string{"C-0001"},
			},
			{
				ID: "E-0002", Hypothesis: "H-0002", Status: entity.ExpMeasured,
				Instruments: []string{"host_timing"},
			},
			{
				ID: "E-0003", Hypothesis: "H-0003", Status: entity.ExpImplemented,
				Instruments: []string{"host_timing"},
			},
			{
				ID: "E-0004", Hypothesis: "H-0004", Status: entity.ExpMeasured,
				Instruments: []string{"host_timing", "host_test"},
			},
			{
				ID: "E-0005", Hypothesis: "H-0005", Status: entity.ExpMeasured,
				Instruments: []string{"host_timing"},
			},
		}
		classByID := map[string]ExperimentReadClass{
			"E-0002": ClassifyHypothesisStatusForExperimentRead(entity.StatusSupported),
		}
		old := now.Add(-10 * time.Minute)
		recent := now.Add(-2 * time.Minute)
		events := []store.Event{
			{Ts: old, Kind: "experiment.implement", Subject: "E-0003"},
			{Ts: recent, Kind: "experiment.implement", Subject: "E-0004"},
		}

		inFlight := BuildInFlightExperiments(exps, classByID, events, now)
		Expect(inFlight).To(HaveLen(3))
		Expect([]string{inFlight[0].ID, inFlight[1].ID, inFlight[2].ID}).To(Equal([]string{"E-0004", "E-0003", "E-0005"}))
		Expect(inFlight[0].ElapsedS).To(Equal(120.0))
		Expect(inFlight[1].ElapsedS).To(Equal(600.0))
		Expect(inFlight[2].ImplementedAt).To(BeNil())
	})

	It("uses one threshold and clock for both in-flight and stale projections", func() {
		now := time.Date(2026, 4, 18, 20, 0, 0, 0, time.UTC)
		exps := []*entity.Experiment{
			{ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpMeasured, Instruments: []string{"host_timing"}},
		}
		classByID := map[string]ExperimentReadClass{
			"E-0001": ClassifyHypothesisStatusForExperimentRead(entity.StatusOpen),
		}
		events := []store.Event{
			{Ts: now.Add(-10 * time.Minute), Kind: "experiment.implement", Subject: "E-0001"},
		}

		inFlight, stale := BuildExperimentActivity(exps, classByID, events, 5*time.Minute, now)
		Expect(inFlight).To(HaveLen(1))
		Expect(stale).To(HaveLen(1))
		Expect(inFlight[0].ElapsedS).To(Equal(600.0))
		Expect(stale[0].StaleMinutes).To(Equal(10.0))
	})
})
