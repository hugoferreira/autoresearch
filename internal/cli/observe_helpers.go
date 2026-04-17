package cli

import (
	"context"
	"fmt"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/instrument"
	"github.com/bytter/autoresearch/internal/store"
)

// observationResult holds the output of a single instrument observation,
// used by observeAll to collect results for display.
type observationResult struct {
	ID    string  `json:"id"`
	Inst  string  `json:"instrument"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// runAndRecordObservation runs a single instrument against an experiment's
// worktree, writes the observation and artifacts to the store, emits the
// observation.record event, and returns the observation entity.
//
// This is the shared core used by `observe`, `observe --all`, and
// `experiment baseline`. It does NOT check firewall gates (observation
// request validation, instrument dependencies, unchanged-worktree guard)
// — the caller is responsible for those.
func runAndRecordObservation(
	s *store.Store,
	cfg *store.Config,
	exp *entity.Experiment,
	instName string,
	samples int,
	author string,
) (*entity.Observation, error) {
	inst := cfg.Instruments[instName]

	ctx := context.Background()
	result, err := instrument.Run(ctx, instrument.Config{
		ProjectDir:  globalProjectDir,
		WorktreeDir: exp.Worktree,
		Name:        instName,
		Instrument:  inst,
		Samples:     samples,
	})
	if err != nil {
		return nil, fmt.Errorf("instrument %s: %w", instName, err)
	}

	var obsArts []entity.Artifact
	for _, ac := range result.Artifacts {
		sha, rel, err := s.WriteArtifact(ac.Content, ac.Filename)
		if err != nil {
			return nil, fmt.Errorf("write artifact %q: %w", ac.Name, err)
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
		return nil, err
	}
	unit := result.Unit
	if unit == "" {
		unit = inst.Unit
	}
	obs := &entity.Observation{
		ID:          id,
		Experiment:  exp.ID,
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
		Author:      or(author, "agent:observer"),
		Aux:         result.Aux,
	}
	obs.Normalize()
	if err := s.WriteObservation(obs); err != nil {
		return nil, fmt.Errorf("write observation: %w", err)
	}

	artShas := make([]string, 0, len(obsArts))
	for _, a := range obsArts {
		artShas = append(artShas, a.SHA)
	}
	eventData := map[string]any{
		"experiment":    exp.ID,
		"instrument":    instName,
		"value":         result.Value,
		"unit":          unit,
		"samples":       result.SamplesN,
		"artifact_shas": artShas,
	}
	if result.Pass != nil {
		eventData["pass"] = *result.Pass
	}
	if result.CILow != nil {
		eventData["ci_low"] = *result.CILow
	}
	if result.CIHigh != nil {
		eventData["ci_high"] = *result.CIHigh
	}
	if result.ExitCode != 0 {
		eventData["exit_code"] = result.ExitCode
	}
	if err := emitEvent(s, "observation.record", or(author, "agent:observer"), id, eventData); err != nil {
		return nil, err
	}

	return obs, nil
}

// observeAll runs all given instruments in dependency-safe order against an
// experiment. It iterates instruments, skipping those whose requires deps
// are not yet satisfied, and retries until all are done or no progress is
// made. Returns the list of observation results for display.
func observeAll(
	s *store.Store,
	cfg *store.Config,
	exp *entity.Experiment,
	instruments []string,
	samples int,
	author string,
) ([]observationResult, error) {
	var results []observationResult
	var priorObs []*entity.Observation

	// Seed with any existing observations on this experiment so that
	// partially-observed experiments can resume.
	if existing, err := s.ListObservationsForExperiment(exp.ID); err == nil {
		priorObs = existing
	}

	remaining := make([]string, len(instruments))
	copy(remaining, instruments)

	for len(remaining) > 0 {
		progress := false
		var deferred []string
		for _, instName := range remaining {
			if err := firewall.CheckInstrumentDependencies(instName, cfg, priorObs); err != nil {
				deferred = append(deferred, instName)
				continue
			}

			obs, err := runAndRecordObservation(s, cfg, exp, instName, samples, author)
			if err != nil {
				return results, err
			}
			priorObs = append(priorObs, obs)

			unit := obs.Unit
			results = append(results, observationResult{
				ID:    obs.ID,
				Inst:  instName,
				Value: obs.Value,
				Unit:  unit,
			})
			progress = true
		}
		if !progress {
			return results, fmt.Errorf("stuck: instruments %v have unsatisfied dependencies", deferred)
		}
		remaining = deferred
	}

	// Bump experiment status to measured if this was the first observation.
	if exp.Status == entity.ExpImplemented {
		exp.Status = entity.ExpMeasured
		if err := s.WriteExperiment(exp); err != nil {
			return results, fmt.Errorf("update experiment status: %w", err)
		}
	}

	return results, nil
}
