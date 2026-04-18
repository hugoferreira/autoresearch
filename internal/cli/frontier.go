package cli

import (
	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/readmodel"
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
	obsByExp := readmodel.LoadObservationsByExperiment(s)
	expClassByID, err := readmodel.ClassifyAllExperimentsForRead(s)
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
			frontier := readmodel.BuildFrontierSnapshot(goal, concls, obsByExp, expClassByID)
			out = append(out, goalFrontier{
				Goal:       goal,
				Rows:       frontier.Rows,
				Assessment: frontier.Assessment,
				StalledFor: frontier.StalledFor,
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
	frontier := readmodel.BuildFrontierSnapshot(goal, concls, obsByExp, expClassByID)
	return []goalFrontier{{
		Goal:       goal,
		Rows:       frontier.Rows,
		Assessment: frontier.Assessment,
		StalledFor: frontier.StalledFor,
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

type frontierRow = readmodel.FrontierRow
type frontierGoalAssessment = readmodel.FrontierGoalAssessment
