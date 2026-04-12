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
		Short: "Manage the research goal",
	}
	goal.AddCommand(goalSetCmd(), goalShowCmd())
	return []*cobra.Command{goal}
}

func goalSetCmd() *cobra.Command {
	var (
		file            string
		objInstrument   string
		objTarget       string
		objDirection    string
		objTargetEffect float64
		constraintMax   []string
		constraintMin   []string
		constraintReq   []string
		steeringText    string
	)
	c := &cobra.Command{
		Use:   "set",
		Short: "Set the research goal (from a file OR from flags)",
		Long: `Set the research goal. Two input modes, mutually exclusive:

  --file goal.md            Read a YAML-frontmatter goal document.
  --objective-* + --constraint-*   Build the goal from flags directly.

The flag form is the one the main agent session uses when translating a
human's natural-language request — the session never
asks the human to author YAML. Humans who prefer an editor-based
workflow can still write goal.md and point --file at it.

The goal's objective must reference a registered instrument. Goals
whose objective has no instrument are rejected with a pointer to the
scope-boundary documentation — they belong to a feature-delivery tool,
not autoresearch.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			fileMode := file != ""
			flagMode := objInstrument != "" || objDirection != "" ||
				len(constraintMax) > 0 || len(constraintMin) > 0 || len(constraintReq) > 0
			if fileMode && flagMode {
				return errors.New("--file and --objective-* flags are mutually exclusive")
			}
			if !fileMode && !flagMode {
				return errors.New("provide either --file or --objective-instrument + --objective-direction + at least one --constraint-*")
			}

			var g *entity.Goal
			if fileMode {
				data, err := os.ReadFile(file)
				if err != nil {
					return fmt.Errorf("read goal file: %w", err)
				}
				g, err = entity.ParseGoal(data)
				if err != nil {
					return fmt.Errorf("parse goal: %w", err)
				}
			} else {
				var err error
				g, err = buildGoalFromFlags(objInstrument, objTarget, objDirection, objTargetEffect,
					constraintMax, constraintMin, constraintReq, steeringText)
				if err != nil {
					return err
				}
			}

			s, err := openStoreLive()
			if err != nil {
				return err
			}
			cfg, err := s.Config()
			if err != nil {
				return err
			}
			if err := firewall.ValidateGoal(g, cfg); err != nil {
				return err
			}

			if globalDryRun {
				return w.Emit(
					fmt.Sprintf("[dry-run] would set goal (objective: %s on %s)", g.Objective.Direction, g.Objective.Instrument),
					map[string]any{"status": "dry-run", "goal": g},
				)
			}
			if err := s.WriteGoal(g); err != nil {
				return err
			}
			if err := s.AppendEvent(store.Event{
				Kind:    "goal.set",
				Actor:   "human",
				Subject: g.Objective.Instrument,
			}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("goal set: %s on %s (target_effect=%g, %d constraints)",
					g.Objective.Direction, g.Objective.Instrument, g.Objective.TargetEffect, len(g.Constraints)),
				map[string]any{"status": "ok", "goal": g},
			)
		},
	}
	c.Flags().StringVar(&file, "file", "", "path to goal.md (mutually exclusive with --objective-*)")
	c.Flags().StringVar(&objInstrument, "objective-instrument", "", "name of the registered instrument the objective targets")
	c.Flags().StringVar(&objTarget, "objective-target", "", "what inside the target is being measured (optional, e.g. 'dsp_fir')")
	c.Flags().StringVar(&objDirection, "objective-direction", "", "increase | decrease")
	c.Flags().Float64Var(&objTargetEffect, "objective-target-effect", 0, "fractional effect the user aspires to (optional)")
	c.Flags().StringArrayVar(&constraintMax, "constraint-max", nil, "max constraint as 'instrument=value' (repeatable)")
	c.Flags().StringArrayVar(&constraintMin, "constraint-min", nil, "min constraint as 'instrument=value' (repeatable)")
	c.Flags().StringArrayVar(&constraintReq, "constraint-require", nil, "require constraint as 'instrument=value' (repeatable, e.g. 'host_test=pass')")
	c.Flags().StringVar(&steeringText, "steering", "", "initial steering note to place in the # Steering section")
	return c
}

func buildGoalFromFlags(
	instrument, target, direction string,
	targetEffect float64,
	maxSpecs, minSpecs, reqSpecs []string,
	steering string,
) (*entity.Goal, error) {
	if strings.TrimSpace(instrument) == "" {
		return nil, errors.New("--objective-instrument is required in flag mode")
	}
	if direction != "increase" && direction != "decrease" {
		return nil, fmt.Errorf("--objective-direction must be 'increase' or 'decrease', got %q", direction)
	}
	g := &entity.Goal{
		SchemaVersion: 1,
		Objective: entity.Objective{
			Instrument:   instrument,
			Target:       target,
			Direction:    direction,
			TargetEffect: targetEffect,
		},
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
		Use:   "show",
		Short: "Show the current research goal",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			g, err := s.ReadGoal()
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(g)
			}
			w.Textf("objective:    %s %s", g.Objective.Direction, g.Objective.Instrument)
			if g.Objective.Target != "" {
				w.Textf(" on %s", g.Objective.Target)
			}
			if g.Objective.TargetEffect > 0 {
				w.Textf(" (target_effect=%g)", g.Objective.TargetEffect)
			}
			w.Textln("")
			w.Textln("constraints:")
			for _, cst := range g.Constraints {
				switch {
				case cst.Max != nil:
					w.Textf("  %-16s  max=%g\n", cst.Instrument, *cst.Max)
				case cst.Min != nil:
					w.Textf("  %-16s  min=%g\n", cst.Instrument, *cst.Min)
				case cst.Require != "":
					w.Textf("  %-16s  require=%s\n", cst.Instrument, cst.Require)
				}
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
