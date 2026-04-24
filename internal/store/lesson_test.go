package store_test

import (
	"os"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("lesson persistence", func() {
	It("allocates, writes, reads, lists, and checks lessons", func() {
		s, _ := mustCreate()
		id, err := s.AllocID(store.KindLesson)
		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal("L-0001"))
		l := &entity.Lesson{
			ID:        id,
			Claim:     "cache line pressure dominates past 8x unroll",
			Scope:     entity.LessonScopeHypothesis,
			Subjects:  []string{"H-0003", "C-0005"},
			Tags:      []string{"cache"},
			Status:    entity.LessonStatusActive,
			Author:    "agent:analyst",
			CreatedAt: time.Now().UTC(),
		}
		Expect(s.WriteLesson(l)).To(Succeed())

		back, err := s.ReadLesson(id)
		Expect(err).NotTo(HaveOccurred())
		Expect(back.Claim).To(Equal(l.Claim))
		Expect(back.Scope).To(Equal(entity.LessonScopeHypothesis))

		list, err := s.ListLessons()
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(HaveLen(1))
		Expect(list[0].ID).To(Equal(id))

		ok, err := s.LessonExists(id)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		ok, err = s.LessonExists("L-9999")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())
	})

	It("filters lessons by scope and subject", func() {
		s, _ := mustCreate()
		now := time.Now().UTC()

		hyp := &entity.Lesson{
			ID: "L-0001", Claim: "hyp lesson",
			Scope: entity.LessonScopeHypothesis, Subjects: []string{"H-0001", "C-0002"},
			Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: now,
		}
		sys := &entity.Lesson{
			ID: "L-0002", Claim: "system lesson",
			Scope:  entity.LessonScopeSystem,
			Status: entity.LessonStatusActive, Author: "agent:critic", CreatedAt: now,
		}
		Expect(s.WriteLesson(hyp)).To(Succeed())
		Expect(s.WriteLesson(sys)).To(Succeed())

		hypLessons, err := s.ListLessonsByScope(entity.LessonScopeHypothesis)
		Expect(err).NotTo(HaveOccurred())
		Expect(hypLessons).To(HaveLen(1))
		Expect(hypLessons[0].ID).To(Equal("L-0001"))

		sysLessons, err := s.ListLessonsByScope(entity.LessonScopeSystem)
		Expect(err).NotTo(HaveOccurred())
		Expect(sysLessons).To(HaveLen(1))
		Expect(sysLessons[0].ID).To(Equal("L-0002"))

		bySubject, err := s.ListLessonsForSubject("C-0002")
		Expect(err).NotTo(HaveOccurred())
		Expect(bySubject).To(HaveLen(1))
		Expect(bySubject[0].ID).To(Equal("L-0001"))

		none, err := s.ListLessonsForSubject("C-9999")
		Expect(err).NotTo(HaveOccurred())
		Expect(none).To(BeEmpty())
	})

	It("tolerates missing legacy lessons directories and recreates them lazily", func() {
		s, _ := mustCreate()
		Expect(os.RemoveAll(s.LessonsDir())).To(Succeed())

		list, err := s.ListLessons()
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(BeEmpty())

		counts, err := s.Counts()
		Expect(err).NotTo(HaveOccurred())
		Expect(counts["lessons"]).To(Equal(0))

		l := &entity.Lesson{
			ID: "L-0001", Claim: "x", Scope: entity.LessonScopeSystem,
			Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: time.Now().UTC(),
		}
		Expect(s.WriteLesson(l)).To(Succeed())
		back, err := s.ReadLesson("L-0001")
		Expect(err).NotTo(HaveOccurred())
		Expect(back.Claim).To(Equal("x"))
	})

	It("orders lessons by numeric ID across five digits", func() {
		s, _ := mustCreate()
		now := time.Now().UTC()

		for _, l := range []*entity.Lesson{
			{ID: "L-10000", Claim: "ten thousand", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: now},
			{ID: "L-9998", Claim: "nine nine nine eight", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: now},
			{ID: "L-10001", Claim: "ten thousand one", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: now},
			{ID: "L-9999", Claim: "nine nine nine nine", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: now},
		} {
			Expect(s.WriteLesson(l)).To(Succeed())
		}

		got, err := s.ListLessons()
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveLen(4))
		Expect([]string{got[0].ID, got[1].ID, got[2].ID, got[3].ID}).To(Equal([]string{"L-9998", "L-9999", "L-10000", "L-10001"}))
	})
})
