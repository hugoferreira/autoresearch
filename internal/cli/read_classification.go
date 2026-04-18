package cli

import (
	"fmt"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
)

const (
	experimentClassificationLive = readmodel.ExperimentClassificationLive
	experimentClassificationDead = readmodel.ExperimentClassificationDead
)

type experimentReadClass = readmodel.ExperimentReadClass
type experimentReadView = readmodel.ExperimentReadView
type staleExperimentView = readmodel.StaleExperimentView

func experimentReadClassForID(classByID map[string]experimentReadClass, expID string) experimentReadClass {
	return readmodel.ExperimentReadClassForID(classByID, expID)
}

func classifyExperimentsForRead(s *store.Store, exps []*entity.Experiment) (map[string]experimentReadClass, error) {
	return readmodel.ClassifyExperimentsForRead(s, exps)
}

func annotateExperimentsForRead(s *store.Store, exps []*entity.Experiment) ([]*experimentReadView, error) {
	return readmodel.AnnotateExperimentsForRead(s, exps)
}

func annotateExperimentForRead(s *store.Store, e *entity.Experiment) (*experimentReadView, error) {
	return readmodel.AnnotateExperimentForRead(s, e)
}

func readExperimentForRead(s *store.Store, id string) (*experimentReadView, error) {
	return readmodel.ReadExperimentForRead(s, id)
}

func listScopedExperimentsForRead(s *store.Store, scope goalScope) ([]*experimentReadView, error) {
	list, err := s.ListExperiments()
	if err == nil {
		list, err = newGoalScopeResolver(s, scope).filterExperiments(list)
	}
	if err != nil {
		return nil, err
	}
	return annotateExperimentsForRead(s, list)
}

func listExperimentsForHypothesisForRead(s *store.Store, hypID string) ([]*experimentReadView, error) {
	list, err := s.ListExperimentsForHypothesis(hypID)
	if err != nil {
		return nil, err
	}
	return annotateExperimentsForRead(s, list)
}

func classifyAllExperimentsForRead(s *store.Store) (map[string]experimentReadClass, error) {
	return readmodel.ClassifyAllExperimentsForRead(s)
}

func classifyHypothesisStatusForExperimentRead(status string) experimentReadClass {
	return readmodel.ClassifyHypothesisStatusForExperimentRead(status)
}

func validateExperimentClassificationFilter(classification string) error {
	switch classification {
	case "", experimentClassificationLive, experimentClassificationDead:
		return nil
	default:
		return fmt.Errorf("--classification must be %q or %q", experimentClassificationLive, experimentClassificationDead)
	}
}

func experimentClassificationSummary(classification, hypothesisStatus string) string {
	if classification == "" {
		classification = experimentClassificationLive
	}
	if classification != experimentClassificationDead {
		return classification
	}
	if hypothesisStatus == "" {
		return classification
	}
	return fmt.Sprintf("%s (hypothesis=%s)", classification, hypothesisStatus)
}

func experimentClassificationMarker(classification string) string {
	if classification == experimentClassificationDead {
		return "[dead]"
	}
	return ""
}

func findStaleExperimentsForRead(exps []*entity.Experiment, classByID map[string]experimentReadClass, events []store.Event, threshold time.Duration, now time.Time) []staleExperimentView {
	return readmodel.FindStaleExperimentsForRead(exps, classByID, events, threshold, now)
}
