package cli

import (
	"sort"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func frontierCommands() []*cobra.Command {
	var goalID string
	c := &cobra.Command{
		Use:   "frontier",
		Short: "Show the Pareto frontier of conclusions on the goal's objective",
		Long: `List supported, feasible conclusions in order of the objective direction
(best first), alongside a "stalled_for" counter: the number of conclusions
written since the last one that actually improved the frontier.

Defaults to the active goal; pass --goal G-NNNN to view a historical
goal's frontier. Only conclusions whose hypothesis was bound to the
scoped goal are considered.

A conclusion is "feasible" if every constraint with op=require is satisfied
by at least one matching observation in its candidate experiment. For v1
we only filter on require-constraints; max/min are reported but not yet
used to disqualify.

Orchestrators read ` + "`frontier --json`" + `'s ` + "`stalled_for`" + ` field against the
configured ` + "`budgets.frontier_stall_k`" + ` to decide when to stop the loop.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			var goal *entity.Goal
			if goalID != "" {
				goal, err = s.ReadGoal(goalID)
			} else {
				goal, err = s.ActiveGoal()
			}
			if err != nil {
				return err
			}
			concls, err := s.ListConclusions()
			if err != nil {
				return err
			}
			concls, err = scopeConclusionsToGoal(s, concls, goal.ID)
			if err != nil {
				return err
			}
			obsByExp := loadObservationsByExperiment(s)
			rows, stalledFor := computeFrontierFromObservations(goal, concls, obsByExp)
			assessment := assessGoalCompletion(goal, concls, obsByExp)

			cfg, err := s.Config()
			if err != nil {
				return err
			}

			if w.IsJSON() {
				return w.JSON(map[string]any{
					"goal_id": goal.ID,
					"objective": map[string]any{
						"instrument": goal.Objective.Instrument,
						"direction":  goal.Objective.Direction,
					},
					"frontier":         rows,
					"goal_assessment":  assessment,
					"stalled_for":      stalledFor,
					"frontier_stall_k": cfg.Budgets.FrontierStallK,
					"stall_reached":    cfg.Budgets.FrontierStallK > 0 && stalledFor >= cfg.Budgets.FrontierStallK,
				})
			}
			w.Textf("[goal: %s, objective: %s, %d supported conclusions]\n", goal.ID, formatGoalObjective(goal), len(rows))
			w.Textln("")
			if len(rows) == 0 {
				w.Textln("(no supported+feasible conclusions yet)")
				w.Textln("")
			} else {
				for i, r := range rows {
					marker := "  "
					if i == 0 {
						marker = "* "
					}
					w.Textf("%s%s  %s  %s=%.6g\n", marker, r.Conclusion, r.Hypothesis, goal.Objective.Instrument, r.Value)
				}
				w.Textln("")
			}
			switch assessment.Mode {
			case "threshold":
				w.Textf("goal_assessment: threshold=%g -> %s", assessment.Threshold, assessment.OnThreshold)
				if assessment.Met {
					w.Textf(" (met by %s; recommended=%s)\n", assessment.MetByConclusion, assessment.RecommendedAction)
				} else {
					w.Textf(" (not yet met; recommended=%s)\n", assessment.RecommendedAction)
				}
			default:
				w.Textln("goal_assessment: open-ended -> continue_until_stall (recommended=continue)")
			}
			w.Textf("stalled_for: %d", stalledFor)
			if cfg.Budgets.FrontierStallK > 0 {
				w.Textf(" (stall-k=%d)", cfg.Budgets.FrontierStallK)
				if stalledFor >= cfg.Budgets.FrontierStallK {
					w.Textln(" — STALL REACHED, orchestrator should stop")
				} else {
					w.Textln("")
				}
			} else {
				w.Textln("")
			}
			return nil
		},
	}
	c.Flags().StringVar(&goalID, "goal", "", "goal to scope the frontier to (defaults to active goal)")
	return []*cobra.Command{c}
}

// scopeConclusionsToGoal drops conclusions whose hypothesis is not bound to
// goalID. When goalID is empty the list is returned unchanged — the caller
// has already signalled "no goal scoping".
func scopeConclusionsToGoal(s *store.Store, concls []*entity.Conclusion, goalID string) ([]*entity.Conclusion, error) {
	if goalID == "" {
		return concls, nil
	}
	cache := map[string]string{}
	out := make([]*entity.Conclusion, 0, len(concls))
	for _, c := range concls {
		hid := c.Hypothesis
		gid, ok := cache[hid]
		if !ok {
			h, err := s.ReadHypothesis(hid)
			if err != nil {
				return nil, err
			}
			gid = h.GoalID
			cache[hid] = gid
		}
		if gid == goalID {
			out = append(out, c)
		}
	}
	return out, nil
}

type frontierRow struct {
	Conclusion string  `json:"conclusion"`
	Hypothesis string  `json:"hypothesis"`
	Candidate  string  `json:"candidate_experiment"`
	Value      float64 `json:"value"`
	DeltaFrac  float64 `json:"delta_frac"`
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
	return computeFrontierFromObservations(goal, concls, loadObservationsByExperiment(s))
}

func computeFrontierFromObservations(goal *entity.Goal, concls []*entity.Conclusion, obsByExp map[string][]*entity.Observation) (rows []frontierRow, stalledFor int) {
	rows = []frontierRow{} // always non-nil so --json emits [] not null
	requireByInst := frontierRequireConstraints(goal)

	for _, c := range concls {
		val, ok := frontierCandidateValue(goal, c, obsByExp, requireByInst)
		if !ok {
			continue
		}
		rows = append(rows, frontierRow{
			Conclusion: c.ID,
			Hypothesis: c.Hypothesis,
			Candidate:  c.CandidateExp,
			Value:      val,
			DeltaFrac:  c.Effect.DeltaFrac,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return betterFrontierValue(goal.Objective.Direction, rows[i].Value, rows[j].Value)
	})

	sortedByTime := make([]*entity.Conclusion, len(concls))
	copy(sortedByTime, concls)
	sort.Slice(sortedByTime, func(i, j int) bool { return sortedByTime[i].CreatedAt.Before(sortedByTime[j].CreatedAt) })

	hasBest := false
	var bestValue float64
	for _, c := range sortedByTime {
		val, ok := frontierCandidateValue(goal, c, obsByExp, requireByInst)
		if !ok {
			if hasBest {
				stalledFor++
			}
			continue
		}
		if !hasBest {
			hasBest = true
			bestValue = val
			stalledFor = 0
			continue
		}
		if betterFrontierValue(goal.Objective.Direction, val, bestValue) {
			bestValue = val
			stalledFor = 0
		} else {
			stalledFor++
		}
	}
	return rows, stalledFor
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
	return candidateObjectiveValue(obsByExp[c.CandidateExp], goal.Objective.Instrument), true
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

	requireByInst := frontierRequireConstraints(goal)
	var bestReviewed frontierCandidate
	hasReviewed := false
	for _, c := range concls {
		if c == nil || c.ReviewedBy == "" {
			continue
		}
		val, ok := frontierCandidateValue(goal, c, obsByExp, requireByInst)
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

func candidateObjectiveValue(obs []*entity.Observation, instrument string) float64 {
	for _, o := range obs {
		if o.Instrument == instrument {
			return o.Value
		}
	}
	return 0
}
