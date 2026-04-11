package cli

import (
	"fmt"
	"sort"

	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func instrumentCommands() []*cobra.Command {
	i := &cobra.Command{
		Use:   "instrument",
		Short: "Manage measurement instruments",
	}
	i.AddCommand(instrumentListCmd(), instrumentRegisterCmd(), instrumentRunCmd())
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
				tier := inst.Tier
				if tier == "" {
					tier = "-"
				}
				unit := inst.Unit
				if unit == "" {
					unit = "-"
				}
				extra := ""
				if inst.Pattern != "" {
					extra = fmt.Sprintf("  pattern=/%s/", inst.Pattern)
				}
				w.Textf("  %-16s  tier=%-8s  parser=%-18s  unit=%-12s%s\n", n, tier, inst.Parser, unit, extra)
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
		tier       string
		minSamples int
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
				Tier:       tier,
				MinSamples: minSamples,
			}
			if globalDryRun {
				return w.Emit(
					fmt.Sprintf("[dry-run] would register instrument %q (tier=%s, unit=%s)", name, tier, unit),
					map[string]any{"status": "dry-run", "name": name, "instrument": inst},
				)
			}
			if err := s.RegisterInstrument(name, inst); err != nil {
				return err
			}
			if err := s.AppendEvent(store.Event{
				Kind:    "instrument.register",
				Actor:   "human",
				Subject: name,
				Data:    jsonRaw(inst),
			}); err != nil {
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
	c.Flags().StringVar(&tier, "tier", "", "tier: host | qemu | hardware")
	c.Flags().IntVar(&minSamples, "min-samples", 0, "minimum samples required (strict mode)")
	return c
}

func instrumentRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <name>",
		Short: "Run a single instrument against a worktree",
		RunE:  stub("instrument run"),
	}
}
