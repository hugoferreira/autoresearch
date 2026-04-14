package cli

import (
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/store"
)

type lessonSyncMode string

const (
	lessonSyncOnAccept    lessonSyncMode = "accept"
	lessonSyncOnDowngrade lessonSyncMode = "downgrade"
	lessonSyncOnAppeal    lessonSyncMode = "appeal"
)

type lessonStateChange struct {
	LessonID   string
	FromStatus string
	ToStatus   string
	FromSource string
	ToSource   string
}

func lessonStatusForSourceChain(sourceChain string) (string, bool) {
	switch sourceChain {
	case entity.LessonSourceSystem, entity.LessonSourceReviewedDecisive:
		return entity.LessonStatusActive, true
	case entity.LessonSourceUnreviewedDecisive:
		return entity.LessonStatusProvisional, true
	case entity.LessonSourceInconclusive:
		return entity.LessonStatusInvalidated, true
	default:
		return "", false
	}
}

func initializeLessonState(s *store.Store, l *entity.Lesson) error {
	if l == nil {
		return nil
	}
	sourceChain, err := firewall.AssessLessonSourceChain(s, l)
	if err != nil {
		return err
	}
	if l.Provenance == nil {
		l.Provenance = &entity.LessonProvenance{}
	}
	l.Provenance.SourceChain = sourceChain
	if status, ok := lessonStatusForSourceChain(sourceChain); ok {
		l.Status = status
		return nil
	}
	l.Status = l.EffectiveStatus()
	return nil
}

func annotateLessonForRead(s *store.Store, l *entity.Lesson) (*entity.Lesson, error) {
	if l == nil {
		return nil, nil
	}
	view := *l
	sourceChain, err := firewall.AssessLessonSourceChain(s, l)
	if err != nil {
		// Legacy lessons may still be readable even if a referenced subject
		// has gone missing. Preserve the stored metadata rather than failing
		// the whole read surface.
		sourceChain = l.EffectiveSourceChain()
		if sourceChain == "" && l.Scope == entity.LessonScopeSystem {
			sourceChain = entity.LessonSourceSystem
		}
	}
	view.Provenance = &entity.LessonProvenance{SourceChain: sourceChain}
	view.Status = lessonDisplayStatus(l, sourceChain)
	return &view, nil
}

func lessonDisplayStatus(l *entity.Lesson, sourceChain string) string {
	if l != nil && l.EffectiveStatus() == entity.LessonStatusSuperseded {
		return entity.LessonStatusSuperseded
	}
	if status, ok := lessonStatusForSourceChain(sourceChain); ok {
		return status
	}
	if l == nil {
		return entity.LessonStatusActive
	}
	return l.EffectiveStatus()
}

func lessonIsSteering(s *store.Store, l *entity.Lesson) bool {
	view, err := annotateLessonForRead(s, l)
	if err != nil || view == nil {
		return false
	}
	if view.Status != entity.LessonStatusActive {
		return false
	}
	if view.Provenance == nil {
		return false
	}
	switch view.Provenance.SourceChain {
	case entity.LessonSourceSystem, entity.LessonSourceReviewedDecisive:
		return true
	default:
		return false
	}
}

func syncHypothesisLessons(s *store.Store, hypID string, mode lessonSyncMode) ([]lessonStateChange, error) {
	lessons, err := s.ListLessons()
	if err != nil {
		return nil, err
	}

	var changes []lessonStateChange
	for _, l := range lessons {
		if l == nil || l.EffectiveStatus() == entity.LessonStatusSuperseded {
			continue
		}
		touches, err := lessonTouchesHypothesis(s, l, hypID)
		if err != nil {
			return nil, err
		}
		if !touches {
			continue
		}

		fromStatus := l.EffectiveStatus()
		fromSource := l.EffectiveSourceChain()
		if fromSource == "" {
			fromSource, _ = firewall.AssessLessonSourceChain(s, l)
		}
		toSource, err := firewall.AssessLessonSourceChain(s, l)
		if err != nil {
			return nil, err
		}
		toStatus := lessonSyncedStatus(mode, toSource)
		if fromStatus == toStatus && fromSource == toSource {
			continue
		}

		l.Status = toStatus
		if l.Provenance == nil {
			l.Provenance = &entity.LessonProvenance{}
		}
		l.Provenance.SourceChain = toSource
		if err := s.WriteLesson(l); err != nil {
			return nil, err
		}
		changes = append(changes, lessonStateChange{
			LessonID:   l.ID,
			FromStatus: fromStatus,
			ToStatus:   toStatus,
			FromSource: fromSource,
			ToSource:   toSource,
		})
	}
	return changes, nil
}

func lessonSyncedStatus(_ lessonSyncMode, sourceChain string) string {
	if status, ok := lessonStatusForSourceChain(sourceChain); ok {
		return status
	}
	return entity.LessonStatusProvisional
}

func lessonTouchesHypothesis(s *store.Store, l *entity.Lesson, hypID string) (bool, error) {
	if l == nil {
		return false, nil
	}
	for _, subject := range l.Subjects {
		switch {
		case subject == hypID:
			return true, nil
		case strings.HasPrefix(subject, "E-"):
			e, err := s.ReadExperiment(subject)
			if err != nil {
				return false, fmt.Errorf("lesson %s subject %s: %w", l.ID, subject, err)
			}
			if e.Hypothesis == hypID {
				return true, nil
			}
		case strings.HasPrefix(subject, "C-"):
			c, err := s.ReadConclusion(subject)
			if err != nil {
				return false, fmt.Errorf("lesson %s subject %s: %w", l.ID, subject, err)
			}
			if c.Hypothesis == hypID {
				return true, nil
			}
		}
	}
	return false, nil
}

func lessonEventKindForStatus(status string) string {
	switch status {
	case entity.LessonStatusActive:
		return "lesson.activate"
	case entity.LessonStatusProvisional:
		return "lesson.provisional"
	case entity.LessonStatusInvalidated:
		return "lesson.invalidate"
	default:
		return "lesson.update"
	}
}
