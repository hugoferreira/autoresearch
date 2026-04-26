package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("lesson relevant", func() {
	BeforeEach(saveGlobals)

	It("returns a small ranked subset with active, system, and direct invalidated lessons", func() {
		dir, _ := setupLessonRelevanceFixture()

		got := runCLIJSON[struct {
			ScopeGoalID string                         `json:"scope_goal_id"`
			Hypothesis  string                         `json:"hypothesis"`
			Limit       int                            `json:"limit"`
			Lessons     []readmodel.RelevantLessonView `json:"lessons"`
		}](dir,
			"lesson", "relevant",
			"--goal", "G-0001",
			"--hypothesis", "H-0001",
			"--limit", "3",
		)

		Expect(got.ScopeGoalID).To(Equal("G-0001"))
		Expect(got.Hypothesis).To(Equal("H-0001"))
		Expect(got.Limit).To(Equal(3))
		Expect(got.Lessons).To(HaveLen(3))
		Expect(got.Lessons[0].ID).To(Equal("L-0001"))
		Expect(got.Lessons[0].Reasons).To(ContainElement("cited by current hypothesis"))
		Expect(got.Lessons).To(ContainElement(SatisfyAll(
			HaveField("ID", "L-0002"),
			HaveField("Status", entity.LessonStatusInvalidated),
			HaveField("Reasons", ContainElement("invalidated")),
		)))
		Expect(got.Lessons).To(ContainElement(SatisfyAll(
			HaveField("ID", "L-0003"),
			HaveField("Scope", entity.LessonScopeSystem),
		)))
		Expect(got.Lessons).NotTo(ContainElement(HaveField("ID", "L-0004")))
		Expect(got.Lessons).NotTo(ContainElement(HaveField("ID", "L-0005")))

		text := runCLI(dir, "lesson", "relevant", "--hypothesis", "H-0001", "--limit", "2")
		expectText(text, "L-0001", "score=", "reason:")
	})
})

func setupLessonRelevanceFixture() (string, *store.Store) {
	GinkgoHelper()

	dir, s := setupGoalStore()
	now := time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC)
	Expect(s.WriteGoal(&entity.Goal{
		ID:        "G-0002",
		Status:    entity.GoalStatusActive,
		CreatedAt: &now,
		Objective: entity.Objective{Instrument: "memory", Direction: "decrease"},
	})).To(Succeed())
	for _, h := range []*entity.Hypothesis{
		{
			ID:         "H-0001",
			GoalID:     "G-0001",
			Claim:      "try a histogram follow-up",
			Status:     entity.StatusOpen,
			InspiredBy: []string{"L-0001"},
			Predicts:   entity.Predicts{Instrument: "timing", Target: "kernel", Direction: "decrease", MinEffect: 0.05},
			Tags:       []string{"histogram"},
			Author:     "test",
			CreatedAt:  now,
		},
		{
			ID:        "H-0002",
			GoalID:    "G-0001",
			Claim:     "reviewed source",
			Status:    entity.StatusSupported,
			Predicts:  entity.Predicts{Instrument: "timing", Target: "kernel", Direction: "decrease", MinEffect: 0.05},
			Author:    "test",
			CreatedAt: now,
		},
		{
			ID:        "H-0003",
			GoalID:    "G-0001",
			Claim:     "dead source",
			Status:    entity.StatusInconclusive,
			Predicts:  entity.Predicts{Instrument: "timing", Target: "kernel", Direction: "decrease", MinEffect: 0.05},
			Author:    "test",
			CreatedAt: now,
		},
		{
			ID:        "H-0004",
			GoalID:    "G-0002",
			Claim:     "other goal",
			Status:    entity.StatusSupported,
			Predicts:  entity.Predicts{Instrument: "memory", Target: "allocator", Direction: "decrease", MinEffect: 0.05},
			Author:    "test",
			CreatedAt: now,
		},
	} {
		Expect(s.WriteHypothesis(h)).To(Succeed())
	}
	for _, l := range []*entity.Lesson{
		{
			ID:        "L-0001",
			Claim:     "Use SINGLETON_MISS_HISTOGRAM for the timing follow-up",
			Scope:     entity.LessonScopeHypothesis,
			Subjects:  []string{"H-0002"},
			Status:    entity.LessonStatusActive,
			Tags:      []string{"histogram"},
			Author:    "agent:analyst",
			CreatedAt: now,
		},
		{
			ID:        "L-0002",
			Claim:     "The old cache axis is exhausted for timing",
			Scope:     entity.LessonScopeHypothesis,
			Subjects:  []string{"H-0003"},
			Status:    entity.LessonStatusActive,
			Tags:      []string{"timing"},
			Author:    "agent:analyst",
			CreatedAt: now.Add(time.Minute),
			PredictedEffect: &entity.PredictedEffect{
				Instrument: "timing",
				Direction:  "decrease",
				MinEffect:  0.05,
			},
		},
		{
			ID:        "L-0003",
			Claim:     "The measurement harness has a timing-specific warmup cost",
			Scope:     entity.LessonScopeSystem,
			Status:    entity.LessonStatusActive,
			Tags:      []string{"timing"},
			Author:    "agent:analyst",
			CreatedAt: now.Add(2 * time.Minute),
		},
		{
			ID:        "L-0004",
			Claim:     "Documentation wording is unrelated to this goal",
			Scope:     entity.LessonScopeSystem,
			Status:    entity.LessonStatusActive,
			Tags:      []string{"docs"},
			Author:    "agent:analyst",
			CreatedAt: now.Add(3 * time.Minute),
		},
		{
			ID:        "L-0005",
			Claim:     "Other goal timing-looking lesson should be scoped out",
			Scope:     entity.LessonScopeHypothesis,
			Subjects:  []string{"H-0004"},
			Status:    entity.LessonStatusActive,
			Tags:      []string{"timing"},
			Author:    "agent:analyst",
			CreatedAt: now.Add(4 * time.Minute),
		},
	} {
		Expect(s.WriteLesson(l)).To(Succeed())
	}
	return dir, s
}
