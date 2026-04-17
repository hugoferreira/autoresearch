package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func instrumentCommands() []*cobra.Command {
	i := &cobra.Command{
		Use:   "instrument",
		Short: "Manage measurement instruments",
	}
	i.AddCommand(instrumentListCmd(), instrumentRegisterCmd(), instrumentDeleteCmd())
	return []*cobra.Command{i}
}

func instrumentListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered instruments",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			insts, err := s.ListInstruments()
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(insts)
			}
			if len(insts) == 0 {
				w.Textln("(no instruments registered)")
				return nil
			}
			names := make([]string, 0, len(insts))
			for n := range insts {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				inst := insts[n]
				unit := inst.Unit
				if unit == "" {
					unit = "-"
				}
				extra := ""
				if inst.Pattern != "" {
					extra = fmt.Sprintf("  pattern=/%s/", inst.Pattern)
				}
				if len(inst.Requires) > 0 {
					extra += fmt.Sprintf("  requires=%s", strings.Join(inst.Requires, ","))
				}
				w.Textf("  %-16s  parser=%-18s  unit=%-12s%s\n", n, inst.Parser, unit, extra)
			}
			return nil
		},
	}
}

func instrumentRegisterCmd() *cobra.Command {
	var (
		cmdStr     []string
		parser     string
		pattern    string
		unit       string
		requires   []string
		minSamples int
		author     string
	)
	c := &cobra.Command{
		Use:   "register <name>",
		Short: "Register a new instrument in .research/config.yaml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			name := args[0]

			s, err := openStoreLive()
			if err != nil {
				return err
			}
			inst := store.Instrument{
				Cmd:        cmdStr,
				Parser:     parser,
				Pattern:    pattern,
				Unit:       unit,
				Requires:   requires,
				MinSamples: minSamples,
			}
			if err := dryRun(w, fmt.Sprintf("register instrument %q (unit=%s)", name, unit), map[string]any{"name": name, "instrument": inst}); err != nil {
				return err
			}
			if err := s.RegisterInstrument(name, inst); err != nil {
				return err
			}
			eventData := map[string]any{
				"cmd":    inst.Cmd,
				"parser": inst.Parser,
				"unit":   inst.Unit,
			}
			if inst.Pattern != "" {
				eventData["pattern"] = inst.Pattern
			}
			if inst.MinSamples > 0 {
				eventData["min_samples"] = inst.MinSamples
			}
			if len(inst.Requires) > 0 {
				eventData["requires"] = inst.Requires
			}
			if err := emitEvent(s, "instrument.register", author, name, eventData); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("registered instrument %q", name),
				map[string]any{"status": "ok", "name": name, "instrument": inst},
			)
		},
	}
	c.Flags().StringSliceVar(&cmdStr, "cmd", nil, "shell argv of the instrument (comma-separated)")
	c.Flags().StringVar(&parser, "parser", "", "parser name (builtin:passfail | builtin:timing | builtin:size | builtin:scalar)")
	c.Flags().StringVar(&pattern, "pattern", "", "regex with exactly one capture group (required for builtin:scalar)")
	c.Flags().StringVar(&unit, "unit", "", "unit of measurement (e.g. cycles, bytes, seconds, instructions)")
	c.Flags().StringArrayVar(&requires, "requires", nil, "prerequisite as 'instrument=pass' (repeatable); observe will refuse to run this instrument until prerequisites have passing observations")
	c.Flags().IntVar(&minSamples, "min-samples", 0, "minimum samples required (strict mode)")
	addAuthorFlag(c, &author, "")
	return c
}

func instrumentDeleteCmd() *cobra.Command {
	var (
		reason string
		force  bool
		author string
	)
	c := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an instrument from .research/config.yaml",
		Long: `Delete removes an instrument definition from the project config.

By default, delete refuses to remove an instrument that is still
referenced by an active goal's objective, an active goal's constraint,
any hypothesis's prediction, or any recorded observation. Deleting a
referenced instrument would orphan the reference; for those cases the
right action is usually a new goal, or a hypothesis kill/supersede
rather than stripping the instrument out from under them.

--force bypasses the reference check except for active-goal-objective
references, which are never bypassable (the goal becomes structurally
unmeasurable). Use --force with intent; the event payload records
what was orphaned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			name := args[0]

			s, err := openStoreLive()
			if err != nil {
				return err
			}

			insts, err := s.ListInstruments()
			if err != nil {
				return err
			}
			if _, ok := insts[name]; !ok {
				return fmt.Errorf("instrument %q: %w", name, store.ErrInstrumentNotFound)
			}

			goals, err := s.ListGoals()
			if err != nil {
				return fmt.Errorf("list goals: %w", err)
			}
			hyps, err := s.ListHypotheses()
			if err != nil {
				return fmt.Errorf("list hypotheses: %w", err)
			}
			obs, err := s.ListObservations()
			if err != nil {
				return fmt.Errorf("list observations: %w", err)
			}

			usage := firewall.CheckInstrumentSafeToDelete(name, goals, hyps, obs)

			if !usage.Ok() {
				if usage.BlocksEvenWithForce() {
					return fmt.Errorf("refusing to delete instrument %q: referenced by active goal objective (%s). Start a new goal rather than remove the objective instrument",
						name, strings.Join(usage.GoalObjectives, ", "))
				}
				if !force {
					return fmt.Errorf("refusing to delete instrument %q: still referenced (%s). Re-run with --force to delete anyway (orphaned references will be listed in the event payload), or prefer killing the referencing hypotheses / concluding the goal first",
						name, usage.Summary())
				}
			}

			if reason == "" {
				reason = "(no reason given)"
			}
			action := fmt.Sprintf("delete instrument %q", name)
			if force && !usage.Ok() {
				action = fmt.Sprintf("force-delete instrument %q (orphaning %s)", name, usage.Summary())
			}

			eventData := map[string]any{
				"name":   name,
				"reason": reason,
				"forced": force && !usage.Ok(),
			}
			if !usage.Ok() {
				eventData["orphaned"] = map[string]any{
					"goal_constraints": usage.GoalConstraints,
					"hypotheses":       usage.Hypotheses,
					"observations":     usage.Observations,
				}
			}

			if err := dryRun(w, action, eventData); err != nil {
				return err
			}

			deleted, err := s.DeleteInstrument(name)
			if err != nil {
				if errors.Is(err, store.ErrInstrumentNotFound) {
					return fmt.Errorf("instrument %q: %w", name, err)
				}
				return err
			}

			if err := emitEvent(s, "instrument.delete", or(author, "human"), name, eventData); err != nil {
				return err
			}

			return w.Emit(
				fmt.Sprintf("deleted instrument %q", name),
				map[string]any{"status": "ok", "name": name, "instrument": deleted, "forced": force && !usage.Ok()},
			)
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "why the instrument is being removed (recorded on the instrument.delete event)")
	c.Flags().BoolVar(&force, "force", false, "override the reference check (does not override active-goal-objective references)")
	addAuthorFlag(c, &author, "human")
	return c
}
