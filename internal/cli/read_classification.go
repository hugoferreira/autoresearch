package cli

import (
	"fmt"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

const (
	experimentClassificationLive = "live"
	experimentClassificationDead = "dead"
)

type experimentReadClass struct {
	Classification   string `json:"classification"`
	HypothesisStatus string `json:"hypothesis_status,omitempty"`
}

type experimentReadView struct {
	*entity.Experiment
	Classification   string `json:"classification"`
	HypothesisStatus string `json:"hypothesis_status,omitempty"`
}

type staleExperimentView struct {
	ID            string  `json:"id"`
	Hypothesis    string  `json:"hypothesis"`
	Status        string  `json:"status"`
	LastEventKind string  `json:"last_event_kind"`
	StaleMinutes  float64 `json:"stale_minutes"`
}

func normalizeExperimentReadClass(class experimentReadClass) experimentReadClass {
	if class.Classification == "" {
		class.Classification = experimentClassificationLive
	}
	return class
}

// loopActionable reports whether the research loop should still steer from
// this experiment's current hypothesis state. This is intentionally broader
// than gc's "terminal" notion: decisive-but-unreviewed hypotheses are also
// non-actionable for further loop steering even though they are not yet safe
// to reclaim.
func (class experimentReadClass) loopActionable() bool {
	return normalizeExperimentReadClass(class).Classification == experimentClassificationLive
}

func experimentReadClassForID(classByID map[string]experimentReadClass, expID string) experimentReadClass {
	return normalizeExperimentReadClass(classByID[expID])
}

func classifyExperimentsForRead(s *store.Store, exps []*entity.Experiment) (map[string]experimentReadClass, error) {
	hyps, err := s.ListHypotheses()
	if err != nil {
		return nil, err
	}
	hypStatus := make(map[string]string, len(hyps))
	for _, h := range hyps {
		if h == nil {
			continue
		}
		hypStatus[h.ID] = h.Status
	}
	out := make(map[string]experimentReadClass, len(exps))
	for _, e := range exps {
		if e == nil {
			continue
		}
		out[e.ID] = classifyExperimentForRead(e, hypStatus)
	}
	return out, nil
}

func annotateExperimentsForRead(s *store.Store, exps []*entity.Experiment) ([]*experimentReadView, error) {
	classByID, err := classifyExperimentsForRead(s, exps)
	if err != nil {
		return nil, err
	}
	out := make([]*experimentReadView, 0, len(exps))
	for _, e := range exps {
		if e == nil {
			continue
		}
		class := normalizeExperimentReadClass(classByID[e.ID])
		out = append(out, &experimentReadView{
			Experiment:       e,
			Classification:   class.Classification,
			HypothesisStatus: class.HypothesisStatus,
		})
	}
	return out, nil
}

func annotateExperimentForRead(s *store.Store, e *entity.Experiment) (*experimentReadView, error) {
	views, err := annotateExperimentsForRead(s, []*entity.Experiment{e})
	if err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return nil, nil
	}
	return views[0], nil
}

func readExperimentForRead(s *store.Store, id string) (*experimentReadView, error) {
	e, err := s.ReadExperiment(id)
	if err != nil {
		return nil, err
	}
	return annotateExperimentForRead(s, e)
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
	exps, err := s.ListExperiments()
	if err != nil {
		return nil, err
	}
	return classifyExperimentsForRead(s, exps)
}

func classifyExperimentForRead(e *entity.Experiment, hypStatus map[string]string) experimentReadClass {
	if e == nil {
		return normalizeExperimentReadClass(experimentReadClass{})
	}
	return classifyHypothesisStatusForExperimentRead(hypStatus[e.Hypothesis])
}

func classifyHypothesisStatusForExperimentRead(status string) experimentReadClass {
	switch status {
	case entity.StatusUnreviewed, entity.StatusSupported, entity.StatusRefuted, entity.StatusKilled:
		return normalizeExperimentReadClass(experimentReadClass{
			Classification:   experimentClassificationDead,
			HypothesisStatus: status,
		})
	default:
		return normalizeExperimentReadClass(experimentReadClass{})
	}
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
	classification = normalizeExperimentReadClass(experimentReadClass{Classification: classification}).Classification
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
	stale := []staleExperimentView{}
	for _, e := range exps {
		switch e.Status {
		case entity.ExpDesigned, entity.ExpImplemented, entity.ExpMeasured:
		default:
			continue
		}
		if e.IsBaseline {
			continue
		}
		if !experimentReadClassForID(classByID, e.ID).loopActionable() {
			continue
		}
		ts, kind := findLastEventForExperiment(events, e.ID)
		if ts == nil {
			continue
		}
		age := now.Sub(*ts)
		if age < threshold {
			continue
		}
		stale = append(stale, staleExperimentView{
			ID:            e.ID,
			Hypothesis:    e.Hypothesis,
			Status:        e.Status,
			LastEventKind: kind,
			StaleMinutes:  age.Minutes(),
		})
	}
	return stale
}
