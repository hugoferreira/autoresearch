package cli

import (
	"math"
	"sort"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func frontierCommands() []*cobra.Command {
	var goalFlag string
	c := &cobra.Command{
		Use:   "frontier",
		Short: "Show the Pareto frontier of conclusions on the goal's objective",
		Long: `List supported, feasible conclusions in order of the objective direction
(best first), alongside a "stalled_for" counter: the number of conclusions
written since the last one that actually improved the frontier.

Defaults to the active goal; pass --goal G-NNNN to view a historical
goal's frontier, or --goal all to render one frontier section per goal.
Only conclusions whose hypothesis was bound to the scoped goal are
considered.

A conclusion is "feasible" if every constraint with op=require is satisfied
by at least one matching observation in its candidate experiment. For v1
we only filter on require-constraints; max/min are reported but not yet
used to disqualify.

` + "`goal_assessment`" + ` is stricter than the row filter: it only reports
threshold satisfaction when a reviewed, supported conclusion reaches the
goal threshold and its candidate satisfies every goal constraint.

Orchestrators read ` + "`frontier --json`" + `'s ` + "`stalled_for`" + ` field against the
configured ` + "`budgets.frontier_stall_k`" + ` to decide when to stop the loop.`,
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
			frontiers, err := collectFrontiers(s, scope)
			if err != nil {
				return err
			}
			cfg, err := s.Config()
			if err != nil {
				return err
			}

			if scope.All {
				if w.IsJSON() {
					items := make([]map[string]any, 0, len(frontiers))
					for _, f := range frontiers {
						items = append(items, map[string]any{
							"goal_id":          f.Goal.ID,
							"objective":        f.Goal.Objective,
							"frontier":         f.Rows,
							"goal_assessment":  f.Assessment,
							"stalled_for":      f.StalledFor,
							"frontier_stall_k": cfg.Budgets.FrontierStallK,
							"stall_reached":    cfg.Budgets.FrontierStallK > 0 && f.StalledFor >= cfg.Budgets.FrontierStallK,
						})
					}
					return w.JSON(mergeGoalScopePayload(map[string]any{"frontiers": items}, scope))
				}
				w.Textln("[goal: all]")
				if len(frontiers) == 0 {
					w.Textln("(no goals)")
					return nil
				}
				for i, f := range frontiers {
					if i > 0 {
						w.Textln("")
					}
					renderFrontierSection(w, f, cfg.Budgets.FrontierStallK)
				}
				return nil
			}

			if len(frontiers) == 0 {
				return nil
			}
			f := frontiers[0]
			if w.IsJSON() {
				return w.JSON(mergeGoalScopePayload(map[string]any{
					"goal_id": f.Goal.ID,
					"objective": map[string]any{
						"instrument": f.Goal.Objective.Instrument,
						"direction":  f.Goal.Objective.Direction,
					},
					"frontier":         f.Rows,
					"goal_assessment":  f.Assessment,
					"stalled_for":      f.StalledFor,
					"frontier_stall_k": cfg.Budgets.FrontierStallK,
					"stall_reached":    cfg.Budgets.FrontierStallK > 0 && f.StalledFor >= cfg.Budgets.FrontierStallK,
				}, scope))
			}
			renderFrontierSection(w, f, cfg.Budgets.FrontierStallK)
			return nil
		},
	}
	c.Flags().StringVar(&goalFlag, "goal", "", "goal to scope the frontier to (defaults to active goal; use 'all' for every goal)")
	return []*cobra.Command{c}
}

// scopeConclusionsToGoal drops conclusions whose hypothesis is not bound to
// goalID. When goalID is empty the list is returned unchanged — the caller
// has already signalled "no goal scoping".
func scopeConclusionsToGoal(s *store.Store, concls []*entity.Conclusion, goalID string) ([]*entity.Conclusion, error) {
	if goalID == "" {
		return concls, nil
	}
	return newGoalScopeResolver(s, goalScope{GoalID: goalID}).filterConclusions(concls)
}

type goalFrontier struct {
	Goal       *entity.Goal
	Rows       []frontierRow
	Assessment frontierGoalAssessment
	StalledFor int
}

func collectFrontiers(s *store.Store, scope goalScope) ([]goalFrontier, error) {
	allConcls, err := s.ListConclusions()
	if err != nil {
		return nil, err
	}
	obsByExp := loadObservationsByExperiment(s)
	expClassByID, err := classifyAllExperimentsForRead(s)
	if err != nil {
		return nil, err
	}
	if scope.All {
		goals, err := s.ListGoals()
		if err != nil {
			return nil, err
		}
		out := make([]goalFrontier, 0, len(goals))
		for _, goal := range goals {
			concls, err := scopeConclusionsToGoal(s, allConcls, goal.ID)
			if err != nil {
				return nil, err
			}
			rows, stalled := computeFrontierFromObservations(goal, concls, obsByExp, expClassByID)
			out = append(out, goalFrontier{
				Goal:       goal,
				Rows:       rows,
				Assessment: assessGoalCompletion(goal, concls, obsByExp),
				StalledFor: stalled,
			})
		}
		return out, nil
	}
	goal, err := s.ReadGoal(scope.GoalID)
	if err != nil {
		return nil, err
	}
	concls, err := scopeConclusionsToGoal(s, allConcls, goal.ID)
	if err != nil {
		return nil, err
	}
	rows, stalled := computeFrontierFromObservations(goal, concls, obsByExp, expClassByID)
	return []goalFrontier{{
		Goal:       goal,
		Rows:       rows,
		Assessment: assessGoalCompletion(goal, concls, obsByExp),
		StalledFor: stalled,
	}}, nil
}

func renderFrontierSection(w *output.Writer, f goalFrontier, stallK int) {
	if f.Goal == nil {
		return
	}
	w.Textf("[goal: %s, objective: %s, %d supported conclusions]\n", f.Goal.ID, formatGoalObjective(f.Goal), len(f.Rows))
	w.Textln("")
	if len(f.Rows) == 0 {
		w.Textln("(no supported+feasible conclusions yet)")
		w.Textln("")
	} else {
		for i, r := range f.Rows {
			marker := "  "
			if i == 0 {
				marker = "* "
			}
			w.Textf("%s%s  %s  %s=%.6g", marker, r.Conclusion, r.Hypothesis, f.Goal.Objective.Instrument, r.Value)
			if r.RescuedBy != "" {
				w.Textf("  [rescued by %s]", r.RescuedBy)
			}
			if r.Classification == experimentClassificationDead {
				w.Textf("  %s", experimentClassificationMarker(r.Classification))
			}
			w.Textln("")
		}
		w.Textln("")
	}
	switch f.Assessment.Mode {
	case "threshold":
		w.Textf("goal_assessment: threshold=%g -> %s", f.Assessment.Threshold, f.Assessment.OnThreshold)
		if f.Assessment.Met {
			w.Textf(" (met by %s; recommended=%s)\n", f.Assessment.MetByConclusion, f.Assessment.RecommendedAction)
		} else {
			w.Textf(" (not yet met; recommended=%s)\n", f.Assessment.RecommendedAction)
		}
	default:
		w.Textln("goal_assessment: open-ended -> continue_until_stall (recommended=continue)")
	}
	w.Textf("stalled_for: %d", f.StalledFor)
	if stallK > 0 {
		w.Textf(" (stall-k=%d)", stallK)
		if f.StalledFor >= stallK {
			w.Textln(" — STALL REACHED, orchestrator should stop")
			return
		}
	}
	w.Textln("")
}

type frontierRow struct {
	Conclusion string  `json:"conclusion"`
	Hypothesis string  `json:"hypothesis"`
	Candidate  string  `json:"candidate_experiment"`
	Value      float64 `json:"value"`
	DeltaFrac  float64 `json:"delta_frac"`
	// Classification is a read-time label for the candidate experiment.
	// "dead" means the experiment's parent hypothesis is already terminal,
	// so the row is still historically valid but no longer actionable work.
	Classification string `json:"classification"`
	// HypothesisStatus records the terminal hypothesis status that caused
	// Classification=dead. Kept separate from Hypothesis, which is the ID.
	HypothesisStatus string `json:"hypothesis_status,omitempty"`
	// RescuedBy is non-empty when the backing conclusion was supported via
	// a goal rescuer rather than a clean primary win. Renderers display a
	// "rescued by <instrument>" annotation so the reader cannot mistake a
	// rescued row for a clean primary-metric improvement.
	RescuedBy string `json:"rescued_by,omitempty"`
	// TiebreakValues holds one candidate value per goal.Rescuers clause, in
	// declared order. Used when two rows are within goal.NeutralBandFrac on
	// primary — the sort then picks the row that wins on the first rescuer
	// whose value differs. Absent rescuer data is represented as NaN so the
	// tiebreak skips cleanly over it.
	TiebreakValues []float64 `json:"-"`
}

type frontierGoalAssessment struct {
	Mode              string  `json:"mode"`
	Threshold         float64 `json:"threshold,omitempty"`
	OnThreshold       string  `json:"on_threshold,omitempty"`
	Met               bool    `json:"met"`
	MetByConclusion   string  `json:"met_by_conclusion,omitempty"`
	RecommendedAction string  `json:"recommended_action"`
}

type frontierCandidate struct {
	Conclusion *entity.Conclusion
	Value      float64
}

// computeFrontier returns rows in best-first order for the objective and the
// count of conclusions written since the last frontier improvement.
func computeFrontier(s *store.Store, goal *entity.Goal, concls []*entity.Conclusion) (rows []frontierRow, stalledFor int) {
	return computeFrontierFromObservations(goal, concls, loadObservationsByExperiment(s), loadExperimentReadClasses(s))
}

func computeFrontierFromObservations(goal *entity.Goal, concls []*entity.Conclusion, obsByExp map[string][]*entity.Observation, expClassByID map[string]experimentReadClass) (rows []frontierRow, stalledFor int) {
	rows = []frontierRow{} // always non-nil so --json emits [] not null
	requireByInst := frontierRequireConstraints(goal)

	for _, c := range concls {
		val, ok := frontierCandidateValue(goal, c, obsByExp, requireByInst)
		if !ok {
			continue
		}
		class := expClassByID[c.CandidateExp]
		if class.Classification == "" {
			class.Classification = experimentClassificationLive
		}
		rows = append(rows, frontierRow{
			Conclusion:       c.ID,
			Hypothesis:       c.Hypothesis,
			Candidate:        c.CandidateExp,
			Value:            val,
			DeltaFrac:        c.Effect.DeltaFrac,
			Classification:   class.Classification,
			HypothesisStatus: class.HypothesisStatus,
			RescuedBy:        c.Strict.RescuedBy,
			TiebreakValues:   rescuerTiebreakValues(goal, obsByExp[c.CandidateExp]),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return frontierRowBetter(goal, rows[i], rows[j])
	})

	sortedByTime := make([]*entity.Conclusion, len(concls))
	copy(sortedByTime, concls)
	sort.Slice(sortedByTime, func(i, j int) bool { return sortedByTime[i].CreatedAt.Before(sortedByTime[j].CreatedAt) })

	hasBest := false
	var bestRow frontierRow
	for _, c := range sortedByTime {
		val, ok := frontierCandidateValue(goal, c, obsByExp, requireByInst)
		if !ok {
			if hasBest {
				stalledFor++
			}
			continue
		}
		cur := frontierRow{
			Value:          val,
			TiebreakValues: rescuerTiebreakValues(goal, obsByExp[c.CandidateExp]),
		}
		if !hasBest {
			hasBest = true
			bestRow = cur
			stalledFor = 0
			continue
		}
		if frontierRowBetter(goal, cur, bestRow) {
			bestRow = cur
			stalledFor = 0
		} else {
			stalledFor++
		}
	}
	return rows, stalledFor
}

// rescuerTiebreakValues extracts one candidate value per goal rescuer, in
// declared order. Missing observations (the candidate wasn't measured on a
// rescuer instrument) are represented as NaN so the tiebreak comparator can
// skip them cleanly.
func rescuerTiebreakValues(goal *entity.Goal, obs []*entity.Observation) []float64 {
	if goal == nil || len(goal.Rescuers) == 0 {
		return nil
	}
	out := make([]float64, len(goal.Rescuers))
	for i, r := range goal.Rescuers {
		v, ok := candidateObjectiveValue(obs, r.Instrument)
		if !ok {
			out[i] = math.NaN()
			continue
		}
		out[i] = v
	}
	return out
}

// frontierRowBetter reports whether a is strictly better than b on the goal.
// When the primary values are within goal.NeutralBandFrac of each other, the
// comparison falls through to rescuers in goal-declared order; the first
// rescuer whose value differs decides. This is a limited Pareto-tiebreak,
// not a full multi-objective frontier.
func frontierRowBetter(goal *entity.Goal, a, b frontierRow) bool {
	if goal == nil {
		return false
	}
	if !withinPrimaryNeutralBand(goal, a.Value, b.Value) {
		return betterFrontierValue(goal.Objective.Direction, a.Value, b.Value)
	}
	for i, r := range goal.Rescuers {
		if i >= len(a.TiebreakValues) || i >= len(b.TiebreakValues) {
			continue
		}
		av, bv := a.TiebreakValues[i], b.TiebreakValues[i]
		if math.IsNaN(av) || math.IsNaN(bv) {
			continue
		}
		if av == bv {
			continue
		}
		return betterFrontierValue(r.Direction, av, bv)
	}
	return false
}

// withinPrimaryNeutralBand reports whether two primary values are close
// enough (relative to the larger magnitude) that the goal considers them
// "tied" on the primary metric. Returns false whenever the goal doesn't
// declare a neutral band, preserving v3-goal behaviour exactly.
func withinPrimaryNeutralBand(goal *entity.Goal, a, b float64) bool {
	if goal == nil || goal.NeutralBandFrac <= 0 {
		return false
	}
	denom := math.Max(math.Max(math.Abs(a), math.Abs(b)), 1e-9)
	return math.Abs(a-b)/denom <= goal.NeutralBandFrac
}

func frontierRequireConstraints(goal *entity.Goal) map[string]string {
	requireByInst := map[string]string{}
	if goal == nil {
		return requireByInst
	}
	for _, c := range goal.Constraints {
		if c.Require != "" {
			requireByInst[c.Instrument] = c.Require
		}
	}
	return requireByInst
}

func frontierCandidateValue(goal *entity.Goal, c *entity.Conclusion, obsByExp map[string][]*entity.Observation, requireByInst map[string]string) (float64, bool) {
	if goal == nil || c == nil {
		return 0, false
	}
	if c.Verdict != entity.VerdictSupported || c.Effect.Instrument != goal.Objective.Instrument {
		return 0, false
	}
	if !requireSatisfied(obsByExp[c.CandidateExp], requireByInst) {
		return 0, false
	}
	return candidateObjectiveValue(obsByExp[c.CandidateExp], goal.Objective.Instrument)
}

func betterFrontierValue(direction string, candidate, incumbent float64) bool {
	if direction == "decrease" {
		return candidate < incumbent
	}
	return candidate > incumbent
}

func assessGoalCompletion(goal *entity.Goal, concls []*entity.Conclusion, obsByExp map[string][]*entity.Observation) frontierGoalAssessment {
	if goal == nil || goal.IsOpenEnded() {
		return frontierGoalAssessment{
			Mode:              "open_ended",
			Met:               false,
			RecommendedAction: "continue",
		}
	}

	assessment := frontierGoalAssessment{
		Mode:              "threshold",
		Threshold:         goal.Completion.Threshold,
		OnThreshold:       goal.EffectiveOnThreshold(),
		Met:               false,
		RecommendedAction: "continue",
	}

	var bestReviewed frontierCandidate
	hasReviewed := false
	for _, c := range concls {
		if c == nil || c.ReviewedBy == "" {
			continue
		}
		if c.Verdict != entity.VerdictSupported || c.Effect.Instrument != goal.Objective.Instrument {
			continue
		}
		obs := obsByExp[c.CandidateExp]
		if !goalConstraintsSatisfied(obs, goal.Constraints) {
			continue
		}
		val, ok := candidateObjectiveValue(obs, goal.Objective.Instrument)
		if !ok {
			continue
		}
		if !hasReviewed || betterFrontierValue(goal.Objective.Direction, val, bestReviewed.Value) {
			bestReviewed = frontierCandidate{Conclusion: c, Value: val}
			hasReviewed = true
		}
	}
	if !hasReviewed {
		return assessment
	}
	if meetsGoalThreshold(goal.Objective.Direction, bestReviewed.Conclusion.Effect.DeltaFrac, goal.Completion.Threshold) {
		assessment.Met = true
		assessment.MetByConclusion = bestReviewed.Conclusion.ID
		switch assessment.OnThreshold {
		case entity.GoalOnThresholdAskHuman:
			assessment.RecommendedAction = "ask_human"
		case entity.GoalOnThresholdStop:
			assessment.RecommendedAction = "stop"
		default:
			assessment.RecommendedAction = "continue"
		}
	}
	return assessment
}

func loadExperimentReadClasses(s *store.Store) map[string]experimentReadClass {
	classByID, err := classifyAllExperimentsForRead(s)
	if err != nil {
		return nil
	}
	return classByID
}

func meetsGoalThreshold(direction string, deltaFrac, threshold float64) bool {
	if threshold <= 0 {
		return false
	}
	if direction == "decrease" {
		return deltaFrac <= -threshold
	}
	return deltaFrac >= threshold
}

// loadObservationsByExperiment reads all observations once and returns them
// indexed by experiment ID. A nil map (on read error) is safe — callers get
// empty slices from the nil map.
func loadObservationsByExperiment(s *store.Store) map[string][]*entity.Observation {
	all, err := s.ListObservations()
	if err != nil {
		return nil
	}
	m := make(map[string][]*entity.Observation, len(all))
	for _, o := range all {
		m[o.Experiment] = append(m[o.Experiment], o)
	}
	return m
}

func requireSatisfied(obs []*entity.Observation, req map[string]string) bool {
	if len(req) == 0 {
		return true
	}
	for inst, wanted := range req {
		ok := false
		for _, o := range obs {
			if o.Instrument != inst {
				continue
			}
			switch wanted {
			case "pass":
				if o.Pass != nil && *o.Pass {
					ok = true
				}
			default:
				// For v1, any non-"pass" require is treated as pass-through;
				// we document that require-clauses other than "pass" are
				// not evaluated yet.
				ok = true
			}
			if ok {
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func goalConstraintsSatisfied(obs []*entity.Observation, constraints []entity.Constraint) bool {
	for _, c := range constraints {
		if !goalConstraintSatisfied(obs, c) {
			return false
		}
	}
	return true
}

func goalConstraintSatisfied(obs []*entity.Observation, c entity.Constraint) bool {
	for _, o := range obs {
		if o.Instrument != c.Instrument {
			continue
		}
		switch {
		case c.Max != nil:
			if o.Value <= *c.Max {
				return true
			}
		case c.Min != nil:
			if o.Value >= *c.Min {
				return true
			}
		case c.Require != "":
			switch c.Require {
			case "pass":
				if o.Pass != nil && *o.Pass {
					return true
				}
			default:
				return true
			}
		}
	}
	return false
}

func candidateObjectiveValue(obs []*entity.Observation, instrument string) (float64, bool) {
	for _, o := range obs {
		if o.Instrument == instrument {
			return o.Value, true
		}
	}
	return 0, false
}
