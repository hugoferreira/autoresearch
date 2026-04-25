package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("experiment read classification", func() {
	It("annotates hypothesis experiment reads with live/dead metadata", func() {
		s, err := store.Create(GinkgoT().TempDir(), store.Config{
			Build: store.CommandSpec{Command: "true"},
			Test:  store.CommandSpec{Command: "true"},
		})
		Expect(err).NotTo(HaveOccurred())

		now := time.Now().UTC()
		for _, h := range []*entity.Hypothesis{
			{
				ID: "H-0001", Claim: "done", Status: entity.StatusSupported,
				Predicts: entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease"},
				KillIf:   []string{"tests fail"}, Author: "human", CreatedAt: now,
			},
			{
				ID: "H-0002", Claim: "live", Status: entity.StatusOpen,
				Predicts: entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease"},
				KillIf:   []string{"tests fail"}, Author: "human", CreatedAt: now,
			},
		} {
			Expect(s.WriteHypothesis(h)).To(Succeed())
		}
		for _, e := range []*entity.Experiment{
			{ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpMeasured, CreatedAt: now},
			{ID: "E-0002", Hypothesis: "H-0002", Status: entity.ExpMeasured, CreatedAt: now},
		} {
			Expect(s.WriteExperiment(e)).To(Succeed())
		}

		views, err := listExperimentsForHypothesisForRead(s, "H-0001")
		Expect(err).NotTo(HaveOccurred())
		Expect(views).To(HaveLen(1))
		Expect(views[0].ID).To(Equal("E-0001"))
		Expect(views[0].Classification).To(Equal(experimentClassificationDead))
		Expect(views[0].HypothesisStatus).To(Equal(entity.StatusSupported))
	})

	It("validates and renders classification helpers", func() {
		Expect(validateExperimentClassificationFilter("")).To(Succeed())
		Expect(validateExperimentClassificationFilter(experimentClassificationLive)).To(Succeed())
		Expect(validateExperimentClassificationFilter(experimentClassificationDead)).To(Succeed())
		Expect(validateExperimentClassificationFilter("zombie")).To(HaveOccurred())

		Expect(experimentClassificationSummary("", "")).To(Equal(experimentClassificationLive))
		Expect(experimentClassificationSummary(experimentClassificationDead, entity.StatusSupported)).To(Equal("dead (hypothesis=supported)"))
		Expect(experimentClassificationMarker(experimentClassificationDead)).To(Equal("[dead]"))
		Expect(experimentClassificationMarker(experimentClassificationLive)).To(BeEmpty())
		Expect(frontierClassificationMarker(experimentClassificationDead, entity.StatusSupported)).To(Equal("[supported]"))
		Expect(frontierClassificationMarker(experimentClassificationDead, entity.StatusRefuted)).To(Equal("[refuted]"))
		Expect(frontierClassificationMarker(experimentClassificationDead, entity.StatusKilled)).To(Equal("[killed]"))
		Expect(frontierClassificationMarker(experimentClassificationDead, entity.StatusUnreviewed)).To(Equal("[pending review]"))
		Expect(frontierClassificationMarker(experimentClassificationDead, "")).To(Equal("[dead]"))
		Expect(frontierClassificationMarker(experimentClassificationLive, entity.StatusSupported)).To(BeEmpty())
	})
})
