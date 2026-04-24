package store_test

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("hypothesis persistence", func() {
	It("allocates, writes, reads, and lists hypotheses", func() {
		s, _ := mustCreate()
		id, err := s.AllocID(store.KindHypothesis)
		Expect(err).NotTo(HaveOccurred())
		h := &entity.Hypothesis{
			ID:    id,
			Claim: "unroll cuts cycles",
			Predicts: entity.Predicts{
				Instrument: "qemu_cycles", Target: "dsp", Direction: "decrease", MinEffect: 0.1,
			},
			KillIf:    []string{"flash grew"},
			Status:    entity.StatusOpen,
			Author:    "human:alice",
			CreatedAt: time.Now().UTC(),
		}

		Expect(s.WriteHypothesis(h)).To(Succeed())
		back, err := s.ReadHypothesis(id)
		Expect(err).NotTo(HaveOccurred())
		Expect(back.Claim).To(Equal(h.Claim))

		list, err := s.ListHypotheses()
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(HaveLen(1))
		Expect(list[0].ID).To(Equal(id))
	})

	It("registers instruments in config", func() {
		s, _ := mustCreate()
		Expect(s.RegisterInstrument("size_flash", store.Instrument{Unit: "bytes"})).To(Succeed())
		insts, err := s.ListInstruments()
		Expect(err).NotTo(HaveOccurred())
		Expect(insts).To(HaveKey("size_flash"))
	})
})
