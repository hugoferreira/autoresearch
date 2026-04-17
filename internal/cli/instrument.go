package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func instrumentCommands() []*cobra.Command {
	i := &cobra.Command{
		Use:   "instrument",
		Short: "Manage measurement instruments",
	}
	i.AddCommand(instrumentListCmd(), instrumentRegisterCmd())
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
	c.Flags().StringArrayVar(&cmdStr, "cmd", nil, "shell argv element; repeat once per element (e.g. --cmd make --cmd -f --cmd Makefile). Commas in a value are preserved, so pipelines like `awk '{gsub(/ /,\"\",x)}'` pass through as a single element.")
	c.Flags().StringVar(&parser, "parser", "", "parser name (builtin:passfail | builtin:timing | builtin:size | builtin:scalar)")
	c.Flags().StringVar(&pattern, "pattern", "", "regex with exactly one capture group (required for builtin:scalar)")
	c.Flags().StringVar(&unit, "unit", "", "unit of measurement (e.g. cycles, bytes, seconds, instructions)")
	c.Flags().StringArrayVar(&requires, "requires", nil, "prerequisite as 'instrument=pass' (repeatable); observe will refuse to run this instrument until prerequisites have passing observations")
	c.Flags().IntVar(&minSamples, "min-samples", 0, "minimum samples required (strict mode)")
	addAuthorFlag(c, &author, "")
	return c
}
