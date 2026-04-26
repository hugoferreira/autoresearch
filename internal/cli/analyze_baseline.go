package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
)

type analyzeBaselineSelectionError struct {
	message    string
	candidates []readmodel.BaselineCandidate
}

func (e *analyzeBaselineSelectionError) Error() string { return e.message }

func selectAnalyzeBaselineObservations(
	s *store.Store,
	obsIdx *readmodel.ObservationIndex,
	expID string,
	instrument string,
	ref string,
	observationID string,
) ([]*entity.Observation, *readmodel.BaselineResolution, error) {
	if expID == "" && observationID == "" {
		return nil, nil, nil
	}
	if observationID != "" && ref != "" {
		return nil, nil, fmt.Errorf("--baseline-ref and --baseline-observation are mutually exclusive")
	}
	if expID != "" {
		if _, err := s.ReadExperiment(expID); err != nil {
			return nil, nil, err
		}
	}
	var (
		baseObs []*entity.Observation
		res     *readmodel.BaselineResolution
		err     error
	)
	switch {
	case observationID != "":
		baseObs, res, err = selectAnalyzeBaselineObservationID(obsIdx, expID, instrument, observationID)
	case ref != "":
		baseObs, res, err = selectAnalyzeBaselineRef(obsIdx, expID, instrument, ref)
	default:
		baseObs, res, err = selectAnalyzeBaselineSingleScope(obsIdx, expID, instrument)
	}
	if err != nil {
		return nil, nil, err
	}
	if res != nil && res.ExperimentID != "" {
		if _, err := s.ReadExperiment(res.ExperimentID); err != nil {
			return nil, nil, err
		}
	}
	return baseObs, res, nil
}

func selectAnalyzeBaselineObservationID(obsIdx *readmodel.ObservationIndex, expID, instrument, id string) ([]*entity.Observation, *readmodel.BaselineResolution, error) {
	obs := obsIdx.ObservationByID(id)
	if obs == nil {
		return nil, nil, fmt.Errorf("baseline observation %s not found", id)
	}
	scope := readmodel.ObservationScopeFromObservation(obs)
	if expID != "" && scope.Experiment != expID {
		return nil, nil, fmt.Errorf("baseline observation %s belongs to experiment %s, not %s", id, scope.Experiment, expID)
	}
	expID = scope.Experiment
	if expID == "" {
		return nil, nil, fmt.Errorf("baseline observation %s does not record an experiment", id)
	}
	if instrument != "" && obs.Instrument != instrument {
		return nil, nil, fmt.Errorf("baseline observation %s is on instrument %q, not %q", id, obs.Instrument, instrument)
	}
	baseObs := obsIdx.ObservationsForScope(scope)
	if instrument != "" {
		baseObs = filterObservationsByInstrument(baseObs, instrument)
	}
	if len(baseObs) == 0 {
		return nil, nil, fmt.Errorf("baseline observation %s scope has no observations on instrument %q", id, instrument)
	}
	return baseObs, analyzeBaselineResolution(expID, scope), nil
}

func selectAnalyzeBaselineRef(obsIdx *readmodel.ObservationIndex, expID, instrument, ref string) ([]*entity.Observation, *readmodel.BaselineResolution, error) {
	scopes := obsIdx.ScopesForExperimentInstrument(expID, instrument)
	var matches []readmodel.ObservationScope
	for _, scope := range scopes {
		if analyzeBaselineRefMatches(scope.Ref, ref) {
			matches = append(matches, scope)
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil, fmt.Errorf("baseline experiment %s has no observations for baseline ref %s", expID, ref)
	case 1:
		baseObs := obsIdx.ObservationsForScope(matches[0])
		if instrument != "" {
			baseObs = filterObservationsByInstrument(baseObs, instrument)
		}
		return baseObs, analyzeBaselineResolution(expID, matches[0]), nil
	default:
		return nil, nil, analyzeBaselineAmbiguousError(
			fmt.Sprintf("baseline experiment %s has multiple observation scopes for baseline ref %s", expID, ref),
			obsIdx, expID, instrument,
		)
	}
}

func selectAnalyzeBaselineSingleScope(obsIdx *readmodel.ObservationIndex, expID, instrument string) ([]*entity.Observation, *readmodel.BaselineResolution, error) {
	var scopes []readmodel.ObservationScope
	if instrument != "" {
		scopes = obsIdx.ScopesForExperimentInstrument(expID, instrument)
	} else {
		scopes = obsIdx.ScopesForExperiment(expID)
	}
	switch len(scopes) {
	case 0:
		if instrument != "" {
			return nil, nil, fmt.Errorf("baseline experiment %s has no observations on instrument %q", expID, instrument)
		}
		return nil, nil, fmt.Errorf("baseline experiment %s has no observations", expID)
	case 1:
		baseObs := obsIdx.ObservationsForScope(scopes[0])
		if instrument != "" {
			baseObs = filterObservationsByInstrument(baseObs, instrument)
		}
		return baseObs, analyzeBaselineResolution(expID, scopes[0]), nil
	default:
		return nil, nil, analyzeBaselineAmbiguousError(
			fmt.Sprintf("baseline experiment %s has observations for multiple recorded scopes (%s); analyze requires a baseline selector",
				expID, strings.Join(readmodel.SortedObservationScopeLabels(scopes), ", ")),
			obsIdx, expID, instrument,
		)
	}
}

func analyzeBaselineResolution(expID string, scope readmodel.ObservationScope) *readmodel.BaselineResolution {
	return &readmodel.BaselineResolution{
		ExperimentID: expID,
		Attempt:      scope.Attempt,
		Ref:          scope.Ref,
		SHA:          scope.SHA,
		Source:       readmodel.BaselineSourceExplicit,
	}
}

func analyzeBaselineAmbiguousError(message string, obsIdx *readmodel.ObservationIndex, expID, instrument string) error {
	return &analyzeBaselineSelectionError{
		message:    message,
		candidates: obsIdx.BaselineCandidatesForExperimentInstrument(expID, instrument),
	}
}

func analyzeBaselineRefMatches(stored, ref string) bool {
	stored = strings.TrimSpace(stored)
	ref = strings.TrimSpace(ref)
	if stored == "" || ref == "" {
		return false
	}
	if stored == ref {
		return true
	}
	if strings.TrimPrefix(stored, "refs/heads/") == ref {
		return true
	}
	if strings.TrimPrefix(stored, "refs/tags/") == ref {
		return true
	}
	return false
}

func emitAnalyzeBaselineError(w *output.Writer, expID, baselineExp string, err error) {
	candidates, ok := analyzeBaselineErrorCandidates(err)
	if !ok || len(candidates) == 0 {
		return
	}
	baselineExp = inferAnalyzeBaselineErrorExperiment(baselineExp, candidates)
	_ = w.JSON(map[string]any{
		"status":              "error",
		"error":               err.Error(),
		"experiment":          expID,
		"baseline":            baselineExp,
		"baseline_candidates": candidates,
	})
}

func analyzeBaselineErrorCandidates(err error) ([]readmodel.BaselineCandidate, bool) {
	var selectionErr *analyzeBaselineSelectionError
	if errors.As(err, &selectionErr) {
		return selectionErr.candidates, true
	}
	var scopeErr *readmodel.BaselineScopeAmbiguityError
	if errors.As(err, &scopeErr) {
		return scopeErr.Candidates, true
	}
	return nil, false
}

func inferAnalyzeBaselineErrorExperiment(expID string, candidates []readmodel.BaselineCandidate) string {
	expID = strings.TrimSpace(expID)
	if expID != "" {
		return expID
	}
	seen := map[string]struct{}{}
	for _, c := range candidates {
		if strings.TrimSpace(c.Experiment) != "" {
			seen[c.Experiment] = struct{}{}
		}
	}
	if len(seen) != 1 {
		return ""
	}
	for id := range seen {
		return id
	}
	return ""
}

func analyzeBaselineInstrument(requested, fallback string) string {
	if strings.TrimSpace(requested) != "" {
		return strings.TrimSpace(requested)
	}
	return strings.TrimSpace(fallback)
}
