package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

type cycleContextSnapshot struct {
	Project                string                           `json:"project"`
	ScopeGoalID            string                           `json:"scope_goal_id,omitempty"`
	ScopeAll               bool                             `json:"scope_all"`
	CapturedAt             time.Time                        `json:"captured_at"`
	Paused                 bool                             `json:"paused"`
	PauseReason            string                           `json:"pause_reason,omitempty"`
	Mode                   string                           `json:"mode"`
	MainCheckoutDirty      bool                             `json:"main_checkout_dirty"`
	MainCheckoutDirtyPaths []string                         `json:"main_checkout_dirty_paths"`
	Budgets                readmodel.BudgetSnapshot         `json:"budgets"`
	Counts                 map[string]int                   `json:"counts"`
	Instruments            map[string]store.Instrument      `json:"instruments"`
	ActiveScratch          []readmodel.ScratchWorkspaceView `json:"active_scratch"`
	StaleScratch           []readmodel.ScratchWorkspaceView `json:"stale_scratch,omitempty"`
	*cycleContextScope
	Goals []cycleContextGoal `json:"goals,omitempty"`
}

type cycleContextGoal struct {
	GoalID string         `json:"goal_id"`
	Counts map[string]int `json:"counts"`
	cycleContextScope
}

type cycleContextScope struct {
	Goal            *entity.Goal                   `json:"goal,omitempty"`
	FrontierBest    *frontierRow                   `json:"frontier_best"`
	FrontierStallK  int                            `json:"frontier_stall_k"`
	StalledFor      int                            `json:"stalled_for"`
	StallReached    bool                           `json:"stall_reached"`
	GoalAssessment  *frontierGoalAssessment        `json:"goal_assessment,omitempty"`
	OpenHypotheses  []*entity.Hypothesis           `json:"open_hypotheses"`
	InFlight        []dashboardInFlight            `json:"in_flight"`
	ActiveLessons   []readmodel.LessonSummaryView  `json:"active_lessons"`
	RelevantLessons []readmodel.RelevantLessonView `json:"relevant_lessons"`
}

type cycleContextInputs struct {
	cfg          *store.Config
	state        *store.State
	now          time.Time
	mainCheckout mainCheckoutState
	instruments  map[string]store.Instrument
	hypotheses   []*entity.Hypothesis
	experiments  []*entity.Experiment
	conclusions  []*entity.Conclusion
	observations []*entity.Observation
	lessons      []*entity.Lesson
	scratch      []*entity.Scratch
	events       []store.Event
}

func cycleContextCommands() []*cobra.Command {
	var goalFlag string
	c := &cobra.Command{
		Use:   "cycle-context",
		Short: "One-shot read-only context snapshot for research agents",
		Long: `Emit the boot-time world snapshot an orchestrator needs before
starting a research cycle: pause state, goal, frontier best, open
hypotheses, active lesson summaries, instruments, in-flight work, and
budget/count status.

The command is read-only. It never mutates .research/, emits no events,
and is not a steering surface. Use --json as the agent contract; text
mode is only a compact human summary.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			scope, err := resolveGoalScope(s, goalFlag)
			if err != nil {
				return err
			}
			snap, err := captureCycleContext(s, scope)
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(snap)
			}
			renderCycleContextText(w, snap)
			return nil
		},
	}
	c.Flags().StringVar(&goalFlag, "goal", "", "goal to scope the context to (defaults to active goal; use 'all' for every goal)")
	return []*cobra.Command{c}
}

func captureCycleContext(s *store.Store, scope goalScope) (*cycleContextSnapshot, error) {
	inputs, err := loadCycleContextInputs(s)
	if err != nil {
		return nil, err
	}

	snap := &cycleContextSnapshot{
		Project:                s.Root(),
		ScopeGoalID:            scope.GoalID,
		ScopeAll:               scope.All,
		CapturedAt:             inputs.now,
		Paused:                 inputs.state.Paused,
		PauseReason:            inputs.state.PauseReason,
		Mode:                   inputs.cfg.Mode,
		MainCheckoutDirty:      inputs.mainCheckout.Dirty,
		MainCheckoutDirtyPaths: nonNilStrings(inputs.mainCheckout.Paths),
		Budgets:                readmodel.BuildBudgetSnapshot(inputs.cfg, inputs.state, inputs.now),
		Instruments:            inputs.instruments,
		Counts:                 readmodel.BuildCountsWithLessons(len(inputs.hypotheses), len(inputs.experiments), len(inputs.observations), len(inputs.conclusions), len(inputs.lessons)),
		ActiveScratch:          readmodel.ActiveScratchWorkspaces(inputs.scratch, inputs.now),
	}
	if inputs.cfg.Budgets.StaleExperimentMinutes > 0 {
		snap.StaleScratch = readmodel.StaleScratchWorkspaces(
			inputs.scratch,
			time.Duration(inputs.cfg.Budgets.StaleExperimentMinutes)*time.Minute,
			inputs.now,
		)
	}

	if scope.All {
		goals, err := s.ListGoals()
		if err != nil {
			return nil, err
		}
		snap.Goals = make([]cycleContextGoal, 0, len(goals))
		for _, goal := range goals {
			payload, counts, err := buildCycleContextScope(s, inputs, goalScope{GoalID: goal.ID}, goal)
			if err != nil {
				return nil, err
			}
			snap.Goals = append(snap.Goals, cycleContextGoal{
				GoalID:            goal.ID,
				Counts:            counts,
				cycleContextScope: *payload,
			})
		}
		return snap, nil
	}

	var goal *entity.Goal
	if scope.GoalID != "" {
		goal, err = s.ReadGoal(scope.GoalID)
		if err != nil {
			return nil, err
		}
	}
	payload, counts, err := buildCycleContextScope(s, inputs, scope, goal)
	if err != nil {
		return nil, err
	}
	snap.Counts = counts
	snap.cycleContextScope = payload
	return snap, nil
}

func loadCycleContextInputs(s *store.Store) (*cycleContextInputs, error) {
	cfg, err := s.Config()
	if err != nil {
		return nil, err
	}
	st, err := s.State()
	if err != nil {
		return nil, err
	}
	mainCheckout, err := captureMainCheckoutState(s.Root())
	if err != nil {
		return nil, err
	}
	instruments, err := s.ListInstruments()
	if err != nil {
		return nil, err
	}
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
	scratch, err := s.ListScratch()
	if err != nil {
		return nil, err
	}
	events, err := s.Events(0)
	if err != nil {
		return nil, err
	}
	return &cycleContextInputs{
		cfg:          cfg,
		state:        st,
		now:          time.Now().UTC(),
		mainCheckout: mainCheckout,
		instruments:  instruments,
		hypotheses:   hyps,
		experiments:  exps,
		conclusions:  concls,
		observations: obs,
		lessons:      lessons,
		scratch:      scratch,
		events:       events,
	}, nil
}

func buildCycleContextScope(s *store.Store, inputs *cycleContextInputs, scope goalScope, goal *entity.Goal) (*cycleContextScope, map[string]int, error) {
	resolver := newGoalScopeResolver(s, scope)

	hyps := resolver.filterHypotheses(inputs.hypotheses)
	exps, err := resolver.filterExperiments(inputs.experiments)
	if err != nil {
		return nil, nil, err
	}
	concls, err := resolver.filterConclusions(inputs.conclusions)
	if err != nil {
		return nil, nil, err
	}
	obs, err := resolver.filterObservations(inputs.observations)
	if err != nil {
		return nil, nil, err
	}
	lessons, err := resolver.filterLessons(inputs.lessons)
	if err != nil {
		return nil, nil, err
	}
	events, err := resolver.filterEvents(inputs.events)
	if err != nil {
		return nil, nil, err
	}

	expClassByID := readmodel.ClassifyExperimentsForReadFromHypotheses(exps, hyps)
	inFlight, _ := readmodel.BuildExperimentActivity(exps, expClassByID, events, 0, inputs.now)
	obsIdx := readmodel.NewObservationIndex(obs)
	inFlight = addCycleContextBaselineRecommendations(s, inFlight, exps, hyps, obsIdx)

	activeLessonViews, err := readmodel.ListLessonsForRead(s, lessons, readmodel.LessonListOptions{Status: entity.LessonStatusActive})
	if err != nil {
		return nil, nil, err
	}
	allLessonViews, err := readmodel.ListLessonsForRead(s, lessons, readmodel.LessonListOptions{Status: readmodel.LessonStatusAll})
	if err != nil {
		return nil, nil, err
	}

	var frontierBest *frontierRow
	var stalledFor int

	payload := &cycleContextScope{
		Goal:           goal,
		FrontierStallK: inputs.cfg.Budgets.FrontierStallK,
		OpenHypotheses: openHypotheses(hyps),
		InFlight:       nonNilInFlight(inFlight),
		ActiveLessons:  nonNilLessonSummaries(readmodel.BuildLessonSummaryViews(activeLessonViews)),
	}

	if goal != nil {
		frontier := readmodel.BuildFrontierSnapshot(goal, concls, obsIdx, expClassByID)
		if len(frontier.Rows) > 0 {
			best := frontier.Rows[0]
			frontierBest = &best
			payload.FrontierBest = frontierBest
		}
		stalledFor = frontier.StalledFor
		payload.StalledFor = stalledFor
		payload.StallReached = inputs.cfg.Budgets.FrontierStallK > 0 && stalledFor >= inputs.cfg.Budgets.FrontierStallK
		frontierAssessment := frontier.Assessment
		payload.GoalAssessment = &frontierAssessment
	}
	payload.RelevantLessons = nonNilRelevantLessons(readmodel.RankRelevantLessons(allLessonViews, readmodel.LessonRelevanceContext{
		Goal:                goal,
		OpenHypotheses:      payload.OpenHypotheses,
		InFlightExperiments: relevantInFlightExperiments(exps),
		FrontierBest:        frontierBest,
		Conclusions:         concls,
		Hypotheses:          hyps,
		Limit:               readmodel.DefaultRelevantLessonLimit,
	}))

	counts := readmodel.BuildCountsWithLessons(len(hyps), len(exps), len(obs), len(concls), len(lessons))
	return payload, counts, nil
}

func addCycleContextBaselineRecommendations(
	s *store.Store,
	inFlight []dashboardInFlight,
	exps []*entity.Experiment,
	hyps []*entity.Hypothesis,
	obsIdx *readmodel.ObservationIndex,
) []dashboardInFlight {
	expByID := make(map[string]*entity.Experiment, len(exps))
	for _, exp := range exps {
		if exp != nil {
			expByID[exp.ID] = exp
		}
	}
	hypByID := make(map[string]*entity.Hypothesis, len(hyps))
	for _, hyp := range hyps {
		if hyp != nil {
			hypByID[hyp.ID] = hyp
		}
	}
	out := make([]dashboardInFlight, len(inFlight))
	copy(out, inFlight)
	for i := range out {
		exp := expByID[out[i].ID]
		if exp == nil {
			continue
		}
		hyp := hypByID[exp.Hypothesis]
		if hyp == nil {
			continue
		}
		instrument := analyzeBaselineInstrument("", hyp.Predicts.Instrument)
		res, err := readmodel.ResolveInferredBaselineWithIndex(s, obsIdx, hyp, exp, instrument)
		if err != nil {
			res = &readmodel.BaselineResolution{Note: err.Error()}
		}
		out[i].RecommendedBaseline = res
	}
	return out
}

func openHypotheses(hyps []*entity.Hypothesis) []*entity.Hypothesis {
	out := make([]*entity.Hypothesis, 0)
	for _, h := range hyps {
		if h != nil && h.Status == entity.StatusOpen {
			out = append(out, h)
		}
	}
	return out
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func nonNilInFlight(in []dashboardInFlight) []dashboardInFlight {
	if in == nil {
		return []dashboardInFlight{}
	}
	return in
}

func nonNilLessonSummaries(in []readmodel.LessonSummaryView) []readmodel.LessonSummaryView {
	if in == nil {
		return []readmodel.LessonSummaryView{}
	}
	return in
}

func nonNilRelevantLessons(in []readmodel.RelevantLessonView) []readmodel.RelevantLessonView {
	if in == nil {
		return []readmodel.RelevantLessonView{}
	}
	return in
}

func renderCycleContextText(w *output.Writer, snap *cycleContextSnapshot) {
	w.Textf("project:        %s\n", snap.Project)
	w.Textf("scope:          %s\n", goalScope{GoalID: snap.ScopeGoalID, All: snap.ScopeAll}.label())
	w.Textf("mode:           %s\n", snap.Mode)
	if snap.Paused {
		w.Textf("state:          PAUSED — %s\n", snap.PauseReason)
	} else {
		w.Textln("state:          active")
	}
	if snap.MainCheckoutDirty {
		w.Textf("main checkout:  DIRTY (%d path(s))\n", len(snap.MainCheckoutDirtyPaths))
	} else {
		w.Textln("main checkout:  clean")
	}
	w.Textf("instruments:    %d\n", len(snap.Instruments))
	w.Textf("active scratch: %d\n", len(snap.ActiveScratch))
	if snap.ScopeAll {
		w.Textf("goals:          %d\n", len(snap.Goals))
		for _, goal := range snap.Goals {
			renderCycleContextGoalText(w, goal.GoalID, &goal.cycleContextScope)
		}
		return
	}
	renderCycleContextGoalText(w, snap.ScopeGoalID, snap.cycleContextScope)
}

func renderCycleContextGoalText(w *output.Writer, goalID string, scope *cycleContextScope) {
	if scope == nil {
		return
	}
	w.Textln("")
	if scope.Goal != nil {
		w.Textf("goal:           %s (%s)\n", goalID, formatGoalObjective(scope.Goal))
	} else {
		w.Textln("goal:           (none)")
	}
	if scope.FrontierBest != nil {
		w.Textf("frontier_best:  %s candidate=%s delta_frac=%.6g\n", scope.FrontierBest.Conclusion, scope.FrontierBest.Candidate, scope.FrontierBest.DeltaFrac)
	} else {
		w.Textln("frontier_best:  (none)")
	}
	w.Textf("stalled_for:    %d", scope.StalledFor)
	if scope.FrontierStallK > 0 {
		w.Textf("/%d", scope.FrontierStallK)
	}
	if scope.StallReached {
		w.Textf(" (stall reached)")
	}
	w.Textln("")
	w.Textf("open hypotheses: %d\n", len(scope.OpenHypotheses))
	w.Textf("in flight:      %d\n", len(scope.InFlight))
	w.Textf("active lessons: %d\n", len(scope.ActiveLessons))
	w.Textf("relevant lessons: %d\n", len(scope.RelevantLessons))
}
