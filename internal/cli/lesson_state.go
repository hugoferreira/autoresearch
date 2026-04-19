package cli

import (
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/readmodel"
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
	return readmodel.LessonStatusForSourceChain(sourceChain)
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
	view, err := readmodel.AnnotateLessonForRead(s, l)
	if err != nil || view == nil {
		return nil, err
	}
	return view.Lesson, nil
}

func lessonDisplayStatus(l *entity.Lesson, sourceChain string) string {
	return readmodel.LessonDisplayStatus(l, sourceChain)
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
