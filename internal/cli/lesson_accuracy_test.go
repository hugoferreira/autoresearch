package cli

import (
	"fmt"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

func TestComputeLessonAccuracy(t *testing.T) {
	tests := []struct {
		name            string
		deltas          []float64
		wantTrend       string
		wantHit         int
		wantOvershoot   int
		wantUndershoot  int
		wantComparisons int
	}{
		{
			name:            "no comparisons",
			deltas:          nil,
			wantTrend:       lessonAccuracyTrendNone,
			wantComparisons: 0,
		},
		{
			name:            "overshoot only",
			deltas:          []float64{-0.05},
			wantTrend:       lessonAccuracyTrendDown,
			wantOvershoot:   1,
			wantComparisons: 1,
		},
		{
			name:            "undershoot only",
			deltas:          []float64{-0.25},
			wantTrend:       lessonAccuracyTrendUp,
			wantUndershoot:  1,
			wantComparisons: 1,
		},
		{
			name:            "overshoot majority",
			deltas:          []float64{-0.05, -0.03, -0.25},
			wantTrend:       lessonAccuracyTrendDown,
			wantOvershoot:   2,
			wantUndershoot:  1,
			wantComparisons: 3,
		},
		{
			name:            "undershoot majority",
			deltas:          []float64{-0.25, -0.30, -0.05},
			wantTrend:       lessonAccuracyTrendUp,
			wantOvershoot:   1,
			wantUndershoot:  2,
			wantComparisons: 3,
		},
		{
			name:            "tie",
			deltas:          []float64{-0.05, -0.25},
			wantTrend:       lessonAccuracyTrendNone,
			wantOvershoot:   1,
			wantUndershoot:  1,
			wantComparisons: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := mustCreateCLIStore(t)
			lesson, nextAt := seedLessonAccuracyFixture(t, s)
			for i, delta := range tc.deltas {
				addInspiredConclusion(t, s, lesson.ID, i+2, nextAt.Add(time.Duration(i)*time.Minute), delta)
			}

			lessons, concls, hyps, err := collectLessonAccuracyInputs(s)
			if err != nil {
				t.Fatal(err)
			}
			reports, summaries, err := computeLessonAccuracy(s, lessons, concls, buildLessonLinkIndex(hyps))
			if err != nil {
				t.Fatal(err)
			}

			summary, ok := summaries[lesson.ID]
			if tc.wantComparisons == 0 {
				if ok {
					t.Fatalf("summary = %+v, want no summary for %s", summary, lesson.ID)
				}
				if len(reports) != 0 {
					t.Fatalf("reports = %+v, want no reports", reports)
				}
				return
			}
			if !ok {
				t.Fatalf("missing summary for %s", lesson.ID)
			}
			if got := summary.trend(); got != tc.wantTrend {
				t.Fatalf("trend = %q, want %q", got, tc.wantTrend)
			}
			if summary.Hit != tc.wantHit || summary.Overshoot != tc.wantOvershoot || summary.Undershoot != tc.wantUndershoot {
				t.Fatalf("summary counts = %+v", summary)
			}
			if len(reports) != 1 {
				t.Fatalf("reports len = %d, want 1", len(reports))
			}
			if got := len(reports[0].Comparisons); got != tc.wantComparisons {
				t.Fatalf("comparisons len = %d, want %d", got, tc.wantComparisons)
			}
		})
	}
}

func seedLessonAccuracyFixture(t *testing.T, s *store.Store) (*entity.Lesson, time.Time) {
	t.Helper()
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
	if err := s.WriteHypothesis(sourceHyp); err != nil {
		t.Fatal(err)
	}

	sourceConc := &entity.Conclusion{
		ID:         "C-0001",
		Hypothesis: sourceHyp.ID,
		Verdict:    entity.VerdictSupported,
		Effect:     entity.Effect{Instrument: "host_timing", DeltaFrac: -0.12},
		ReviewedBy: "agent:gate",
		Author:     "agent:critic",
		CreatedAt:  base.Add(time.Minute),
	}
	if err := s.WriteConclusion(sourceConc); err != nil {
		t.Fatal(err)
	}

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
	if err := s.WriteLesson(lesson); err != nil {
		t.Fatal(err)
	}

	return lesson, lesson.CreatedAt.Add(time.Minute)
}

func addInspiredConclusion(t *testing.T, s *store.Store, lessonID string, n int, createdAt time.Time, delta float64) {
	t.Helper()

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
	if err := s.WriteHypothesis(hyp); err != nil {
		t.Fatal(err)
	}

	conclusion := &entity.Conclusion{
		ID:         fmt.Sprintf("C-%04d", n),
		Hypothesis: hyp.ID,
		Verdict:    entity.VerdictSupported,
		Effect:     entity.Effect{Instrument: "host_timing", DeltaFrac: delta},
		Author:     "agent:analyst",
		CreatedAt:  createdAt.Add(time.Second),
	}
	if err := s.WriteConclusion(conclusion); err != nil {
		t.Fatal(err)
	}
}
