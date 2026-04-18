package readmodel

import (
	"sort"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

const (
	ExperimentClassificationLive = "live"
	ExperimentClassificationDead = "dead"
)

// ExperimentReadClass captures whether a persisted experiment should still be
// treated as actionable work by read-only steering surfaces.
type ExperimentReadClass struct {
	Classification   string `json:"classification"`
	HypothesisStatus string `json:"hypothesis_status,omitempty"`
}

// ExperimentReadView is the read-side projection used by list/show surfaces.
type ExperimentReadView struct {
	*entity.Experiment
	Classification   string `json:"classification"`
	HypothesisStatus string `json:"hypothesis_status,omitempty"`
}

// StaleExperimentView is the stale-work read model shared by status/dashboard.
type StaleExperimentView struct {
	ID            string  `json:"id"`
	Hypothesis    string  `json:"hypothesis"`
	Status        string  `json:"status"`
	LastEventKind string  `json:"last_event_kind"`
	StaleMinutes  float64 `json:"stale_minutes"`
}

// InFlightExperimentView is the read model shared by dashboard/TUI for active
// experiments that still need human or agent attention.
type InFlightExperimentView struct {
	ID            string     `json:"id"`
	Hypothesis    string     `json:"hypothesis"`
	Status        string     `json:"status"`
	Instruments   []string   `json:"instruments"`
	ImplementedAt *time.Time `json:"implemented_at,omitempty"`
	ElapsedS      float64    `json:"elapsed_s"`
}

func normalizeExperimentReadClass(class ExperimentReadClass) ExperimentReadClass {
	if class.Classification == "" {
		class.Classification = ExperimentClassificationLive
	}
	return class
}

// LoopActionable reports whether the research loop should still steer from
// this experiment's current hypothesis state. This is intentionally broader
// than gc's "terminal" notion: decisive-but-unreviewed hypotheses are also
// non-actionable for further loop steering even though they are not yet safe
// to reclaim.
func (class ExperimentReadClass) LoopActionable() bool {
	return normalizeExperimentReadClass(class).Classification == ExperimentClassificationLive
}

func ExperimentReadClassForID(classByID map[string]ExperimentReadClass, expID string) ExperimentReadClass {
	return normalizeExperimentReadClass(classByID[expID])
}

// BuildHypothesisStatusIndex indexes hypotheses by ID for read-side
// experiment classification and other projections that only need status.
func BuildHypothesisStatusIndex(hyps []*entity.Hypothesis) map[string]string {
	hypStatus := make(map[string]string, len(hyps))
	for _, h := range hyps {
		if h == nil {
			continue
		}
		hypStatus[h.ID] = h.Status
	}
	return hypStatus
}

// ClassifyExperimentsForReadFromHypotheses derives read-time experiment
// classification from already-loaded hypotheses, avoiding another store read.
func ClassifyExperimentsForReadFromHypotheses(exps []*entity.Experiment, hyps []*entity.Hypothesis) map[string]ExperimentReadClass {
	return classifyExperimentsForRead(exps, BuildHypothesisStatusIndex(hyps))
}

func ClassifyExperimentsForRead(s *store.Store, exps []*entity.Experiment) (map[string]ExperimentReadClass, error) {
	hyps, err := s.ListHypotheses()
	if err != nil {
		return nil, err
	}
	return ClassifyExperimentsForReadFromHypotheses(exps, hyps), nil
}

func AnnotateExperimentsForRead(s *store.Store, exps []*entity.Experiment) ([]*ExperimentReadView, error) {
	classByID, err := ClassifyExperimentsForRead(s, exps)
	if err != nil {
		return nil, err
	}
	out := make([]*ExperimentReadView, 0, len(exps))
	for _, e := range exps {
		if e == nil {
			continue
		}
		class := normalizeExperimentReadClass(classByID[e.ID])
		out = append(out, &ExperimentReadView{
			Experiment:       e,
			Classification:   class.Classification,
			HypothesisStatus: class.HypothesisStatus,
		})
	}
	return out, nil
}

func AnnotateExperimentForRead(s *store.Store, e *entity.Experiment) (*ExperimentReadView, error) {
	views, err := AnnotateExperimentsForRead(s, []*entity.Experiment{e})
	if err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return nil, nil
	}
	return views[0], nil
}

func ReadExperimentForRead(s *store.Store, id string) (*ExperimentReadView, error) {
	e, err := s.ReadExperiment(id)
	if err != nil {
		return nil, err
	}
	return AnnotateExperimentForRead(s, e)
}

func ClassifyAllExperimentsForRead(s *store.Store) (map[string]ExperimentReadClass, error) {
	exps, err := s.ListExperiments()
	if err != nil {
		return nil, err
	}
	return ClassifyExperimentsForRead(s, exps)
}

func LoadExperimentReadClasses(s *store.Store) map[string]ExperimentReadClass {
	classByID, err := ClassifyAllExperimentsForRead(s)
	if err != nil {
		return nil
	}
	return classByID
}

func ClassifyHypothesisStatusForExperimentRead(status string) ExperimentReadClass {
	switch status {
	case entity.StatusUnreviewed, entity.StatusSupported, entity.StatusRefuted, entity.StatusKilled:
		return normalizeExperimentReadClass(ExperimentReadClass{
			Classification:   ExperimentClassificationDead,
			HypothesisStatus: status,
		})
	default:
		return normalizeExperimentReadClass(ExperimentReadClass{})
	}
}

func classifyExperimentForRead(e *entity.Experiment, hypStatus map[string]string) ExperimentReadClass {
	if e == nil {
		return normalizeExperimentReadClass(ExperimentReadClass{})
	}
	return ClassifyHypothesisStatusForExperimentRead(hypStatus[e.Hypothesis])
}

func classifyExperimentsForRead(exps []*entity.Experiment, hypStatus map[string]string) map[string]ExperimentReadClass {
	out := make(map[string]ExperimentReadClass, len(exps))
	for _, e := range exps {
		if e == nil {
			continue
		}
		out[e.ID] = classifyExperimentForRead(e, hypStatus)
	}
	return out
}

func BuildExperimentActivity(exps []*entity.Experiment, classByID map[string]ExperimentReadClass, events []store.Event, staleThreshold time.Duration, now time.Time) (inFlight []InFlightExperimentView, stale []StaleExperimentView) {
	inFlight = BuildInFlightExperiments(exps, classByID, events, now)
	if staleThreshold > 0 {
		stale = FindStaleExperimentsForRead(exps, classByID, events, staleThreshold, now)
	}
	return inFlight, stale
}

func BuildInFlightExperiments(exps []*entity.Experiment, classByID map[string]ExperimentReadClass, events []store.Event, now time.Time) []InFlightExperimentView {
	inFlight := []InFlightExperimentView{}
	for _, e := range exps {
		if e.Status != entity.ExpImplemented && e.Status != entity.ExpMeasured {
			continue
		}
		if len(e.ReferencedAsBaselineBy) > 0 {
			continue
		}
		if !ExperimentReadClassForID(classByID, e.ID).LoopActionable() {
			continue
		}
		row := InFlightExperimentView{
			ID:          e.ID,
			Hypothesis:  e.Hypothesis,
			Status:      e.Status,
			Instruments: append([]string{}, e.Instruments...),
		}
		if ts, kind := FindLastEventForExperiment(events, e.ID); ts != nil && kind == "experiment.implement" {
			row.ImplementedAt = ts
			row.ElapsedS = now.Sub(*ts).Seconds()
		}
		inFlight = append(inFlight, row)
	}
	sort.SliceStable(inFlight, func(i, j int) bool {
		a, b := inFlight[i].ImplementedAt, inFlight[j].ImplementedAt
		if a == nil && b == nil {
			return inFlight[i].ID < inFlight[j].ID
		}
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return a.After(*b)
	})
	return inFlight
}

func FindStaleExperimentsForRead(exps []*entity.Experiment, classByID map[string]ExperimentReadClass, events []store.Event, threshold time.Duration, now time.Time) []StaleExperimentView {
	stale := []StaleExperimentView{}
	for _, e := range exps {
		switch e.Status {
		case entity.ExpDesigned, entity.ExpImplemented, entity.ExpMeasured:
		default:
			continue
		}
		if e.IsBaseline {
			continue
		}
		if !ExperimentReadClassForID(classByID, e.ID).LoopActionable() {
			continue
		}
		ts, kind := FindLastEventForExperiment(events, e.ID)
		if ts == nil {
			continue
		}
		age := now.Sub(*ts)
		if age < threshold {
			continue
		}
		stale = append(stale, StaleExperimentView{
			ID:            e.ID,
			Hypothesis:    e.Hypothesis,
			Status:        e.Status,
			LastEventKind: kind,
			StaleMinutes:  age.Minutes(),
		})
	}
	return stale
}

// FindLastEventForExperiment scans a pre-loaded event list backward for the
// most recent event referencing expID and returns its timestamp and kind.
func FindLastEventForExperiment(events []store.Event, expID string) (ts *time.Time, kind string) {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Subject == expID {
			t := e.Ts
			return &t, e.Kind
		}
	}
	return nil, ""
}
