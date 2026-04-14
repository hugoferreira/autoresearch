package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

const goalScopeAll = "all"

type goalScope struct {
	GoalID string
	All    bool
}

func (s goalScope) label() string {
	if s.All || s.GoalID == "" {
		return goalScopeAll
	}
	return s.GoalID
}

func (s goalScope) payload() map[string]any {
	out := map[string]any{"scope_all": s.All}
	if !s.All && s.GoalID != "" {
		out["scope_goal_id"] = s.GoalID
	}
	return out
}

func resolveGoalScope(s *store.Store, flag string) (goalScope, error) {
	flag = strings.TrimSpace(flag)
	if flag == "" {
		st, err := s.State()
		if err != nil {
			return goalScope{}, err
		}
		if st.CurrentGoalID == "" {
			return goalScope{All: true}, nil
		}
		return goalScope{GoalID: st.CurrentGoalID}, nil
	}
	if flag == goalScopeAll {
		return goalScope{All: true}, nil
	}
	ok, err := s.GoalExists(flag)
	if err != nil {
		return goalScope{}, err
	}
	if !ok {
		return goalScope{}, fmt.Errorf("--goal %s: goal does not exist", flag)
	}
	return goalScope{GoalID: flag}, nil
}

func mergeGoalScopePayload(payload map[string]any, scope goalScope) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	for k, v := range scope.payload() {
		payload[k] = v
	}
	return payload
}

func isNotFound(err error) bool {
	return errors.Is(err, store.ErrGoalNotFound) ||
		errors.Is(err, store.ErrHypothesisNotFound) ||
		errors.Is(err, store.ErrExperimentNotFound) ||
		errors.Is(err, store.ErrObservationNotFound) ||
		errors.Is(err, store.ErrConclusionNotFound) ||
		errors.Is(err, store.ErrLessonNotFound)
}

type goalScopeResolver struct {
	store *store.Store
	scope goalScope

	hypGoals       map[string]string
	expGoals       map[string]string
	conclusionMap  map[string]string
	observationMap map[string]string

	eventsLoaded bool
	events       []store.Event

	baselineLoaded bool
	baselineGoals  map[string]string
}

func newGoalScopeResolver(s *store.Store, scope goalScope) *goalScopeResolver {
	return &goalScopeResolver{
		store:          s,
		scope:          scope,
		hypGoals:       map[string]string{},
		expGoals:       map[string]string{},
		conclusionMap:  map[string]string{},
		observationMap: map[string]string{},
		baselineGoals:  map[string]string{},
	}
}

func (r *goalScopeResolver) inScope(goalID string) bool {
	if r.scope.All {
		return true
	}
	return goalID != "" && goalID == r.scope.GoalID
}

func (r *goalScopeResolver) hypothesisGoal(id string) (string, error) {
	if id == "" {
		return "", nil
	}
	if gid, ok := r.hypGoals[id]; ok {
		return gid, nil
	}
	h, err := r.store.ReadHypothesis(id)
	if err != nil {
		if isNotFound(err) {
			r.hypGoals[id] = ""
			return "", nil
		}
		return "", err
	}
	r.hypGoals[id] = h.GoalID
	return h.GoalID, nil
}

func (r *goalScopeResolver) ensureEvents() error {
	if r.eventsLoaded {
		return nil
	}
	all, err := r.store.Events(0)
	if err != nil {
		return err
	}
	r.events = all
	r.eventsLoaded = true
	return nil
}

func (r *goalScopeResolver) ensureBaselineGoals() error {
	if r.baselineLoaded {
		return nil
	}
	if err := r.ensureEvents(); err != nil {
		return err
	}
	type baselinePayload struct {
		Goal string `json:"goal"`
	}
	for _, ev := range r.events {
		if ev.Kind != "experiment.baseline" || ev.Subject == "" {
			continue
		}
		var payload baselinePayload
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			continue
		}
		if payload.Goal != "" {
			r.baselineGoals[ev.Subject] = payload.Goal
		}
	}
	r.baselineLoaded = true
	return nil
}

func (r *goalScopeResolver) experimentGoal(id string) (string, error) {
	if id == "" {
		return "", nil
	}
	if gid, ok := r.expGoals[id]; ok {
		return gid, nil
	}
	e, err := r.store.ReadExperiment(id)
	if err != nil {
		if isNotFound(err) {
			r.expGoals[id] = ""
			return "", nil
		}
		return "", err
	}
	if e.Hypothesis != "" {
		gid, err := r.hypothesisGoal(e.Hypothesis)
		if err != nil {
			return "", err
		}
		r.expGoals[id] = gid
		return gid, nil
	}
	if err := r.ensureBaselineGoals(); err != nil {
		return "", err
	}
	gid := r.baselineGoals[id]
	r.expGoals[id] = gid
	return gid, nil
}

func (r *goalScopeResolver) conclusionGoal(id string) (string, error) {
	if id == "" {
		return "", nil
	}
	if gid, ok := r.conclusionMap[id]; ok {
		return gid, nil
	}
	c, err := r.store.ReadConclusion(id)
	if err != nil {
		if isNotFound(err) {
			r.conclusionMap[id] = ""
			return "", nil
		}
		return "", err
	}
	gid, err := r.hypothesisGoal(c.Hypothesis)
	if err != nil {
		return "", err
	}
	r.conclusionMap[id] = gid
	return gid, nil
}

func (r *goalScopeResolver) observationGoal(id string) (string, error) {
	if id == "" {
		return "", nil
	}
	if gid, ok := r.observationMap[id]; ok {
		return gid, nil
	}
	o, err := r.store.ReadObservation(id)
	if err != nil {
		if isNotFound(err) {
			r.observationMap[id] = ""
			return "", nil
		}
		return "", err
	}
	gid, err := r.experimentGoal(o.Experiment)
	if err != nil {
		return "", err
	}
	r.observationMap[id] = gid
	return gid, nil
}

func (r *goalScopeResolver) lessonMatches(l *entity.Lesson) (bool, error) {
	if l == nil || r.scope.All {
		return true, nil
	}
	if l.Scope == entity.LessonScopeSystem {
		return true, nil
	}
	for _, sub := range l.Subjects {
		var (
			gid string
			err error
		)
		switch {
		case strings.HasPrefix(sub, "H-"):
			gid, err = r.hypothesisGoal(sub)
		case strings.HasPrefix(sub, "E-"):
			gid, err = r.experimentGoal(sub)
		case strings.HasPrefix(sub, "C-"):
			gid, err = r.conclusionGoal(sub)
		default:
			continue
		}
		if err != nil {
			return false, err
		}
		if r.inScope(gid) {
			return true, nil
		}
	}
	return false, nil
}

func (r *goalScopeResolver) eventMatches(ev store.Event) (bool, error) {
	if r.scope.All {
		return true, nil
	}
	if ev.Subject == "" {
		return false, nil
	}
	switch {
	case strings.HasPrefix(ev.Subject, "G-"):
		return ev.Subject == r.scope.GoalID, nil
	case strings.HasPrefix(ev.Subject, "H-"):
		gid, err := r.hypothesisGoal(ev.Subject)
		return r.inScope(gid), err
	case strings.HasPrefix(ev.Subject, "E-"):
		gid, err := r.experimentGoal(ev.Subject)
		return r.inScope(gid), err
	case strings.HasPrefix(ev.Subject, "O-"):
		gid, err := r.observationGoal(ev.Subject)
		return r.inScope(gid), err
	case strings.HasPrefix(ev.Subject, "C-"):
		gid, err := r.conclusionGoal(ev.Subject)
		return r.inScope(gid), err
	case strings.HasPrefix(ev.Subject, "L-"):
		l, err := r.store.ReadLesson(ev.Subject)
		if err != nil {
			if isNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return r.lessonMatches(l)
	default:
		return false, nil
	}
}

func (r *goalScopeResolver) filterHypotheses(all []*entity.Hypothesis) []*entity.Hypothesis {
	if r.scope.All {
		return all
	}
	out := make([]*entity.Hypothesis, 0, len(all))
	for _, h := range all {
		if h != nil && r.inScope(h.GoalID) {
			out = append(out, h)
		}
	}
	return out
}

func (r *goalScopeResolver) filterExperiments(all []*entity.Experiment) ([]*entity.Experiment, error) {
	if r.scope.All {
		return all, nil
	}
	out := make([]*entity.Experiment, 0, len(all))
	for _, e := range all {
		gid, err := r.experimentGoal(e.ID)
		if err != nil {
			return nil, err
		}
		if r.inScope(gid) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (r *goalScopeResolver) filterConclusions(all []*entity.Conclusion) ([]*entity.Conclusion, error) {
	if r.scope.All {
		return all, nil
	}
	out := make([]*entity.Conclusion, 0, len(all))
	for _, c := range all {
		gid, err := r.hypothesisGoal(c.Hypothesis)
		if err != nil {
			return nil, err
		}
		if r.inScope(gid) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (r *goalScopeResolver) filterObservations(all []*entity.Observation) ([]*entity.Observation, error) {
	if r.scope.All {
		return all, nil
	}
	out := make([]*entity.Observation, 0, len(all))
	for _, o := range all {
		gid, err := r.experimentGoal(o.Experiment)
		if err != nil {
			return nil, err
		}
		if r.inScope(gid) {
			out = append(out, o)
		}
	}
	return out, nil
}

func (r *goalScopeResolver) filterLessons(all []*entity.Lesson) ([]*entity.Lesson, error) {
	if r.scope.All {
		return all, nil
	}
	out := make([]*entity.Lesson, 0, len(all))
	for _, l := range all {
		ok, err := r.lessonMatches(l)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, l)
		}
	}
	return out, nil
}

func (r *goalScopeResolver) filterEvents(all []store.Event) ([]store.Event, error) {
	if r.scope.All {
		return all, nil
	}
	out := make([]store.Event, 0, len(all))
	for _, ev := range all {
		ok, err := r.eventMatches(ev)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, ev)
		}
	}
	return out, nil
}
