package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/instrument"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func observeCommands() []*cobra.Command {
	var (
		instName string
		samples  int
		author   string
		force    bool
	)
	c := &cobra.Command{
		Use:   "observe <exp-id>",
		Short: "Record an instrument-backed observation",
		Long: `Record an observation. The configured instrument's command is executed
inside the experiment's worktree, the combined output is hashed and
stored as a content-addressed artifact, and a structured observation
file is written under .research/observations/.

Observations are never hand-authored — the CLI is the sole writer and
the artifact is guaranteed to exist on disk. This is the speculation/
observation firewall, made physical.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			expID := args[0]
			if instName == "" {
				return errors.New("--instrument is required")
			}
			if author == "" {
				author = "agent:observer"
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

			inst := cfg.Instruments[instName]
			if exp.Worktree == "" {
				return fmt.Errorf("experiment %s has no worktree; run `autoresearch experiment implement %s` first", expID, expID)
			}

			if globalDryRun {
				return w.Emit(
					fmt.Sprintf("[dry-run] would run instrument %q against %s", instName, exp.Worktree),
					map[string]any{"status": "dry-run", "instrument": instName, "worktree": exp.Worktree},
				)
			}

			ctx := context.Background()
			result, err := instrument.Run(ctx, instrument.Config{
				ProjectDir:  globalProjectDir,
				WorktreeDir: exp.Worktree,
				Name:        instName,
				Instrument:  inst,
				Samples:     samples,
			})
			if err != nil {
				return fmt.Errorf("run instrument: %w", err)
			}

			var obsArts []entity.Artifact
			for _, ac := range result.Artifacts {
				sha, rel, err := s.WriteArtifact(ac.Content, ac.Filename)
				if err != nil {
					return fmt.Errorf("write artifact %q: %w", ac.Name, err)
				}
				obsArts = append(obsArts, entity.Artifact{
					Name:  ac.Name,
					SHA:   sha,
					Path:  rel,
					Bytes: int64(len(ac.Content)),
					Mime:  ac.Mime,
				})
			}

			id, err := s.AllocID(store.KindObservation)
			if err != nil {
				return err
			}
			unit := result.Unit
			if unit == "" {
				unit = inst.Unit
			}
			obs := &entity.Observation{
				ID:          id,
				Experiment:  expID,
				Instrument:  instName,
				MeasuredAt:  result.FinishedAt.UTC(),
				Value:       result.Value,
				Unit:        unit,
				Samples:     result.SamplesN,
				PerSample:   result.PerSample,
				CILow:       result.CILow,
				CIHigh:      result.CIHigh,
				CIMethod:    result.CIMethod,
				Pass:        result.Pass,
				Artifacts:   obsArts,
				Command:     result.Command,
				ExitCode:    result.ExitCode,
				Worktree:    exp.Worktree,
				BaselineSHA: exp.Baseline.SHA,
				Author:      author,
				Aux:         result.Aux,
			}
			obs.Normalize()
			if err := s.WriteObservation(obs); err != nil {
				return fmt.Errorf("write observation: %w", err)
			}

			// Bump the experiment status to "measured" the first time any
			// observation is recorded against it. Subsequent observations
			// do not change status.
			if exp.Status == entity.ExpImplemented {
				exp.Status = entity.ExpMeasured
				if err := s.WriteExperiment(exp); err != nil {
					return fmt.Errorf("update experiment status: %w", err)
				}
			}

			artShas := make([]string, 0, len(obsArts))
			for _, a := range obsArts {
				artShas = append(artShas, a.SHA)
			}
			if err := s.AppendEvent(store.Event{
				Kind:    "observation.record",
				Actor:   author,
				Subject: id,
				Data: jsonRaw(map[string]any{
					"experiment":   expID,
					"instrument":   instName,
					"value":        result.Value,
					"unit":         unit,
					"samples":      result.SamplesN,
					"artifact_shas": artShas,
				}),
			}); err != nil {
				return err
			}

			if w.IsJSON() {
				return w.JSON(map[string]any{
					"status":      "ok",
					"id":          id,
					"observation": obs,
				})
			}
			w.Textf("recorded %s\n", id)
			w.Textf("  instrument:  %s\n", instName)
			w.Textf("  value:       %g %s\n", obs.Value, unit)
			if obs.Samples > 1 && obs.CILow != nil && obs.CIHigh != nil {
				w.Textf("  samples:     %d (95%% CI [%g, %g], %s)\n", obs.Samples, *obs.CILow, *obs.CIHigh, obs.CIMethod)
			}
			if obs.Pass != nil {
				w.Textf("  pass:        %v (exit=%d)\n", *obs.Pass, obs.ExitCode)
			}
			w.Textln("  artifacts:")
			for _, a := range obsArts {
				w.Textf("    - %-10s %s  (%d bytes)  sha=%s\n", a.Name, a.Path, a.Bytes, a.SHA[:12])
			}
			return nil
		},
	}
	c.Flags().StringVar(&instName, "instrument", "", "registered instrument name (required)")
	c.Flags().IntVar(&samples, "samples", 0, "number of samples (timing); 0 uses instrument min_samples or default 5")
	c.Flags().StringVar(&author, "author", "", "author (default agent:observer)")
	c.Flags().BoolVar(&force, "force", false, "bypass instrument dependency checks")
	return []*cobra.Command{c}
}
