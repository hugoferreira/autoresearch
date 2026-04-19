package cli

import (
	"fmt"

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

func listScopedExperimentsForRead(s *store.Store, scope goalScope) ([]*experimentReadView, error) {
	list, err := s.ListExperiments()
	if err == nil {
		list, err = newGoalScopeResolver(s, scope).filterExperiments(list)
	}
	if err != nil {
		return nil, err
	}
	return readmodel.AnnotateExperimentsForRead(s, list)
}

func listExperimentsForHypothesisForRead(s *store.Store, hypID string) ([]*experimentReadView, error) {
	list, err := s.ListExperimentsForHypothesis(hypID)
	if err != nil {
		return nil, err
	}
	return readmodel.AnnotateExperimentsForRead(s, list)
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

func frontierClassificationMarker(classification, hypothesisStatus string) string {
	if classification != experimentClassificationDead {
		return ""
	}
	switch hypothesisStatus {
	case entity.StatusSupported:
		return "[supported]"
	case entity.StatusRefuted:
		return "[refuted]"
	case entity.StatusKilled:
		return "[killed]"
	case entity.StatusUnreviewed:
		return "[pending review]"
	default:
		return experimentClassificationMarker(classification)
	}
}
