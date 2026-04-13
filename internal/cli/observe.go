package cli

import (
	"errors"
	"fmt"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/worktree"
	"github.com/spf13/cobra"
)

func observeCommands() []*cobra.Command {
	var (
		instName       string
		samples        int
		author         string
		force          bool
		allowUnchanged bool
		all            bool
	)
	c := &cobra.Command{
		Use:   "observe <exp-id>",
		Short: "Record instrument-backed observations",
		Long: `Record an observation. The configured instrument's command is executed
inside the experiment's worktree, the combined output is hashed and
stored as a content-addressed artifact, and a structured observation
file is written under .research/observations/.

Use --instrument to run a single instrument, or --all to run every
instrument declared on the experiment in dependency-safe order.

Observations are never hand-authored — the CLI is the sole writer and
the artifact is guaranteed to exist on disk. This is the speculation/
observation firewall, made physical.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			expID := args[0]

			if !all && instName == "" {
				return errors.New("--instrument or --all is required")
			}
			if all && instName != "" {
				return errors.New("--instrument and --all are mutually exclusive")
			}

			s, err := openStoreLive()
			if err != nil {
				return err
			}
			cfg, err := s.Config()
			if err != nil {
				return err
			}
			exp, err := s.ReadExperiment(expID)
			if err != nil {
				return err
			}
			if exp.Worktree == "" {
				return fmt.Errorf("experiment %s has no worktree; run `autoresearch experiment implement %s` first", expID, expID)
			}

			// Refuse if the experiment branch has no commits above baseline
			// and is not a baseline experiment. This catches the pattern
			// where a worktree was created but the coder helper failed to
			// commit any changes.
			if !allowUnchanged && !exp.IsBaseline && exp.Branch != "" && exp.Baseline.SHA != "" {
				if hasCommits, err := worktree.HasCommitsAbove(globalProjectDir, exp.Branch, exp.Baseline.SHA); err == nil && !hasCommits {
					inst := instName
					if all {
						inst = "--all"
					}
					return fmt.Errorf(
						"experiment %s branch %s has no commits above baseline %s — "+
							"the implementation may not have succeeded\n"+
							"  reset:   autoresearch experiment reset %s --reason \"...\"\n"+
							"  proceed: autoresearch observe %s --instrument %s --allow-unchanged",
						expID, exp.Branch, exp.Baseline.SHA[:12], expID, expID, inst)
				}
			}

			// --all: run every instrument on the experiment in dependency order.
			if all {
				if len(exp.Instruments) == 0 {
					return fmt.Errorf("experiment %s declares no instruments", expID)
				}
				if err := dryRun(w, fmt.Sprintf("observe all %d instruments on %s", len(exp.Instruments), expID), map[string]any{"instruments": exp.Instruments, "worktree": exp.Worktree}); err != nil {
					return err
				}

				results, err := observeAll(s, cfg, exp, exp.Instruments, samples, or(author, "agent:observer"))
				if err != nil {
					return err
				}

				if w.IsJSON() {
					ids := make([]string, 0, len(results))
					for _, r := range results {
						ids = append(ids, r.ID)
					}
					return w.JSON(map[string]any{
						"status":       "ok",
						"experiment":   expID,
						"observations": ids,
						"results":      results,
					})
				}
				w.Textf("observed %d instruments on %s\n", len(results), expID)
				for _, r := range results {
					w.Textf("  %-16s %s = %g %s\n", r.ID, r.Inst, r.Value, r.Unit)
				}
				return nil
			}

			// Single instrument mode.
			strict := cfg.Mode == "" || cfg.Mode == "strict"
			if err := firewall.CheckObservationRequest(instName, samples, exp, cfg, strict); err != nil {
				return err
			}
			if !force {
				priorObs, err := s.ListObservationsForExperiment(expID)
				if err != nil {
					return err
				}
				if err := firewall.CheckInstrumentDependencies(instName, cfg, priorObs); err != nil {
					return err
				}
			}

			if err := dryRun(w, fmt.Sprintf("run instrument %q against %s", instName, exp.Worktree), map[string]any{"instrument": instName, "worktree": exp.Worktree}); err != nil {
				return err
			}

			obs, err := runAndRecordObservation(s, cfg, exp, instName, samples, or(author, "agent:observer"))
			if err != nil {
				return err
			}

			// Bump experiment status on first observation.
			if exp.Status == entity.ExpImplemented {
				exp.Status = entity.ExpMeasured
				if err := s.WriteExperiment(exp); err != nil {
					return fmt.Errorf("update experiment status: %w", err)
				}
			}

			if w.IsJSON() {
				return w.JSON(map[string]any{
					"status":      "ok",
					"id":          obs.ID,
					"observation": obs,
				})
			}
			w.Textf("recorded %s\n", obs.ID)
			w.Textf("  instrument:  %s\n", instName)
			w.Textf("  value:       %g %s\n", obs.Value, obs.Unit)
			if obs.Samples > 1 && obs.CILow != nil && obs.CIHigh != nil {
				w.Textf("  samples:     %d (95%% CI [%g, %g], %s)\n", obs.Samples, *obs.CILow, *obs.CIHigh, obs.CIMethod)
			}
			if obs.Pass != nil {
				w.Textf("  pass:        %v (exit=%d)\n", *obs.Pass, obs.ExitCode)
			}
			w.Textln("  artifacts:")
			for _, a := range obs.Artifacts {
				w.Textf("    - %-10s %s  (%d bytes)  sha=%s\n", a.Name, a.Path, a.Bytes, a.SHA[:12])
			}
			return nil
		},
	}
	c.Flags().StringVar(&instName, "instrument", "", "registered instrument name")
	c.Flags().BoolVar(&all, "all", false, "run all instruments declared on the experiment in dependency order")
	c.Flags().IntVar(&samples, "samples", 0, "number of samples (timing); 0 uses instrument min_samples or default 5")
	addAuthorFlag(c, &author, "")
	c.Flags().BoolVar(&force, "force", false, "bypass instrument dependency checks")
	c.Flags().BoolVar(&allowUnchanged, "allow-unchanged", false, "proceed even when the experiment branch has no commits above baseline")
	return []*cobra.Command{c}
}
