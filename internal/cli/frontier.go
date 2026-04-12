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
			rows, stalledFor := computeFrontier(s, goal, concls)

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
					"stalled_for":      stalledFor,
					"frontier_stall_k": cfg.Budgets.FrontierStallK,
					"stall_reached":    cfg.Budgets.FrontierStallK > 0 && stalledFor >= cfg.Budgets.FrontierStallK,
				})
			}
			if len(rows) == 0 {
				w.Textln("(no supported+feasible conclusions yet)")
				return nil
			}
			w.Textf("[goal: %s, objective: %s %s, %d supported conclusions]\n", goal.ID, goal.Objective.Direction, goal.Objective.Instrument, len(rows))
			w.Textln("")
			for i, r := range rows {
				marker := "  "
				if i == 0 {
					marker = "* "
				}
				w.Textf("%s%s  %s  %s=%.6g\n", marker, r.Conclusion, r.Hypothesis, goal.Objective.Instrument, r.Value)
			}
			w.Textln("")
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

// computeFrontier returns rows in best-first order for the objective and the
// count of conclusions written since the last frontier improvement.
func computeFrontier(s *store.Store, goal *entity.Goal, concls []*entity.Conclusion) (rows []frontierRow, stalledFor int) {
	rows = []frontierRow{} // always non-nil so --json emits [] not null
	// Filter: supported + feasible (require-constraints satisfied).
	requireByInst := map[string]string{}
	for _, c := range goal.Constraints {
		if c.Require != "" {
			requireByInst[c.Instrument] = c.Require
		}
	}

	for _, c := range concls {
		if c.Verdict != entity.VerdictSupported {
			continue
		}
		if c.Effect.Instrument != goal.Objective.Instrument {
			continue
		}
		if !requireSatisfied(s, c, requireByInst) {
			continue
		}
		value := candidateObjectiveValue(s, c, goal.Objective.Instrument)
		rows = append(rows, frontierRow{
			Conclusion: c.ID,
			Hypothesis: c.Hypothesis,
			Candidate:  c.CandidateExp,
			Value:      value,
			DeltaFrac:  c.Effect.DeltaFrac,
		})
	}
	// Sort best-first.
	sort.Slice(rows, func(i, j int) bool {
		if goal.Objective.Direction == "decrease" {
			return rows[i].Value < rows[j].Value
		}
		return rows[i].Value > rows[j].Value
	})

	// stalled_for: walk conclusions in chronological order, track the
	// best-so-far, count how many supported conclusions written AFTER the
	// last improvement.
	sortedByTime := make([]*entity.Conclusion, len(concls))
	copy(sortedByTime, concls)
	sort.Slice(sortedByTime, func(i, j int) bool { return sortedByTime[i].CreatedAt.Before(sortedByTime[j].CreatedAt) })

	hasBest := false
	var bestValue float64
	for _, c := range sortedByTime {
		if c.Verdict != entity.VerdictSupported || c.Effect.Instrument != goal.Objective.Instrument {
			if hasBest {
				stalledFor++
			}
			continue
		}
		if !requireSatisfied(s, c, requireByInst) {
			if hasBest {
				stalledFor++
			}
			continue
		}
		val := candidateObjectiveValue(s, c, goal.Objective.Instrument)
		if !hasBest {
			hasBest = true
			bestValue = val
			stalledFor = 0
			continue
		}
		improved := false
		if goal.Objective.Direction == "decrease" && val < bestValue {
			improved = true
		}
		if goal.Objective.Direction == "increase" && val > bestValue {
			improved = true
		}
		if improved {
			bestValue = val
			stalledFor = 0
		} else {
			stalledFor++
		}
	}
	return rows, stalledFor
}

func requireSatisfied(s *store.Store, c *entity.Conclusion, req map[string]string) bool {
	if len(req) == 0 {
		return true
	}
	obs, err := s.ListObservationsForExperiment(c.CandidateExp)
	if err != nil {
		return false
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

func candidateObjectiveValue(s *store.Store, c *entity.Conclusion, instrument string) float64 {
	obs, err := s.ListObservationsForExperiment(c.CandidateExp)
	if err != nil {
		return 0
	}
	for _, o := range obs {
		if o.Instrument == instrument {
			return o.Value
		}
	}
	return 0
}
