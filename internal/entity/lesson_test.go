package entity_test

import (
	"encoding/json"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Lesson markdown serialization", func() {
	It("round-trips lesson scope, provenance, and body", func() {
		l := &entity.Lesson{
			ID:         "L-0002",
			Claim:      "Loop unrolling past 8× shows no win on FIR_NTAPS=32 — cache line pressure dominates.",
			Scope:      entity.LessonScopeHypothesis,
			Subjects:   []string{"H-0003", "C-0003", "C-0005"},
			Tags:       []string{"cache", "unroll"},
			Status:     entity.LessonStatusProvisional,
			Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceUnreviewedDecisive},
			Author:     "agent:analyst",
			CreatedAt:  time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC),
			Body:       "# Lesson\n\nObserved across three experiments; DeltaFrac plateaus at ~-0.07.\n",
		}

		data, err := l.Marshal()
		Expect(err).NotTo(HaveOccurred())
		back, err := entity.ParseLesson(data)
		Expect(err).NotTo(HaveOccurred())

		Expect(back.ID).To(Equal(l.ID))
		Expect(back.Claim).To(Equal(l.Claim))
		Expect(back.Scope).To(Equal(entity.LessonScopeHypothesis))
		Expect(back.Subjects).To(Equal([]string{"H-0003", "C-0003", "C-0005"}))
		Expect(back.Provenance).NotTo(BeNil())
		Expect(*back.Provenance).To(Equal(entity.LessonProvenance{SourceChain: entity.LessonSourceUnreviewedDecisive}))
		Expect(back.Body).To(Equal(l.Body))
	})

	It("preserves supersession links", func() {
		l := &entity.Lesson{
			ID:             "L-0001",
			Claim:          "obsolete",
			Scope:          entity.LessonScopeSystem,
			Status:         entity.LessonStatusSuperseded,
			SupersededByID: "L-0002",
			CreatedAt:      time.Now().UTC(),
		}

		data, err := l.Marshal()
		Expect(err).NotTo(HaveOccurred())
		back, err := entity.ParseLesson(data)
		Expect(err).NotTo(HaveOccurred())

		Expect(back.Status).To(Equal(entity.LessonStatusSuperseded))
		Expect(back.SupersededByID).To(Equal("L-0002"))
	})
})

var _ = Describe("Lesson JSON serialization", func() {
	It("includes non-empty lesson bodies", func() {
		l := &entity.Lesson{
			ID: "L-0001", Claim: "x", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive,
			Body: "# Lesson\n\nsome insight\n",
		}

		data, err := json.Marshal(l)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(ContainSubstring(`"body"`))

		var back entity.Lesson
		Expect(json.Unmarshal(data, &back)).To(Succeed())
		Expect(back.Body).To(Equal(l.Body))
	})
})
