package cli

import (
	"errors"
	"fmt"

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
		appendMode     bool
		allowUnchanged bool
		all            bool
		candidateRef   string
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
observation firewall, made physical.

By default, observe is idempotent for the current implementation attempt
and measured candidate provenance: if enough samples already exist for the
current implementation attempt and candidate ref/SHA, it no-ops; if some
exist but not enough, it tops up to the requested total.

For non-baseline experiments, observe requires --candidate-ref. The CLI
does not create candidate commits or refs for you: use normal git to
create a clean, reviewable ref for the commit you want to measure, then
pass that ref to observe. The worktree must be clean and HEAD must match
the candidate ref. Pass --append to force another run.`,
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
					proceedArgs := "--all"
					if all {
						proceedArgs = "--all"
					} else {
						proceedArgs = fmt.Sprintf("--instrument %s", instName)
					}
					return fmt.Errorf(
						"experiment %s branch %s has no commits above baseline %s — "+
							"the implementation may not have succeeded\n"+
							"  reset:   autoresearch experiment reset %s --reason \"...\"\n"+
							"  proceed: autoresearch observe %s %s --candidate-ref <ref> --allow-unchanged",
						expID, exp.Branch, exp.Baseline.SHA[:12], expID, expID, proceedArgs)
				}
			}

			// --all: run every instrument on the experiment in dependency order.
			if all {
				if len(exp.Instruments) == 0 {
					return fmt.Errorf("experiment %s declares no instruments", expID)
				}
				scope, priorObs, err := loadCurrentObservations(s, exp, candidateRef)
				if err != nil {
					return err
				}
				payload := map[string]any{"instruments": exp.Instruments, "worktree": exp.Worktree}
				if scope.CandidateRef != "" {
					payload["candidate_ref"] = scope.CandidateRef
					payload["candidate_sha"] = scope.CandidateSHA
				}
				if err := dryRun(w, fmt.Sprintf("observe all %d instruments on %s", len(exp.Instruments), expID), payload); err != nil {
					return err
				}

				exec, err := observeAll(s, cfg, exp, scope, priorObs, exp.Instruments, samples, appendMode, or(author, "agent:observer"))
				if err != nil {
					return err
				}
				summary := buildObserveResultSummary(exec.Results, exec.CurrentObservations, exec.NewObservations)

				if w.IsJSON() {
					return w.JSON(map[string]any{
						"status":              "ok",
						"experiment":          expID,
						"action":              summary.Action,
						"observations":        summary.CurrentIDs,
						"new_observations":    summary.RecordedIDs,
						"reused_observations": summary.ReusedIDs,
						"results":             exec.Results,
					})
				}
				switch {
				case summary.RecordedCount == 0 && summary.SkippedCount > 0:
					w.Textf("no new observations on %s; %d instrument(s) already satisfied\n", expID, summary.SkippedCount)
				case summary.SkippedCount == 0:
					w.Textf("observed %d instrument(s) on %s\n", summary.RecordedCount, expID)
				default:
					w.Textf("observed %d instrument(s) on %s; skipped %d already satisfied instrument(s)\n", summary.RecordedCount, expID, summary.SkippedCount)
				}
				for _, r := range exec.Results {
					if r.skipped() {
						w.Textf("  %-16s already satisfied (have %d, target %d)\n", r.Inst, r.CurrentSamples, r.TargetSamples)
						continue
					}
					w.Textf("  %-16s %s = %g %s  (recorded %d, now %d/%d)\n", r.ID, r.Inst, r.Value, r.Unit, r.Samples, r.CurrentSamples, r.TargetSamples)
				}
				return nil
			}

			// Single instrument mode.
			strict := cfg.Mode == "" || cfg.Mode == "strict"
			if err := firewall.CheckObservationRequest(instName, samples, exp, cfg, strict); err != nil {
				return err
			}
			scope, priorObs, err := loadCurrentObservations(s, exp, candidateRef)
			if err != nil {
				return err
			}
			check, err := buildObserveSampleCheck(cfg, expID, instName, samples, priorObs)
			if err != nil {
				return err
			}
			if !appendMode && check.TargetSatisfied {
				result := buildSkippedObservationResult(check)
				return w.Emit(
					fmt.Sprintf("observation already satisfied for %s on %s: %s", instName, expID, formatObserveSatisfiedText(check)),
					map[string]any{
						"status":       "ok",
						"action":       result.Action,
						"sample_check": check,
					},
				)
			}

			if !force {
				if err := firewall.CheckInstrumentDependencies(instName, cfg, priorObs); err != nil {
					return err
				}
			}
			actionText, actionPayload := describeObserveAction(exp, check, appendMode)
			if scope.CandidateRef != "" {
				actionPayload["candidate_ref"] = scope.CandidateRef
				actionPayload["candidate_sha"] = scope.CandidateSHA
			}
			if err := dryRun(w, actionText, actionPayload); err != nil {
				return err
			}
			exec, err := executeObservationRun(s, cfg, exp, scope, check, appendMode, or(author, "agent:observer"))
			if err != nil {
				return err
			}
			if err := markExperimentMeasuredIfNeeded(s, exp); err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(recordedObservationPayload(exec))
			}
			if len(exec.Observations) == 1 {
				w.Textf("recorded %s\n", exec.Latest.ID)
			} else {
				w.Textf("recorded %d observations for %s on %s\n", len(exec.Observations), instName, expID)
				for _, id := range exec.Result.IDs {
					w.Textf("  id:          %s\n", id)
				}
			}
			w.Textf("  instrument:  %s\n", instName)
			w.Textf("  value:       %g %s\n", exec.Latest.Value, exec.Latest.Unit)
			w.Textf("  samples:     +%d (now %d/%d)\n", exec.Result.Samples, exec.Result.CurrentSamples, exec.Result.TargetSamples)
			if exec.Latest.Samples > 1 && exec.Latest.CILow != nil && exec.Latest.CIHigh != nil {
				w.Textf("  latest run:  %d (95%% CI [%g, %g], %s)\n", exec.Latest.Samples, *exec.Latest.CILow, *exec.Latest.CIHigh, exec.Latest.CIMethod)
			}
			if exec.Latest.Pass != nil {
				w.Textf("  pass:        %v (exit=%d)\n", *exec.Latest.Pass, exec.Latest.ExitCode)
			}
			w.Textln("  artifacts:")
			for _, a := range exec.Latest.Artifacts {
				w.Textf("    - %-10s %s  (%d bytes)  sha=%s\n", a.Name, a.Path, a.Bytes, a.SHA[:12])
			}
			return nil
		},
	}
	c.Flags().StringVar(&instName, "instrument", "", "registered instrument name")
	c.Flags().BoolVar(&all, "all", false, "run all instruments declared on the experiment in dependency order")
	c.Flags().IntVar(&samples, "samples", 0, "desired sample count; without --append, tops up to this total for multi-sample instruments")
	addAuthorFlag(c, &author, "")
	c.Flags().BoolVar(&force, "force", false, "bypass instrument dependency checks")
	c.Flags().BoolVar(&appendMode, "append", false, "force another observation run even when enough samples already exist")
	c.Flags().BoolVar(&allowUnchanged, "allow-unchanged", false, "proceed even when the experiment branch has no commits above baseline")
	c.Flags().StringVar(&candidateRef, "candidate-ref", "", "reviewable git ref naming the clean candidate being measured (required for non-baseline experiments)")
	c.AddCommand(observeCheckCommand())
	return []*cobra.Command{c}
}

func observeCheckCommand() *cobra.Command {
	var (
		instName     string
		samples      int
		candidateRef string
	)
	c := &cobra.Command{
		Use:   "check <exp-id>",
		Short: "Report sample sufficiency for one instrument on one experiment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			expID := args[0]
			if instName == "" {
				return errors.New("--instrument is required")
			}

			s, err := openStore()
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
			_, priorObs, err := loadCurrentObservations(s, exp, candidateRef)
			if err != nil {
				return err
			}
			check, err := buildObserveSampleCheck(cfg, expID, instName, samples, priorObs)
			if err != nil {
				return err
			}

			if w.IsJSON() {
				return w.JSON(map[string]any{
					"status": "ok",
					"check":  check,
				})
			}
			w.Textln(formatObserveCheckText(check))
			return nil
		},
	}
	c.Flags().StringVar(&instName, "instrument", "", "registered instrument name")
	c.Flags().IntVar(&samples, "samples", 0, "desired total sample count to check; 0 uses instrument min_samples or parser default")
	c.Flags().StringVar(&candidateRef, "candidate-ref", "", "reviewable git ref naming the clean candidate whose observations should be checked (required for non-baseline experiments)")
	return c
}
