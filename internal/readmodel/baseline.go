package readmodel

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

const (
	BaselineSourceExplicit          = "explicit"
	BaselineSourceCandidateRecorded = "candidate_recorded"
	BaselineSourceAncestorSupported = "ancestor_supported"
	BaselineSourceGoalBaseline      = "goal_baseline"
)

// BaselineResolution describes how conclude resolved its absolute baseline.
// When ExperimentID is empty, Note explains why no usable baseline could be
// inferred.
type BaselineResolution struct {
	ExperimentID       string `json:"experiment,omitempty"`
	Attempt            int    `json:"attempt,omitempty"`
	Ref                string `json:"ref,omitempty"`
	SHA                string `json:"sha,omitempty"`
	Source             string `json:"source,omitempty"`
	AncestorHypothesis string `json:"ancestor_hypothesis,omitempty"`
	AncestorConclusion string `json:"ancestor_conclusion,omitempty"`
	Note               string `json:"note,omitempty"`
}

func (r *BaselineResolution) Scope() ObservationScope {
	if r == nil {
		return ObservationScope{}
	}
	return ObservationScope{
		Experiment: strings.TrimSpace(r.ExperimentID),
		Attempt:    r.Attempt,
		Ref:        strings.TrimSpace(r.Ref),
		SHA:        strings.TrimSpace(r.SHA),
	}
}

// ResolveInferredBaseline resolves conclude's default absolute baseline order:
// candidate-recorded baseline, nearest accepted supported ancestor, then the
// current goal's mapped baseline. It never applies the explicit
// --baseline-experiment override; callers handle that strictly at the CLI.
func ResolveInferredBaseline(s *store.Store, hyp *entity.Hypothesis, candidate *entity.Experiment, instrument string) (*BaselineResolution, error) {
	obs, err := LoadObservationIndexStrict(s)
	if err != nil {
		return nil, err
	}
	return ResolveInferredBaselineWithIndex(s, obs, hyp, candidate, instrument)
}

func ResolveInferredBaselineWithIndex(s *store.Store, obs *ObservationIndex, hyp *entity.Hypothesis, candidate *entity.Experiment, instrument string) (*BaselineResolution, error) {
	if s == nil || hyp == nil || candidate == nil {
		return nil, nil
	}
	instrument = strings.TrimSpace(instrument)
	if instrument == "" {
		return nil, nil
	}
	var notes []string

	if expID := strings.TrimSpace(candidate.Baseline.Experiment); expID != "" {
		scope, ok, note, err := ResolveExperimentInstrumentScope(s, obs, expID, instrument, "candidate recorded baseline")
		if err != nil {
			return nil, err
		}
		if ok {
			return &BaselineResolution{
				ExperimentID: expID,
				Attempt:      scope.Attempt,
				Ref:          scope.Ref,
				SHA:          scope.SHA,
				Source:       BaselineSourceCandidateRecorded,
			}, nil
		}
		notes = appendNote(notes, note)
	}

	concls, err := s.ListConclusions()
	if err != nil {
		return nil, err
	}
	ancestor, note, err := resolveAncestorSupportedBaseline(s, hyp, instrument, concls, obs)
	if err != nil {
		return nil, err
	}
	notes = appendNote(notes, note)
	if ancestor != nil {
		ancestor.Note = joinNotes(append(notes, ancestor.Note)...)
		return ancestor, nil
	}

	goalBase, note, err := resolveGoalBaseline(s, hyp.GoalID, instrument, obs)
	if err != nil {
		return nil, err
	}
	notes = appendNote(notes, note)
	if goalBase != nil {
		goalBase.Note = joinNotes(append(notes, goalBase.Note)...)
		return goalBase, nil
	}

	if len(notes) == 0 {
		notes = append(notes, fmt.Sprintf("no usable baseline could be inferred for instrument %q", instrument))
	}
	return &BaselineResolution{Note: joinNotes(notes...)}, nil
}

func resolveAncestorSupportedBaseline(s *store.Store, hyp *entity.Hypothesis, instrument string, concls []*entity.Conclusion, obs *ObservationIndex) (*BaselineResolution, string, error) {
	if hyp == nil || strings.TrimSpace(hyp.Parent) == "" {
		return nil, "", nil
	}

	acceptedByHyp := map[string][]*entity.Conclusion{}
	for _, c := range concls {
		if c == nil || c.ReviewedBy == "" || c.Verdict != entity.VerdictSupported || strings.TrimSpace(c.CandidateExp) == "" {
			continue
		}
		acceptedByHyp[c.Hypothesis] = append(acceptedByHyp[c.Hypothesis], c)
	}

	seen := map[string]struct{}{}
	var notes []string
	for parentID := strings.TrimSpace(hyp.Parent); parentID != ""; {
		if _, dup := seen[parentID]; dup {
			return nil, "", fmt.Errorf("hypothesis parent chain cycles at %s", parentID)
		}
		seen[parentID] = struct{}{}

		parent, err := s.ReadHypothesis(parentID)
		if err != nil {
			return nil, "", fmt.Errorf("read parent hypothesis %s: %w", parentID, err)
		}

		accepted := acceptedByHyp[parentID]
		usableByScope := map[ObservationScope]*entity.Conclusion{}
		for _, c := range accepted {
			scope, ok := obs.ResolveConclusionCandidateScope(c)
			if !ok {
				continue
			}
			if !observationsContainInstrument(obs.ObservationsForScope(scope), instrument) {
				continue
			}
			usableByScope[scope] = preferAncestorConclusion(usableByScope[scope], c)
		}

		switch len(usableByScope) {
		case 0:
			if len(accepted) > 0 {
				notes = appendNote(notes, fmt.Sprintf("accepted supported ancestor %s has no candidate with observations on instrument %q", parentID, instrument))
			}
		case 1:
			scope, chosen := onlyAncestorScopeConclusion(usableByScope)
			return &BaselineResolution{
				ExperimentID:       chosen.CandidateExp,
				Attempt:            scope.Attempt,
				Ref:                scope.Ref,
				SHA:                scope.SHA,
				Source:             BaselineSourceAncestorSupported,
				AncestorHypothesis: parentID,
				AncestorConclusion: chosen.ID,
			}, joinNotes(notes...), nil
		default:
			scopeLabels := make([]ObservationScope, 0, len(usableByScope))
			for scope := range usableByScope {
				scopeLabels = append(scopeLabels, scope)
			}
			return nil, "", fmt.Errorf("ancestor %s has multiple accepted supported candidate scopes with observations on instrument %q: %s; pass --baseline-experiment explicitly", parentID, instrument, strings.Join(SortedObservationScopeLabels(scopeLabels), ", "))
		}

		parentID = strings.TrimSpace(parent.Parent)
	}

	return nil, joinNotes(notes...), nil
}

func resolveGoalBaseline(s *store.Store, goalID, instrument string, obs *ObservationIndex) (*BaselineResolution, string, error) {
	goalID = strings.TrimSpace(goalID)
	if goalID == "" {
		return nil, "", nil
	}

	ids, err := goalBaselineExperimentIDs(s, goalID)
	if err != nil {
		return nil, "", err
	}
	if len(ids) == 0 {
		return nil, fmt.Sprintf("goal %s has no recorded baseline experiment", goalID), nil
	}

	var (
		usable []ObservationScope
		notes  []string
	)
	for _, expID := range ids {
		scope, ok, note, err := ResolveExperimentInstrumentScope(s, obs, expID, instrument, "goal baseline")
		if err != nil {
			return nil, "", err
		}
		if ok {
			usable = append(usable, scope)
			continue
		}
		notes = appendNote(notes, note)
	}

	switch len(usable) {
	case 0:
		return nil, joinNotes(notes...), nil
	case 1:
		return &BaselineResolution{
			ExperimentID: usable[0].Experiment,
			Attempt:      usable[0].Attempt,
			Ref:          usable[0].Ref,
			SHA:          usable[0].SHA,
			Source:       BaselineSourceGoalBaseline,
		}, joinNotes(notes...), nil
	default:
		return nil, "", fmt.Errorf("goal %s has multiple baseline scopes with observations on instrument %q: %s; pass --baseline-experiment explicitly", goalID, instrument, strings.Join(SortedObservationScopeLabels(usable), ", "))
	}
}

func goalBaselineExperimentIDs(s *store.Store, goalID string) ([]string, error) {
	exps, err := s.ListExperiments()
	if err != nil {
		return nil, err
	}
	var ids []string
	seen := map[string]struct{}{}
	for _, e := range exps {
		if e == nil || !e.IsBaseline || strings.TrimSpace(e.GoalID) == "" {
			continue
		}
		if e.GoalID != goalID {
			continue
		}
		if _, dup := seen[e.ID]; dup {
			continue
		}
		seen[e.ID] = struct{}{}
		ids = append(ids, e.ID)
	}
	return ids, nil
}

// ResolveExperimentInstrumentScope selects the single usable observation scope
// for one experiment on one instrument. It distinguishes missing experiments,
// missing instrument data, and ambiguous multi-scope measurements.
func ResolveExperimentInstrumentScope(s *store.Store, obs *ObservationIndex, expID, instrument, label string) (ObservationScope, bool, string, error) {
	if _, err := s.ReadExperiment(expID); err != nil {
		if errors.Is(err, store.ErrExperimentNotFound) {
			return ObservationScope{}, false, fmt.Sprintf("%s %s does not exist", label, expID), nil
		}
		return ObservationScope{}, false, "", err
	}
	scopes := obs.ScopesForExperimentInstrument(expID, instrument)
	switch len(scopes) {
	case 0:
		return ObservationScope{}, false, fmt.Sprintf("%s %s has no observations on instrument %q", label, expID, instrument), nil
	case 1:
		return scopes[0], true, "", nil
	default:
		return ObservationScope{}, false, "", fmt.Errorf("%s %s has multiple observation scopes on instrument %q: %s", label, expID, instrument, strings.Join(SortedObservationScopeLabels(scopes), ", "))
	}
}

func preferAncestorConclusion(cur, next *entity.Conclusion) *entity.Conclusion {
	if cur == nil {
		return next
	}
	if next == nil {
		return cur
	}
	if next.CreatedAt.After(cur.CreatedAt) {
		return next
	}
	if next.CreatedAt.Equal(cur.CreatedAt) && next.ID > cur.ID {
		return next
	}
	return cur
}

func onlyAncestorScopeConclusion(m map[ObservationScope]*entity.Conclusion) (ObservationScope, *entity.Conclusion) {
	for scope, c := range m {
		return scope, c
	}
	return ObservationScope{}, nil
}

func appendNote(notes []string, note string) []string {
	note = strings.TrimSpace(note)
	if note == "" {
		return notes
	}
	for _, existing := range notes {
		if existing == note {
			return notes
		}
	}
	return append(notes, note)
}

func joinNotes(notes ...string) string {
	var cleaned []string
	for _, note := range notes {
		cleaned = appendNote(cleaned, note)
	}
	return strings.Join(cleaned, "; ")
}
