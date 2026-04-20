package readmodel

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

// ObservationScope is the durable identity of a measured candidate or
// baseline observation set within one experiment.
type ObservationScope struct {
	Experiment string
	Attempt    int
	Ref        string
	SHA        string
}

type observationScopeSelector struct {
	Experiment string
	Attempt    int
	HasAttempt bool
	Ref        string
	HasRef     bool
	SHA        string
	HasSHA     bool
}

func (s observationScopeSelector) hasConstraints() bool {
	return s.HasAttempt || s.HasRef || s.HasSHA
}

// ObservationIndex is the shared read-side view of recorded observations.
// It indexes observations both by experiment and by durable scope.
type ObservationIndex struct {
	byID               map[string]*entity.Observation
	byScope            map[ObservationScope][]*entity.Observation
	scopesByExperiment map[string][]ObservationScope
}

func LoadObservationIndex(s *store.Store) *ObservationIndex {
	idx, err := LoadObservationIndexStrict(s)
	if err != nil {
		return NewObservationIndex(nil)
	}
	return idx
}

func LoadObservationIndexStrict(s *store.Store) (*ObservationIndex, error) {
	all, err := s.ListObservations()
	if err != nil {
		return nil, err
	}
	return NewObservationIndex(all), nil
}

func NewObservationIndex(all []*entity.Observation) *ObservationIndex {
	idx := &ObservationIndex{
		byID:               map[string]*entity.Observation{},
		byScope:            map[ObservationScope][]*entity.Observation{},
		scopesByExperiment: map[string][]ObservationScope{},
	}
	seenScopes := map[string]map[ObservationScope]struct{}{}
	for _, o := range all {
		if o == nil {
			continue
		}
		if id := strings.TrimSpace(o.ID); id != "" {
			idx.byID[id] = o
		}
		expID := strings.TrimSpace(o.Experiment)
		if expID == "" {
			continue
		}
		scope := ObservationScopeFromObservation(o)
		idx.byScope[scope] = append(idx.byScope[scope], o)
		if seenScopes[expID] == nil {
			seenScopes[expID] = map[ObservationScope]struct{}{}
		}
		if _, dup := seenScopes[expID][scope]; dup {
			continue
		}
		seenScopes[expID][scope] = struct{}{}
		idx.scopesByExperiment[expID] = append(idx.scopesByExperiment[expID], scope)
	}
	for expID := range idx.scopesByExperiment {
		sort.Slice(idx.scopesByExperiment[expID], func(i, j int) bool {
			return observationScopeLess(idx.scopesByExperiment[expID][i], idx.scopesByExperiment[expID][j])
		})
	}
	return idx
}

func ObservationScopeFromObservation(o *entity.Observation) ObservationScope {
	if o == nil {
		return ObservationScope{}
	}
	return ObservationScope{
		Experiment: strings.TrimSpace(o.Experiment),
		Attempt:    o.Attempt,
		Ref:        strings.TrimSpace(o.CandidateRef),
		SHA:        strings.TrimSpace(o.CandidateSHA),
	}
}

func FormatObservationScope(scope ObservationScope) string {
	var parts []string
	if scope.Experiment != "" {
		parts = append(parts, scope.Experiment)
	}
	if scope.Attempt > 0 {
		parts = append(parts, fmt.Sprintf("attempt=%d", scope.Attempt))
	}
	switch {
	case scope.Ref != "" && scope.SHA != "":
		parts = append(parts, fmt.Sprintf("%s@%s", scope.Ref, shortScopeSHA(scope.SHA)))
	case scope.Ref != "":
		parts = append(parts, scope.Ref)
	case scope.SHA != "":
		parts = append(parts, shortScopeSHA(scope.SHA))
	}
	if len(parts) == 0 {
		return "(legacy)"
	}
	return strings.Join(parts, " ")
}

func SortedObservationScopeLabels(scopes []ObservationScope) []string {
	labels := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		labels = append(labels, FormatObservationScope(scope))
	}
	sort.Strings(labels)
	return labels
}

func (idx *ObservationIndex) ObservationsForScope(scope ObservationScope) []*entity.Observation {
	if idx == nil {
		return nil
	}
	return idx.byScope[scope]
}

func (idx *ObservationIndex) ScopesForExperiment(expID string) []ObservationScope {
	if idx == nil {
		return nil
	}
	src := idx.scopesByExperiment[strings.TrimSpace(expID)]
	out := make([]ObservationScope, len(src))
	copy(out, src)
	return out
}

func (idx *ObservationIndex) ScopesForExperimentInstrument(expID, instrument string) []ObservationScope {
	if idx == nil {
		return nil
	}
	expID = strings.TrimSpace(expID)
	instrument = strings.TrimSpace(instrument)
	scopes := idx.scopesByExperiment[expID]
	out := make([]ObservationScope, 0, len(scopes))
	for _, scope := range scopes {
		if instrument != "" && !observationsContainInstrument(idx.byScope[scope], instrument) {
			continue
		}
		out = append(out, scope)
	}
	return out
}

func (idx *ObservationIndex) ResolveConclusionCandidateScope(c *entity.Conclusion) (ObservationScope, bool) {
	if idx == nil || c == nil || strings.TrimSpace(c.CandidateExp) == "" {
		return ObservationScope{}, false
	}
	if scope, ok := idx.scopeFromObservationIDs(c.Observations); ok {
		return scope, true
	}
	selector := observationScopeSelector{
		Experiment: strings.TrimSpace(c.CandidateExp),
		Attempt:    c.CandidateAttempt,
		HasAttempt: c.CandidateAttempt > 0,
		Ref:        strings.TrimSpace(c.CandidateRef),
		HasRef:     strings.TrimSpace(c.CandidateRef) != "",
		SHA:        strings.TrimSpace(c.CandidateSHA),
		HasSHA:     strings.TrimSpace(c.CandidateSHA) != "",
	}
	if selector.hasConstraints() {
		if scopes := idx.matchingScopes(selector, ""); len(scopes) == 1 {
			return scopes[0], true
		}
	}
	if scopes := idx.ScopesForExperiment(selector.Experiment); len(scopes) == 1 {
		return scopes[0], true
	}
	return ObservationScope{}, false
}

func (idx *ObservationIndex) ObservationsForConclusionCandidate(c *entity.Conclusion) ([]*entity.Observation, ObservationScope, bool) {
	scope, ok := idx.ResolveConclusionCandidateScope(c)
	if !ok {
		return nil, ObservationScope{}, false
	}
	obs := idx.ObservationsForScope(scope)
	if len(obs) == 0 {
		return nil, ObservationScope{}, false
	}
	return obs, scope, true
}

func (idx *ObservationIndex) scopeFromObservationIDs(ids []string) (ObservationScope, bool) {
	if idx == nil {
		return ObservationScope{}, false
	}
	var (
		scope ObservationScope
		found bool
	)
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		o, ok := idx.byID[id]
		if !ok || o == nil {
			continue
		}
		cur := ObservationScopeFromObservation(o)
		if !found {
			scope = cur
			found = true
			continue
		}
		if cur != scope {
			return ObservationScope{}, false
		}
	}
	return scope, found
}

func (idx *ObservationIndex) matchingScopes(selector observationScopeSelector, instrument string) []ObservationScope {
	if idx == nil || selector.Experiment == "" {
		return nil
	}
	scopes := idx.scopesByExperiment[selector.Experiment]
	out := make([]ObservationScope, 0, len(scopes))
	for _, scope := range scopes {
		if selector.HasAttempt && scope.Attempt != selector.Attempt {
			continue
		}
		if selector.HasRef && scope.Ref != selector.Ref {
			continue
		}
		if selector.HasSHA && scope.SHA != selector.SHA {
			continue
		}
		if instrument != "" && !observationsContainInstrument(idx.byScope[scope], instrument) {
			continue
		}
		out = append(out, scope)
	}
	return out
}

func observationsContainInstrument(obs []*entity.Observation, instrument string) bool {
	for _, o := range obs {
		if o != nil && o.Instrument == instrument {
			return true
		}
	}
	return false
}

func shortScopeSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func observationScopeLess(a, b ObservationScope) bool {
	if a.Attempt != b.Attempt {
		return a.Attempt < b.Attempt
	}
	if a.Ref != b.Ref {
		return a.Ref < b.Ref
	}
	if a.SHA != b.SHA {
		return a.SHA < b.SHA
	}
	return a.Experiment < b.Experiment
}
