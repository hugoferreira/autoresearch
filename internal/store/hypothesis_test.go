package store_test

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("TestHypothesisCRUD", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		s, _ := mustCreate(t)

		id, err := s.AllocID(store.KindHypothesis)
		if err != nil {
			t.Fatal(err)
		}
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
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}

		back, err := s.ReadHypothesis(id)
		if err != nil {
			t.Fatal(err)
		}
		if back.Claim != h.Claim {
			t.Errorf("claim mismatch: %q vs %q", back.Claim, h.Claim)
		}

		list, err := s.ListHypotheses()
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != id {
			t.Errorf("list: %+v", list)
		}
	})
})

var _ = ginkgo.Describe("TestRegisterInstrument", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		s, _ := mustCreate(t)
		if err := s.RegisterInstrument("size_flash", store.Instrument{Unit: "bytes"}); err != nil {
			t.Fatal(err)
		}
		insts, err := s.ListInstruments()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := insts["size_flash"]; !ok {
			t.Errorf("instrument not persisted")
		}
	})
})
