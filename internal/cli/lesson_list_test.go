package cli

import (
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("lesson list", func() {
	BeforeEach(saveGlobals)

	It("defaults to active lessons and allows --status all for audit views", func() {
		dir, s := setupGoalStore()
		now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
		Expect(s.WriteHypothesis(&entity.Hypothesis{
			ID:     "H-0001",
			GoalID: "G-0001",
			Claim:  "prior direction no longer holds",
			Status: entity.StatusInconclusive,
		})).To(Succeed())
		Expect(s.WriteLesson(&entity.Lesson{
			ID:        "L-0001",
			Claim:     "active lesson",
			Scope:     entity.LessonScopeSystem,
			Status:    entity.LessonStatusActive,
			Author:    "agent:analyst",
			CreatedAt: now,
		})).To(Succeed())
		Expect(s.WriteLesson(&entity.Lesson{
			ID:        "L-0002",
			Claim:     "invalidated lesson",
			Scope:     entity.LessonScopeHypothesis,
			Subjects:  []string{"H-0001"},
			Status:    entity.LessonStatusInvalidated,
			Author:    "agent:analyst",
			CreatedAt: now,
		})).To(Succeed())

		defaultRows := runCLIJSON[[]readmodel.LessonSummaryView](dir, "lesson", "list", "--summary")
		Expect(defaultRows).To(HaveLen(1))
		Expect(defaultRows[0].ID).To(Equal("L-0001"))

		allRows := runCLIJSON[[]readmodel.LessonSummaryView](dir, "lesson", "list", "--status", "all", "--summary")
		Expect(allRows).To(HaveLen(2))
		Expect([]string{allRows[0].ID, allRows[1].ID}).To(Equal([]string{"L-0001", "L-0002"}))
		Expect(allRows[1].Status).To(Equal(entity.LessonStatusInvalidated))
	})

	It("supports summary pagination with --since", func() {
		dir, s := setupGoalStore()
		now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

		for _, l := range []*entity.Lesson{
			{
				ID:        "L-0001",
				Claim:     "old lesson",
				Scope:     entity.LessonScopeSystem,
				Status:    entity.LessonStatusActive,
				Author:    "agent:analyst",
				CreatedAt: now,
			},
			{
				ID:        "L-0002",
				Claim:     strings.Repeat("mechanism ", 30),
				Scope:     entity.LessonScopeSystem,
				Status:    entity.LessonStatusActive,
				Tags:      []string{"cache", "audit"},
				Author:    "agent:analyst",
				CreatedAt: now,
			},
			{
				ID:        "L-0003",
				Claim:     "superseded lesson",
				Scope:     entity.LessonScopeSystem,
				Status:    entity.LessonStatusSuperseded,
				Author:    "agent:analyst",
				CreatedAt: now,
			},
		} {
			Expect(s.WriteLesson(l)).To(Succeed())
		}

		got := runCLIJSON[[]readmodel.LessonSummaryView](dir,
			"lesson", "list",
			"--status", "active",
			"--summary",
			"--since", "L-0001",
		)
		Expect(got).To(HaveLen(1))
		Expect(got[0].ID).To(Equal("L-0002"))
		Expect(got[0].ClaimTruncated).To(BeTrue())
		Expect([]rune(got[0].Claim)).To(HaveLen(readmodel.LessonSummaryClaimLimit))
		Expect(got[0].Tags).To(HaveLen(2))
	})

	It("projects requested JSON fields only", func() {
		dir, s := setupGoalStore()
		now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

		Expect(s.WriteLesson(&entity.Lesson{
			ID:        "L-0001",
			Claim:     "projected lesson",
			Scope:     entity.LessonScopeSystem,
			Status:    entity.LessonStatusActive,
			Tags:      []string{"cache"},
			Author:    "agent:analyst",
			CreatedAt: now,
		})).To(Succeed())

		got := runCLIJSON[[]map[string]any](dir,
			"lesson", "list",
			"--fields", "id,scope,tags,status",
		)
		Expect(got).To(HaveLen(1))
		Expect(got[0]).To(HaveLen(4))
		Expect(got[0]).To(HaveKeyWithValue("id", "L-0001"))
		Expect(got[0]).To(HaveKeyWithValue("scope", string(entity.LessonScopeSystem)))
		Expect(got[0]).To(HaveKeyWithValue("status", string(entity.LessonStatusActive)))
		Expect(got[0]["tags"]).To(ConsistOf("cache"))
	})

	It("rejects combining summary and explicit fields", func() {
		dir, _ := setupGoalStore()
		root := Root()
		root.SetArgs([]string{
			"-C", dir,
			"lesson", "list",
			"--summary",
			"--fields", "id,status",
		})

		Expect(root.Execute()).To(MatchError(ContainSubstring("--summary and --fields cannot be combined")))
	})
})
