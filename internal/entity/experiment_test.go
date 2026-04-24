package entity_test

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Experiment markdown serialization", func() {
	It("round-trips lifecycle metadata, baseline links, and body", func() {
		e := &entity.Experiment{
			ID:         "E-0001",
			GoalID:     "G-0001",
			Hypothesis: "H-0001",
			Status:     entity.ExpDesigned,
			Baseline: entity.Baseline{
				Ref: "HEAD",
				SHA: "abcdef1234567890",
			},
			Instruments: []string{"host_compile", "host_test", "host_timing"},
			Attempt:     2,
			Budget: entity.Budget{
				WallTimeS:  600,
				MaxSamples: 30,
			},
			Author:                 "agent:designer",
			CreatedAt:              time.Date(2026, 4, 11, 14, 0, 0, 0, time.UTC),
			Body:                   "# Plan\n\nUnroll the inner loop.\n",
			ReferencedAsBaselineBy: []string{"C-0002", "C-0005"},
		}

		data, err := e.Marshal()
		Expect(err).NotTo(HaveOccurred())
		back, err := entity.ParseExperiment(data)
		Expect(err).NotTo(HaveOccurred())

		Expect(back.ID).To(Equal("E-0001"))
		Expect(back.GoalID).To(Equal("G-0001"))
		Expect(back.Hypothesis).To(Equal("H-0001"))
		Expect(back.Instruments).To(Equal([]string{"host_compile", "host_test", "host_timing"}))
		Expect(back.Baseline.SHA).To(Equal("abcdef1234567890"))
		Expect(back.Budget.MaxSamples).To(Equal(30))
		Expect(back.Attempt).To(Equal(2))
		Expect(back.Body).To(Equal(e.Body))
		Expect(back.ReferencedAsBaselineBy).To(Equal([]string{"C-0002", "C-0005"}))
	})
})
