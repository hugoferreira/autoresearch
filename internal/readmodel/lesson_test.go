package readmodel

import (
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("lesson read views", func() {
	It("filters by since ID, status, subject, and tag", func() {
		f := newBaselineFixture()
		lessons := []*entity.Lesson{
			{
				ID:        "L-0001",
				Claim:     "system lesson",
				Scope:     entity.LessonScopeSystem,
				Status:    entity.LessonStatusActive,
				Tags:      []string{"cache"},
				Author:    "agent:analyst",
				CreatedAt: f.now,
			},
			{
				ID:        "L-0002",
				Claim:     "linked lesson",
				Scope:     entity.LessonScopeHypothesis,
				Subjects:  []string{"H-0007"},
				Status:    entity.LessonStatusProvisional,
				Tags:      []string{"cache", "audit"},
				Author:    "agent:analyst",
				CreatedAt: f.now,
			},
			{
				ID:        "L-0003",
				Claim:     "superseded lesson",
				Scope:     entity.LessonScopeSystem,
				Status:    entity.LessonStatusSuperseded,
				Tags:      []string{"cache"},
				Author:    "agent:analyst",
				CreatedAt: f.now,
			},
		}

		got, err := ListLessonsForRead(f.s, lessons, LessonListOptions{
			SinceID: "L-0001",
			Status:  entity.LessonStatusProvisional,
			Subject: "H-0007",
			Tag:     "audit",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveLen(1))
		Expect(got[0].ID).To(Equal("L-0002"))
	})

	It("orders since filtering by numeric lesson ID rather than lexical sort", func() {
		f := newBaselineFixture()
		for _, l := range []*entity.Lesson{
			{
				ID:        "L-10000",
				Claim:     "newer",
				Scope:     entity.LessonScopeSystem,
				Status:    entity.LessonStatusActive,
				Author:    "agent:analyst",
				CreatedAt: f.now,
			},
			{
				ID:        "L-9999",
				Claim:     "older",
				Scope:     entity.LessonScopeSystem,
				Status:    entity.LessonStatusActive,
				Author:    "agent:analyst",
				CreatedAt: f.now,
			},
			{
				ID:        "L-10001",
				Claim:     "newest",
				Scope:     entity.LessonScopeSystem,
				Status:    entity.LessonStatusActive,
				Author:    "agent:analyst",
				CreatedAt: f.now,
			},
		} {
			Expect(f.s.WriteLesson(l)).To(Succeed())
		}

		lessons, err := f.s.ListLessons()
		Expect(err).NotTo(HaveOccurred())
		got, err := ListLessonsForRead(f.s, lessons, LessonListOptions{SinceID: "L-9998"})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveLen(3))
		Expect([]string{got[0].ID, got[1].ID, got[2].ID}).To(Equal([]string{"L-9999", "L-10000", "L-10001"}))
	})

	It("treats status all as no status filter and rejects unknown statuses", func() {
		views := []*LessonReadView{
			{Lesson: &entity.Lesson{ID: "L-0001", Status: entity.LessonStatusActive}},
			{Lesson: &entity.Lesson{ID: "L-0002", Status: entity.LessonStatusInvalidated}},
		}

		got, err := FilterLessonReadViews(views, LessonListOptions{Status: LessonStatusAll})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveLen(2))
		Expect([]string{got[0].ID, got[1].ID}).To(Equal([]string{"L-0001", "L-0002"}))

		_, err = FilterLessonReadViews(views, LessonListOptions{Status: "retired"})
		Expect(err).To(MatchError(ContainSubstring("lesson status filter must be")))
	})

	It("truncates long summary claims while preserving tags", func() {
		view := &LessonReadView{Lesson: &entity.Lesson{
			ID:     "L-0007",
			Claim:  strings.Repeat("mechanism ", 30),
			Scope:  entity.LessonScopeSystem,
			Status: entity.LessonStatusActive,
			Tags:   []string{"cache", "audit"},
			Author: "agent:analyst",
		}}

		got := BuildLessonSummaryViews([]*LessonReadView{view})
		Expect(got).To(HaveLen(1))
		Expect(got[0].ClaimTruncated).To(BeTrue())
		Expect([]rune(got[0].Claim)).To(HaveLen(LessonSummaryClaimLimit))
		Expect(got[0].Tags).To(Equal([]string{"cache", "audit"}))
	})

	It("parses distinct field projections and projects rows", func() {
		fields, err := ParseLessonFields("id,scope,tags,status,source_chain,id")
		Expect(err).NotTo(HaveOccurred())
		Expect(fields).To(HaveLen(5))

		rows := ProjectLessonReadViews([]*LessonReadView{{
			Lesson: &entity.Lesson{
				ID:     "L-0001",
				Claim:  "system lesson",
				Scope:  entity.LessonScopeSystem,
				Status: entity.LessonStatusActive,
				Tags:   []string{"cache"},
				Author: "agent:analyst",
				Provenance: &entity.LessonProvenance{
					SourceChain: entity.LessonSourceSystem,
				},
			},
		}}, fields)

		Expect(rows).To(HaveLen(1))
		Expect(rows[0]["id"]).To(Equal(any("L-0001")))
		Expect(rows[0]["tags"]).To(Equal([]string{"cache"}))
		Expect(rows[0]["source_chain"]).To(Equal(any(entity.LessonSourceSystem)))
	})

	It("rejects unknown projection fields", func() {
		_, err := ParseLessonFields("id,nope")
		Expect(err).To(HaveOccurred())
	})

	It("ranks direct, tagged, system, and invalidated lessons with explicit reasons", func() {
		views := []*LessonReadView{
			{Lesson: &entity.Lesson{
				ID:     "L-0001",
				Claim:  "inspired lesson",
				Scope:  entity.LessonScopeHypothesis,
				Status: entity.LessonStatusActive,
				Tags:   []string{"histogram"},
			}},
			{Lesson: &entity.Lesson{
				ID:     "L-0002",
				Claim:  "invalidated timing warning",
				Scope:  entity.LessonScopeHypothesis,
				Status: entity.LessonStatusInvalidated,
				Tags:   []string{"timing"},
				PredictedEffect: &entity.PredictedEffect{
					Instrument: "timing",
					Direction:  "decrease",
					MinEffect:  0.05,
				},
			}},
			{Lesson: &entity.Lesson{
				ID:     "L-0003",
				Claim:  "system timing lesson",
				Scope:  entity.LessonScopeSystem,
				Status: entity.LessonStatusActive,
				Tags:   []string{"timing"},
			}},
			{Lesson: &entity.Lesson{
				ID:     "L-0004",
				Claim:  "unrelated system lesson",
				Scope:  entity.LessonScopeSystem,
				Status: entity.LessonStatusActive,
				Tags:   []string{"docs"},
			}},
		}

		got := RankRelevantLessons(views, LessonRelevanceContext{
			Goal: &entity.Goal{Objective: entity.Objective{Instrument: "timing", Direction: "decrease"}},
			Hypothesis: &entity.Hypothesis{
				ID:         "H-0001",
				InspiredBy: []string{"L-0001"},
				Predicts:   entity.Predicts{Instrument: "timing", Target: "kernel"},
				Tags:       []string{"histogram"},
			},
			Limit: 10,
		})

		Expect(got).To(HaveLen(3))
		Expect(got[0].ID).To(Equal("L-0001"))
		Expect(got[0].Reasons).To(ContainElement("cited by current hypothesis"))
		Expect(got).To(ContainElement(SatisfyAll(
			HaveField("ID", "L-0002"),
			HaveField("Status", entity.LessonStatusInvalidated),
			HaveField("Reasons", ContainElement("invalidated")),
		)))
		Expect(got).To(ContainElement(HaveField("ID", "L-0003")))
		Expect(got).NotTo(ContainElement(HaveField("ID", "L-0004")))
	})

	It("does not treat status, system scope, or recency as relevance by themselves", func() {
		views := []*LessonReadView{
			{Lesson: &entity.Lesson{
				ID:     "L-0001",
				Claim:  "recent active system lesson",
				Scope:  entity.LessonScopeSystem,
				Status: entity.LessonStatusActive,
				Tags:   []string{"docs"},
			}},
			{Lesson: &entity.Lesson{
				ID:     "L-0002",
				Claim:  "newer active hypothesis lesson",
				Scope:  entity.LessonScopeHypothesis,
				Status: entity.LessonStatusActive,
				Tags:   []string{"unrelated"},
			}},
		}

		got := RankRelevantLessons(views, LessonRelevanceContext{
			Goal:  &entity.Goal{Objective: entity.Objective{Instrument: "timing", Direction: "decrease"}},
			Limit: 10,
		})

		Expect(got).To(BeEmpty())
	})
})
