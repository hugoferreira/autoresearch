package cli

import (
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
)

func captureRelevantLessons(s *store.Store, goalFlag, hypID string, limit int) ([]readmodel.RelevantLessonView, goalScope, error) {
	hyp, scope, err := resolveLessonRelevantScope(s, goalFlag, hypID)
	if err != nil {
		return nil, goalScope{}, err
	}
	inputs, err := loadLessonRelevanceInputs(s, scope)
	if err != nil {
		return nil, goalScope{}, err
	}
	var goal *entity.Goal
	if scope.GoalID != "" {
		goal, err = s.ReadGoal(scope.GoalID)
		if err != nil {
			return nil, goalScope{}, err
		}
	}

	views, err := readmodel.ListLessonsForRead(s, inputs.lessons, readmodel.LessonListOptions{Status: readmodel.LessonStatusAll})
	if err != nil {
		return nil, goalScope{}, err
	}
	var frontierBest *readmodel.FrontierRow
	if goal != nil {
		expClassByID := readmodel.ClassifyExperimentsForReadFromHypotheses(inputs.experiments, inputs.hypotheses)
		frontier := readmodel.BuildFrontierSnapshot(goal, inputs.conclusions, readmodel.NewObservationIndex(inputs.observations), expClassByID)
		if len(frontier.Rows) > 0 {
			best := frontier.Rows[0]
			frontierBest = &best
		}
	}

	rows := readmodel.RankRelevantLessons(views, readmodel.LessonRelevanceContext{
		Goal:                goal,
		Hypothesis:          hyp,
		OpenHypotheses:      openHypotheses(inputs.hypotheses),
		InFlightExperiments: relevantInFlightExperiments(inputs.experiments),
		FrontierBest:        frontierBest,
		Conclusions:         inputs.conclusions,
		Hypotheses:          inputs.hypotheses,
		Limit:               limit,
	})
	return rows, scope, nil
}

type lessonRelevanceInputs struct {
	hypotheses   []*entity.Hypothesis
	experiments  []*entity.Experiment
	conclusions  []*entity.Conclusion
	observations []*entity.Observation
	lessons      []*entity.Lesson
}

func loadLessonRelevanceInputs(s *store.Store, scope goalScope) (*lessonRelevanceInputs, error) {
	hyps, err := s.ListHypotheses()
	if err != nil {
		return nil, err
	}
	exps, err := s.ListExperiments()
	if err != nil {
		return nil, err
	}
	concls, err := s.ListConclusions()
	if err != nil {
		return nil, err
	}
	obs, err := s.ListObservations()
	if err != nil {
		return nil, err
	}
	lessons, err := s.ListLessons()
	if err != nil {
		return nil, err
	}
	resolver := newGoalScopeResolver(s, scope)
	hyps = resolver.filterHypotheses(hyps)
	exps, err = resolver.filterExperiments(exps)
	if err != nil {
		return nil, err
	}
	concls, err = resolver.filterConclusions(concls)
	if err != nil {
		return nil, err
	}
	obs, err = resolver.filterObservations(obs)
	if err != nil {
		return nil, err
	}
	lessons, err = resolver.filterLessons(lessons)
	if err != nil {
		return nil, err
	}
	return &lessonRelevanceInputs{
		hypotheses:   hyps,
		experiments:  exps,
		conclusions:  concls,
		observations: obs,
		lessons:      lessons,
	}, nil
}

func resolveLessonRelevantScope(s *store.Store, goalFlag, hypID string) (*entity.Hypothesis, goalScope, error) {
	hypID = strings.TrimSpace(hypID)
	if hypID == "" {
		scope, err := resolveGoalScope(s, goalFlag)
		return nil, scope, err
	}
	hyp, err := s.ReadHypothesis(hypID)
	if err != nil {
		return nil, goalScope{}, err
	}
	if strings.TrimSpace(goalFlag) == "" {
		if strings.TrimSpace(hyp.GoalID) == "" {
			return hyp, goalScope{All: true}, nil
		}
		return hyp, goalScope{GoalID: hyp.GoalID}, nil
	}
	scope, err := resolveGoalScope(s, goalFlag)
	if err != nil {
		return nil, goalScope{}, err
	}
	if scope.All {
		return nil, goalScope{}, fmt.Errorf("--goal all cannot be combined with --hypothesis %s", hypID)
	}
	if hyp.GoalID != "" && scope.GoalID != hyp.GoalID {
		return nil, goalScope{}, fmt.Errorf("--hypothesis %s belongs to goal %s, not %s", hypID, hyp.GoalID, scope.GoalID)
	}
	return hyp, scope, nil
}

func resolvedRelevantLessonLimit(limit int) int {
	if limit <= 0 {
		return readmodel.DefaultRelevantLessonLimit
	}
	return limit
}

func relevantInFlightExperiments(exps []*entity.Experiment) []*entity.Experiment {
	out := make([]*entity.Experiment, 0, len(exps))
	for _, exp := range exps {
		if exp == nil || exp.IsBaseline {
			continue
		}
		switch exp.Status {
		case entity.ExpDesigned, entity.ExpImplemented, entity.ExpMeasured:
			out = append(out, exp)
		}
	}
	return out
}
