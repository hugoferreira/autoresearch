package cli

import (
	"fmt"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("lesson accuracy", func() {
	DescribeTable("summarizes follow-up conclusion deltas against predicted effect bands",
		func(deltas []float64, wantTrend string, wantHit, wantOvershoot, wantUndershoot, wantComparisons int) {
			s := createCLIStore()
			lesson, nextAt := seedLessonAccuracyFixture(s)
			for i, delta := range deltas {
				addInspiredConclusion(s, lesson.ID, i+2, nextAt.Add(time.Duration(i)*time.Minute), delta)
			}

			lessons, concls, hyps, err := collectLessonAccuracyInputs(s)
			Expect(err).NotTo(HaveOccurred())
			reports, summaries, err := computeLessonAccuracy(s, lessons, concls, buildLessonLinkIndex(hyps))
			Expect(err).NotTo(HaveOccurred())

			summary, ok := summaries[lesson.ID]
			if wantComparisons == 0 {
				Expect(ok).To(BeFalse())
				Expect(reports).To(BeEmpty())
				return
			}
			Expect(ok).To(BeTrue())
			Expect(summary.trend()).To(Equal(wantTrend))
			Expect(summary.Hit).To(Equal(wantHit))
			Expect(summary.Overshoot).To(Equal(wantOvershoot))
			Expect(summary.Undershoot).To(Equal(wantUndershoot))
			Expect(reports).To(HaveLen(1))
			Expect(reports[0].Comparisons).To(HaveLen(wantComparisons))
		},
		Entry("no comparisons", nil, lessonAccuracyTrendNone, 0, 0, 0, 0),
		Entry("overshoot only", []float64{-0.05}, lessonAccuracyTrendDown, 0, 1, 0, 1),
		Entry("undershoot only", []float64{-0.25}, lessonAccuracyTrendUp, 0, 0, 1, 1),
		Entry("overshoot majority", []float64{-0.05, -0.03, -0.25}, lessonAccuracyTrendDown, 0, 2, 1, 3),
		Entry("undershoot majority", []float64{-0.25, -0.30, -0.05}, lessonAccuracyTrendUp, 0, 1, 2, 3),
		Entry("tie", []float64{-0.05, -0.25}, lessonAccuracyTrendNone, 0, 1, 1, 2),
	)
})

func seedLessonAccuracyFixture(s *store.Store) (*entity.Lesson, time.Time) {
	GinkgoHelper()
	base := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

	sourceHyp := &entity.Hypothesis{
		ID:        "H-0001",
		Claim:     "source hypothesis",
		Predicts:  entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
		KillIf:    []string{"flash grows"},
		Status:    entity.StatusSupported,
		Author:    "agent:orchestrator",
		CreatedAt: base,
	}
	Expect(s.WriteHypothesis(sourceHyp)).To(Succeed())

	sourceConc := &entity.Conclusion{
		ID:         "C-0001",
		Hypothesis: sourceHyp.ID,
		Verdict:    entity.VerdictSupported,
		Effect:     entity.Effect{Instrument: "host_timing", DeltaFrac: -0.12},
		ReviewedBy: "agent:gate",
		Author:     "agent:critic",
		CreatedAt:  base.Add(time.Minute),
	}
	Expect(s.WriteConclusion(sourceConc)).To(Succeed())

	lesson := &entity.Lesson{
		ID:       "L-0001",
		Claim:    "keep pushing this direction",
		Scope:    entity.LessonScopeHypothesis,
		Subjects: []string{sourceConc.ID},
		PredictedEffect: &entity.PredictedEffect{
			Instrument: "host_timing",
			Direction:  "decrease",
			MinEffect:  0.10,
			MaxEffect:  0.20,
		},
		Status:     entity.LessonStatusActive,
		Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceReviewedDecisive},
		Author:     "agent:orchestrator",
		CreatedAt:  base.Add(2 * time.Minute),
	}
	Expect(s.WriteLesson(lesson)).To(Succeed())

	return lesson, lesson.CreatedAt.Add(time.Minute)
}

func addInspiredConclusion(s *store.Store, lessonID string, n int, createdAt time.Time, delta float64) {
	GinkgoHelper()

	hyp := &entity.Hypothesis{
		ID:         fmt.Sprintf("H-%04d", n),
		Claim:      fmt.Sprintf("follow-up %d", n),
		Predicts:   entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
		KillIf:     []string{"flash grows"},
		InspiredBy: []string{lessonID},
		Status:     entity.StatusSupported,
		Author:     "agent:orchestrator",
		CreatedAt:  createdAt,
	}
	Expect(s.WriteHypothesis(hyp)).To(Succeed())

	conclusion := &entity.Conclusion{
		ID:         fmt.Sprintf("C-%04d", n),
		Hypothesis: hyp.ID,
		Verdict:    entity.VerdictSupported,
		Effect:     entity.Effect{Instrument: "host_timing", DeltaFrac: delta},
		Author:     "agent:analyst",
		CreatedAt:  createdAt.Add(time.Second),
	}
	Expect(s.WriteConclusion(conclusion)).To(Succeed())
}
