package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func goalCommands() []*cobra.Command {
	goal := &cobra.Command{
		Use:   "goal",
		Short: "Manage the research goal lifecycle",
		Long: `The research loop is organized into goals. Only one goal is active
at a time; hypotheses and experiments created while that goal is active
are bound to it via goal_id. Goals are concluded or abandoned (terminal)
and replaced by new goals, optionally deriving from a previous one.

Lifecycle:

  goal set     — bootstrap: creates the first goal. Refuses if any goal
                 already exists. Use this once at project start.
  goal new     — start a new goal. Refuses if another goal is still active;
                 conclude or abandon first. Optional --from / --trigger.
  goal conclude [G-NNNN] — mark the active goal concluded (terminal).
  goal abandon  [G-NNNN] — mark the active goal abandoned (terminal).
  goal list     — all goals with status and provenance.
  goal show [G-NNNN]     — defaults to the active goal.

Concluded and abandoned goals are terminal: there is no reopen verb.
Revisit a prior goal by creating a new goal with --from G-NNNN — the
new goal has its own hypotheses, experiments, and observations because
the circumstances (code, tooling, lessons) are different.`,
	}
	goal.AddCommand(
		goalSetCmd(),
		goalNewCmd(),
		goalShowCmd(),
		goalListCmd(),
		goalConcludeCmd(),
		goalAbandonCmd(),
	)
	return []*cobra.Command{goal}
}

type goalFlags struct {
	file             string
	objInstrument    string
	objTarget        string
	objDirection     string
	successThreshold float64
	onSuccess        string
	constraintMax    []string
	constraintMin    []string
	constraintReq    []string
	steeringText     string
}

func addGoalBodyFlags(c *cobra.Command, f *goalFlags) {
	c.Flags().StringVar(&f.file, "file", "", "path to goal.md (mutually exclusive with goal-construction flags)")
	c.Flags().StringVar(&f.objInstrument, "objective-instrument", "", "name of the registered instrument the objective targets")
	c.Flags().StringVar(&f.objTarget, "objective-target", "", "what inside the target is being measured (optional, e.g. 'dsp_fir')")
	c.Flags().StringVar(&f.objDirection, "objective-direction", "", "increase | decrease")
	c.Flags().Float64Var(&f.successThreshold, "success-threshold", 0, "fractional effect that counts as goal satisfaction (optional)")
	c.Flags().StringVar(&f.onSuccess, "on-success", "", "what to do after the success threshold is met: ask_human | stop | continue_until_stall | continue_until_budget_cap")
	c.Flags().StringArrayVar(&f.constraintMax, "constraint-max", nil, "max constraint as 'instrument=value' (repeatable)")
	c.Flags().StringArrayVar(&f.constraintMin, "constraint-min", nil, "min constraint as 'instrument=value' (repeatable)")
	c.Flags().StringArrayVar(&f.constraintReq, "constraint-require", nil, "require constraint as 'instrument=value' (repeatable, e.g. 'host_test=pass')")
	c.Flags().StringVar(&f.steeringText, "steering", "", "initial steering note to place in the # Steering section")
}

// loadGoalFromFlags materializes an in-memory Goal from the common flag set,
// in either file mode (--file goal.md) or flag mode (--objective-* etc.).
func loadGoalFromFlags(f *goalFlags) (*entity.Goal, error) {
	fileMode := f.file != ""
	flagMode := f.objInstrument != "" || f.objDirection != "" ||
		f.successThreshold != 0 || f.onSuccess != "" ||
		len(f.constraintMax) > 0 || len(f.constraintMin) > 0 || len(f.constraintReq) > 0
	if fileMode && flagMode {
		return nil, errors.New("--file and goal-construction flags are mutually exclusive")
	}
	if !fileMode && !flagMode {
		return nil, errors.New("provide either --file or --objective-instrument + --objective-direction + at least one --constraint-*")
	}
	if fileMode {
		data, err := os.ReadFile(f.file)
		if err != nil {
			return nil, fmt.Errorf("read goal file: %w", err)
		}
		return entity.ParseGoal(data)
	}
	return buildGoalFromFlags(f.objInstrument, f.objTarget, f.objDirection, f.successThreshold, f.onSuccess,
		f.constraintMax, f.constraintMin, f.constraintReq, f.steeringText)
}

func goalSetCmd() *cobra.Command {
	var f goalFlags
	var author string
	c := &cobra.Command{
		Use:   "set",
		Short: "Bootstrap the first research goal (refuses if any goal already exists)",
		Long: `Create the very first goal for this project. Refuses if any goal
already exists on disk — use 'goal new' for subsequent goals.

Two input modes, mutually exclusive:

  --file goal.md            Read a YAML-frontmatter goal document.
  --objective-* + --constraint-*   Build the goal from flags directly
                                   (optional: --success-threshold, --on-success).

The flag form is the one the main agent session uses when translating a
human's natural-language request — the session never asks the human to
author YAML. Humans who prefer an editor-based workflow can still write
goal.md and point --file at it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			g, err := loadGoalFromFlags(&f)
			if err != nil {
				return err
			}
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			existing, err := s.ListGoals()
			if err != nil {
				return err
			}
			if len(existing) > 0 {
				return errors.New("a goal already exists; use `autoresearch goal new` to start a new one (conclude or abandon the active goal first if needed)")
			}
			cfg, err := s.Config()
			if err != nil {
				return err
			}
			if err := firewall.ValidateGoal(g, cfg); err != nil {
				return err
			}
			if err := dryRun(w, fmt.Sprintf("bootstrap goal (objective: %s on %s)", g.Objective.Direction, g.Objective.Instrument), map[string]any{"goal": g}); err != nil {
				return err
			}
			id, err := s.AllocID(store.KindGoal)
			if err != nil {
				return err
			}
			now := nowUTC()
			g.ID = id
			g.Status = entity.GoalStatusActive
			g.CreatedAt = &now
			if err := s.WriteGoal(g); err != nil {
				return err
			}
			if err := s.UpdateState(func(st *store.State) error {
				st.CurrentGoalID = id
				return nil
			}); err != nil {
				return err
			}
			if err := emitEvent(s, "goal.set", author, id, map[string]any{
				"instrument": g.Objective.Instrument,
				"direction":  g.Objective.Direction,
			}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("bootstrapped %s: %s (%d constraints)",
					id, formatGoalObjective(g), len(g.Constraints)),
				map[string]any{"status": "ok", "id": id, "goal": g},
			)
		},
	}
	addGoalBodyFlags(c, &f)
	addAuthorFlag(c, &author, "human")
	return c
}

func goalNewCmd() *cobra.Command {
	var (
		f       goalFlags
		from    string
		trigger string
		author  string
	)
	c := &cobra.Command{
		Use:   "new",
		Short: "Start a new research goal (refuses if another goal is active)",
		Long: `Create a new goal and make it the active one. Refuses if another goal
is currently active — conclude or abandon it first.

--from G-NNNN records the previous goal as the parent (defaults to the
most recently closed goal if omitted). --trigger records the
hypothesis/experiment/conclusion/observation that prompted the new goal.

The new goal is independent: hypotheses and experiments created under
it are bound to its goal_id, not inherited from the parent. Revisiting
a goal after the code has evolved is fundamentally a new circumstance.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			g, err := loadGoalFromFlags(&f)
			if err != nil {
				return err
			}
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			if err := firewall.RequireNoActiveGoal(st); err != nil {
				return err
			}
			cfg, err := s.Config()
			if err != nil {
				return err
			}
			if err := firewall.ValidateGoal(g, cfg); err != nil {
				return err
			}

			// Resolve --from: default to most recent closed goal (by id).
			parentID := strings.TrimSpace(from)
			if parentID != "" {
				ok, err := s.GoalExists(parentID)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("--from %s: goal does not exist", parentID)
				}
			} else {
				all, err := s.ListGoals()
				if err != nil {
					return err
				}
				for i := len(all) - 1; i >= 0; i-- {
					if all[i].Status == entity.GoalStatusConcluded || all[i].Status == entity.GoalStatusAbandoned {
						parentID = all[i].ID
						break
					}
				}
			}
			if err := validateTrigger(trigger); err != nil {
				return err
			}

			if err := dryRun(w, fmt.Sprintf("start new goal (objective: %s on %s, from=%q, trigger=%q)", g.Objective.Direction, g.Objective.Instrument, parentID, trigger), map[string]any{"goal": g, "derived_from": parentID, "trigger": trigger}); err != nil {
				return err
			}

			id, err := s.AllocID(store.KindGoal)
			if err != nil {
				return err
			}
			now := nowUTC()
			g.ID = id
			g.Status = entity.GoalStatusActive
			g.CreatedAt = &now
			g.DerivedFrom = parentID
			g.Trigger = strings.TrimSpace(trigger)
			if err := s.WriteGoal(g); err != nil {
				return err
			}
			if err := s.UpdateState(func(st *store.State) error {
				st.CurrentGoalID = id
				return nil
			}); err != nil {
				return err
			}
			if err := emitEvent(s, "goal.new", author, id, map[string]any{
				"instrument":   g.Objective.Instrument,
				"direction":    g.Objective.Direction,
				"derived_from": parentID,
				"trigger":      g.Trigger,
			}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("started %s: %s on %s", id, g.Objective.Direction, g.Objective.Instrument),
				map[string]any{"status": "ok", "id": id, "goal": g},
			)
		},
	}
	addGoalBodyFlags(c, &f)
	c.Flags().StringVar(&from, "from", "", "parent goal id (defaults to most recently closed goal)")
	c.Flags().StringVar(&trigger, "trigger", "", "H-/E-/O-/C- id that prompted this new goal (optional)")
	addAuthorFlag(c, &author, "human")
	return c
}

func validateTrigger(trigger string) error {
	t := strings.TrimSpace(trigger)
	if t == "" {
		return nil
	}
	for _, prefix := range []string{"H-", "E-", "O-", "C-"} {
		if strings.HasPrefix(t, prefix) {
			return nil
		}
	}
	return fmt.Errorf("--trigger %q must be an H-/E-/O-/C- id", trigger)
}

func goalConcludeCmd() *cobra.Command {
	var summary, author string
	c := &cobra.Command{
		Use:   "conclude [G-NNNN]",
		Short: "Mark the active goal (or the named goal) concluded",
		Long: `Mark a goal as concluded. Concluded goals are terminal: there is no
reopen verb. To continue working in the same problem space after the
code has evolved, create a new goal with 'goal new --from <id>'.

If no id is given, the active goal is concluded. --summary is
appended as a '# Closure' section on the goal body.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGoalClosure(args, summary, author, entity.GoalStatusConcluded, "goal.conclude", "concluded")
		},
	}
	c.Flags().StringVar(&summary, "summary", "", "one-paragraph closure summary (optional, persisted on the goal body)")
	addAuthorFlag(c, &author, "human")
	return c
}

func goalAbandonCmd() *cobra.Command {
	var reason, author string
	c := &cobra.Command{
		Use:   "abandon [G-NNNN]",
		Short: "Mark the active goal (or the named goal) abandoned",
		Long: `Mark a goal as abandoned. Abandoned goals are terminal: there is no
reopen verb. --reason is required and persisted on the goal body as
a '# Closure' section.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return errors.New("--reason is required for abandon")
			}
			return runGoalClosure(args, reason, author, entity.GoalStatusAbandoned, "goal.abandon", "abandoned")
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "why the goal is being abandoned (required)")
	addAuthorFlag(c, &author, "human")
	return c
}

func runGoalClosure(args []string, text, author, status, eventKind, label string) error {
	w := output.Default(globalJSON)
	s, err := openStoreLive()
	if err != nil {
		return err
	}
	st, err := s.State()
	if err != nil {
		return err
	}
	var target string
	if len(args) == 1 {
		target = args[0]
	} else {
		target = st.CurrentGoalID
	}
	if target == "" {
		return store.ErrNoActiveGoal
	}
	g, err := s.ReadGoal(target)
	if err != nil {
		return err
	}
	if g.Status != entity.GoalStatusActive {
		return fmt.Errorf("%s has status %q; only active goals can be %s", g.ID, g.Status, label)
	}
	if err := dryRun(w, fmt.Sprintf("mark %s %s", g.ID, label), map[string]any{"id": g.ID, "new_status": status}); err != nil {
		return err
	}
	now := nowUTC()
	g.Status = status
	g.ClosedAt = &now
	g.ClosureReason = strings.TrimSpace(text)
	if g.ClosureReason != "" {
		g.Body = entity.AppendMarkdownSection(g.Body, "Closure", g.ClosureReason)
	}
	if err := s.WriteGoal(g); err != nil {
		return err
	}
	if st.CurrentGoalID == g.ID {
		if err := s.UpdateState(func(st *store.State) error {
			st.CurrentGoalID = ""
			return nil
		}); err != nil {
			return err
		}
	}
	if err := emitEvent(s, eventKind, author, g.ID, map[string]string{"reason": g.ClosureReason}); err != nil {
		return err
	}
	return w.Emit(
		fmt.Sprintf("%s %s", label, g.ID),
		map[string]any{"status": "ok", "id": g.ID, "new_status": status},
	)
}

func buildGoalFromFlags(
	instrument, target, direction string,
	successThreshold float64,
	onSuccess string,
	maxSpecs, minSpecs, reqSpecs []string,
	steering string,
) (*entity.Goal, error) {
	if successThreshold < 0 {
		return nil, errors.New("--success-threshold must be >= 0")
	}
	if successThreshold == 0 && strings.TrimSpace(onSuccess) != "" {
		return nil, errors.New("--on-success requires --success-threshold")
	}
	if strings.TrimSpace(instrument) == "" {
		return nil, errors.New("--objective-instrument is required in flag mode")
	}
	if direction != "increase" && direction != "decrease" {
		return nil, fmt.Errorf("--objective-direction must be 'increase' or 'decrease', got %q", direction)
	}
	g := &entity.Goal{
		SchemaVersion: entity.GoalSchemaVersion,
		Objective: entity.Objective{
			Instrument: instrument,
			Target:     target,
			Direction:  direction,
		},
	}
	if successThreshold > 0 {
		g.Completion = &entity.Completion{
			Threshold:   successThreshold,
			OnThreshold: strings.TrimSpace(onSuccess),
		}
		if g.Completion.OnThreshold == "" {
			g.Completion.OnThreshold = entity.GoalOnThresholdAskHuman
		}
	}
	for _, spec := range maxSpecs {
		c, err := parseConstraintKV(spec, "max")
		if err != nil {
			return nil, err
		}
		g.Constraints = append(g.Constraints, c)
	}
	for _, spec := range minSpecs {
		c, err := parseConstraintKV(spec, "min")
		if err != nil {
			return nil, err
		}
		g.Constraints = append(g.Constraints, c)
	}
	for _, spec := range reqSpecs {
		c, err := parseConstraintKV(spec, "require")
		if err != nil {
			return nil, err
		}
		g.Constraints = append(g.Constraints, c)
	}
	if len(g.Constraints) == 0 {
		return nil, errors.New("at least one --constraint-max / --constraint-min / --constraint-require is required")
	}
	body := "# Steering\n\n"
	if strings.TrimSpace(steering) != "" {
		body += "- " + strings.TrimSpace(steering) + "\n"
	} else {
		body += "_No steering notes yet._\n"
	}
	g.Body = body
	return g, nil
}

func parseConstraintKV(spec, op string) (entity.Constraint, error) {
	spec = strings.TrimSpace(spec)
	idx := strings.IndexByte(spec, '=')
	if idx <= 0 || idx == len(spec)-1 {
		return entity.Constraint{}, fmt.Errorf("constraint %q: expected 'instrument=value'", spec)
	}
	name := strings.TrimSpace(spec[:idx])
	val := strings.TrimSpace(spec[idx+1:])
	c := entity.Constraint{Instrument: name}
	switch op {
	case "require":
		c.Require = val
	case "max":
		n, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return entity.Constraint{}, fmt.Errorf("--constraint-max %q: %w", spec, err)
		}
		c.Max = &n
	case "min":
		n, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return entity.Constraint{}, fmt.Errorf("--constraint-min %q: %w", spec, err)
		}
		c.Min = &n
	default:
		return entity.Constraint{}, fmt.Errorf("unknown op %q", op)
	}
	return c, nil
}

func goalShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [G-NNNN]",
		Short: "Show the active goal (or the named goal)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			var g *entity.Goal
			if len(args) == 1 {
				g, err = s.ReadGoal(args[0])
			} else {
				g, err = s.ActiveGoal()
			}
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(g)
			}
			w.Textf("id:           %s\n", g.ID)
			if g.Status != "" {
				w.Textf("status:       %s\n", g.Status)
			}
			if g.DerivedFrom != "" {
				w.Textf("derived_from: %s\n", g.DerivedFrom)
			}
			if g.Trigger != "" {
				w.Textf("trigger:      %s\n", g.Trigger)
			}
			if g.CreatedAt != nil {
				w.Textf("created_at:   %s\n", g.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
			}
			if g.ClosedAt != nil {
				w.Textf("closed_at:    %s\n", g.ClosedAt.Format("2006-01-02T15:04:05Z07:00"))
			}
			w.Textf("objective:    %s\n", formatGoalObjective(g))
			w.Textf("completion:   %s\n", formatGoalCompletion(g))
			w.Textln("constraints:")
			for _, cst := range g.Constraints {
				w.Textf("  %s\n", entity.FormatConstraint(cst))
			}
			if st := g.Steering(); st != "" {
				w.Textln("")
				w.Textln("steering:")
				w.Textln("  " + st)
			}
			return nil
		},
	}
}

func goalListCmd() *cobra.Command {
	var statusFilter string
	c := &cobra.Command{
		Use:   "list",
		Short: "List all research goals with status and provenance",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			all, err := s.ListGoals()
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			var filtered []*entity.Goal
			for _, g := range all {
				if statusFilter != "" && g.Status != statusFilter {
					continue
				}
				filtered = append(filtered, g)
			}
			if w.IsJSON() {
				return w.JSON(map[string]any{
					"current": st.CurrentGoalID,
					"goals":   filtered,
				})
			}
			if len(filtered) == 0 {
				w.Textln("(no goals)")
				return nil
			}
			for _, g := range filtered {
				marker := "  "
				if g.ID == st.CurrentGoalID {
					marker = "* "
				}
				from := g.DerivedFrom
				if from == "" {
					from = "-"
				}
				w.Textf("%s%-8s  %-10s  from=%-8s  %s %s\n",
					marker, g.ID, g.Status, from, g.Objective.Direction, g.Objective.Instrument)
			}
			return nil
		},
	}
	c.Flags().StringVar(&statusFilter, "status", "", "filter by status (active|concluded|abandoned)")
	return c
}
