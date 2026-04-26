package entity_test

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Hypothesis markdown serialization", func() {
	It("round-trips prediction metadata, kill criteria, and body", func() {
		h := &entity.Hypothesis{
			ID:     "H-0001",
			Parent: "",
			Claim:  "unrolling dsp_fir by 4 reduces cycles >10%",
			Predicts: entity.Predicts{
				Instrument: "qemu_cycles",
				Target:     "dsp_fir_bench",
				Direction:  "decrease",
				MinEffect:  0.10,
			},
			KillIf:                  []string{"flash delta > 1024 bytes", "CI crosses zero"},
			InspiredBy:              []string{"L-0001"},
			AllowInvalidatedLessons: true,
			Status:                  entity.StatusOpen,
			Author:                  "human:alice",
			CreatedAt:               time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC),
			Tags:                    []string{"perf", "unroll"},
			Body:                    "# Notes\n\nIdea from the CMSIS reference.\n",
		}

		data, err := h.Marshal()
		Expect(err).NotTo(HaveOccurred())
		back, err := entity.ParseHypothesis(data)
		Expect(err).NotTo(HaveOccurred())

		Expect(back.ID).To(Equal(h.ID))
		Expect(back.Claim).To(Equal(h.Claim))
		Expect(back.Predicts).To(Equal(h.Predicts))
		Expect(back.KillIf).To(Equal(h.KillIf))
		Expect(back.InspiredBy).To(Equal(h.InspiredBy))
		Expect(back.AllowInvalidatedLessons).To(BeTrue())
		Expect(back.Body).To(Equal(h.Body))
	})
})
