package readmodel

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	Source             string `json:"source,omitempty"`
	AncestorHypothesis string `json:"ancestor_hypothesis,omitempty"`
	AncestorConclusion string `json:"ancestor_conclusion,omitempty"`
	Note               string `json:"note,omitempty"`
}

// ResolveInferredBaseline resolves conclude's default absolute baseline order:
// candidate-recorded baseline, nearest accepted supported ancestor, then the
// current goal's mapped baseline. It never applies the explicit
// --baseline-experiment override; callers handle that strictly at the CLI.
func ResolveInferredBaseline(s *store.Store, hyp *entity.Hypothesis, candidate *entity.Experiment, instrument string) (*BaselineResolution, error) {
	if s == nil || hyp == nil || candidate == nil {
		return nil, nil
	}
	instrument = strings.TrimSpace(instrument)
	if instrument == "" {
		return nil, nil
	}

	obsByExp := LoadObservationsByExperiment(s)
	var notes []string

	if expID := strings.TrimSpace(candidate.Baseline.Experiment); expID != "" {
		ok, note, err := experimentHasInstrumentData(s, obsByExp, expID, instrument, "candidate recorded baseline")
		if err != nil {
			return nil, err
		}
		if ok {
			return &BaselineResolution{
				ExperimentID: expID,
				Source:       BaselineSourceCandidateRecorded,
			}, nil
		}
		notes = appendNote(notes, note)
	}

	concls, err := s.ListConclusions()
	if err != nil {
		return nil, err
	}
	ancestor, note, err := resolveAncestorSupportedBaseline(s, hyp, instrument, concls, obsByExp)
	if err != nil {
		return nil, err
	}
	notes = appendNote(notes, note)
	if ancestor != nil {
		ancestor.Note = joinNotes(append(notes, ancestor.Note)...)
		return ancestor, nil
	}

	goalBase, note, err := resolveGoalBaseline(s, hyp.GoalID, instrument, obsByExp)
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

func resolveAncestorSupportedBaseline(s *store.Store, hyp *entity.Hypothesis, instrument string, concls []*entity.Conclusion, obsByExp map[string][]*entity.Observation) (*BaselineResolution, string, error) {
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
		var usable []*entity.Conclusion
		for _, c := range accepted {
			ok, _, err := experimentHasInstrumentData(s, obsByExp, c.CandidateExp, instrument, "accepted supported ancestor candidate")
			if err != nil {
				return nil, "", err
			}
			if ok {
				usable = append(usable, c)
			}
		}

		switch len(usable) {
		case 0:
			if len(accepted) > 0 {
				notes = appendNote(notes, fmt.Sprintf("accepted supported ancestor %s has no candidate with observations on instrument %q", parentID, instrument))
			}
		case 1:
			return &BaselineResolution{
				ExperimentID:       usable[0].CandidateExp,
				Source:             BaselineSourceAncestorSupported,
				AncestorHypothesis: parentID,
				AncestorConclusion: usable[0].ID,
			}, joinNotes(notes...), nil
		default:
			ids := make([]string, 0, len(usable))
			for _, c := range usable {
				ids = append(ids, c.ID)
			}
			sort.Strings(ids)
			return nil, "", fmt.Errorf("ancestor %s has multiple accepted supported conclusions with observations on instrument %q: %s; pass --baseline-experiment explicitly", parentID, instrument, strings.Join(ids, ", "))
		}

		parentID = strings.TrimSpace(parent.Parent)
	}

	return nil, joinNotes(notes...), nil
}

func resolveGoalBaseline(s *store.Store, goalID, instrument string, obsByExp map[string][]*entity.Observation) (*BaselineResolution, string, error) {
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
		usable []string
		notes  []string
	)
	for _, expID := range ids {
		ok, note, err := experimentHasInstrumentData(s, obsByExp, expID, instrument, "goal baseline")
		if err != nil {
			return nil, "", err
		}
		if ok {
			usable = append(usable, expID)
			continue
		}
		notes = appendNote(notes, note)
	}

	switch len(usable) {
	case 0:
		return nil, joinNotes(notes...), nil
	case 1:
		return &BaselineResolution{
			ExperimentID: usable[0],
			Source:       BaselineSourceGoalBaseline,
		}, joinNotes(notes...), nil
	default:
		sort.Strings(usable)
		return nil, "", fmt.Errorf("goal %s has multiple baseline experiments with observations on instrument %q: %s; pass --baseline-experiment explicitly", goalID, instrument, strings.Join(usable, ", "))
	}
}

func goalBaselineExperimentIDs(s *store.Store, goalID string) ([]string, error) {
	events, err := s.Events(0)
	if err != nil {
		return nil, err
	}
	type baselinePayload struct {
		Goal string `json:"goal"`
	}
	var ids []string
	seen := map[string]struct{}{}
	for _, ev := range events {
		if ev.Kind != "experiment.baseline" || strings.TrimSpace(ev.Subject) == "" {
			continue
		}
		var payload baselinePayload
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			continue
		}
		if payload.Goal != goalID {
			continue
		}
		if _, dup := seen[ev.Subject]; dup {
			continue
		}
		seen[ev.Subject] = struct{}{}
		ids = append(ids, ev.Subject)
	}
	return ids, nil
}

func experimentHasInstrumentData(s *store.Store, obsByExp map[string][]*entity.Observation, expID, instrument, label string) (bool, string, error) {
	if _, err := s.ReadExperiment(expID); err != nil {
		if errors.Is(err, store.ErrExperimentNotFound) {
			return false, fmt.Sprintf("%s %s does not exist", label, expID), nil
		}
		return false, "", err
	}
	for _, o := range obsByExp[expID] {
		if o != nil && o.Instrument == instrument {
			return true, "", nil
		}
	}
	return false, fmt.Sprintf("%s %s has no observations on instrument %q", label, expID, instrument), nil
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
