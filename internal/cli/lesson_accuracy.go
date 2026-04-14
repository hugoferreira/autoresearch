package cli

import (
	"fmt"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

const (
	lessonAccuracyHit        = "HIT"
	lessonAccuracyOvershoot  = "OVERSHOOT"
	lessonAccuracyUndershoot = "UNDERSHOOT"
	lessonAccuracyTrendNone  = ""
	lessonAccuracyTrendDown  = "down"
	lessonAccuracyTrendUp    = "up"
)

type lessonAccuracyRow struct {
	LessonID       string  `json:"lesson_id"`
	ConclusionID   string  `json:"conclusion_id"`
	HypothesisID   string  `json:"hypothesis_id"`
	Predicted      string  `json:"predicted"`
	ActualDelta    float64 `json:"actual_delta_frac"`
	Classification string  `json:"classification"`
	Linked         bool    `json:"linked"`
}

type lessonAccuracy struct {
	LessonID    string              `json:"lesson_id"`
	Claim       string              `json:"claim"`
	Instrument  string              `json:"instrument"`
	Direction   string              `json:"direction"`
	MinEffect   float64             `json:"min_effect"`
	MaxEffect   float64             `json:"max_effect,omitempty"`
	Comparisons []lessonAccuracyRow `json:"comparisons"`
}

type lessonAccuracySummary struct {
	Total      int
	Hit        int
	Overshoot  int
	Undershoot int
}

func (s lessonAccuracySummary) trend() string {
	switch {
	case s.Overshoot > s.Undershoot:
		return lessonAccuracyTrendDown
	case s.Undershoot > s.Overshoot:
		return lessonAccuracyTrendUp
	default:
		return lessonAccuracyTrendNone
	}
}

func collectLessonAccuracyInputs(s *store.Store) ([]*entity.Lesson, []*entity.Conclusion, []*entity.Hypothesis, error) {
	lessons, err := s.ListLessons()
	if err != nil {
		return nil, nil, nil, err
	}
	concls, err := s.ListConclusions()
	if err != nil {
		return nil, nil, nil, err
	}
	hyps, err := s.ListHypotheses()
	if err != nil {
		return nil, nil, nil, err
	}
	return lessons, concls, hyps, nil
}

func computeLessonAccuracy(s *store.Store, lessons []*entity.Lesson, concls []*entity.Conclusion, hyps []*entity.Hypothesis) ([]lessonAccuracy, map[string]lessonAccuracySummary, error) {
	inspiredByLesson := make(map[string]map[string]bool, len(hyps))
	for _, h := range hyps {
		for _, lid := range h.InspiredBy {
			if inspiredByLesson[lid] == nil {
				inspiredByLesson[lid] = map[string]bool{}
			}
			inspiredByLesson[lid][h.ID] = true
		}
	}

	var reports []lessonAccuracy
	summaries := make(map[string]lessonAccuracySummary)

	for _, l := range lessons {
		if !lessonIsSteering(s, l) || l.PredictedEffect == nil {
			continue
		}

		pe := l.PredictedEffect
		report := lessonAccuracy{
			LessonID:   l.ID,
			Claim:      l.Claim,
			Instrument: pe.Instrument,
			Direction:  pe.Direction,
			MinEffect:  pe.MinEffect,
			MaxEffect:  pe.MaxEffect,
		}
		summary := lessonAccuracySummary{}

		linkedHyps := inspiredByLesson[l.ID]
		hasLinked := len(linkedHyps) > 0

		for _, c := range concls {
			if c.CreatedAt.Before(l.CreatedAt) {
				continue
			}
			if c.Effect.Instrument != pe.Instrument {
				continue
			}
			if c.Verdict != entity.VerdictSupported && c.Verdict != entity.VerdictRefuted {
				continue
			}

			linked := false
			if hasLinked {
				if !linkedHyps[c.Hypothesis] {
					continue
				}
				linked = true
			}

			classification := classifyLessonAccuracy(pe, c)
			summary.Total++
			switch classification {
			case lessonAccuracyHit:
				summary.Hit++
			case lessonAccuracyOvershoot:
				summary.Overshoot++
			case lessonAccuracyUndershoot:
				summary.Undershoot++
			}

			report.Comparisons = append(report.Comparisons, lessonAccuracyRow{
				LessonID:       l.ID,
				ConclusionID:   c.ID,
				HypothesisID:   c.Hypothesis,
				Predicted:      formatPredictedEffectRange(pe),
				ActualDelta:    c.Effect.DeltaFrac,
				Classification: classification,
				Linked:         linked,
			})
		}

		if len(report.Comparisons) > 0 {
			reports = append(reports, report)
			summaries[l.ID] = summary
		}
	}

	return reports, summaries, nil
}

func classifyLessonAccuracy(pe *entity.PredictedEffect, c *entity.Conclusion) string {
	actual := c.Effect.DeltaFrac
	if pe.Direction == "decrease" {
		actual = -actual
	}
	switch {
	case actual < pe.MinEffect:
		return lessonAccuracyOvershoot
	case pe.MaxEffect > 0 && actual > pe.MaxEffect:
		return lessonAccuracyUndershoot
	default:
		return lessonAccuracyHit
	}
}

func formatPredictedEffectRange(pe *entity.PredictedEffect) string {
	if pe.MaxEffect > 0 {
		return fmt.Sprintf("%.4f–%.4f", pe.MinEffect, pe.MaxEffect)
	}
	return fmt.Sprintf("%.4f", pe.MinEffect)
}
