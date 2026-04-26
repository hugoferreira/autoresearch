package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/instrument"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/worktree"
	"github.com/spf13/cobra"
)

const (
	observePairModeBracket    = "bracket"
	observePairModeInterleave = "interleave"
)

type observePairStep struct {
	arm     string
	segment string
	exp     *entity.Experiment
	scope   observeScope
	samples int
}

type observePairResponse struct {
	Status                string   `json:"status"`
	PairID                string   `json:"pair_id"`
	Mode                  string   `json:"mode"`
	Instrument            string   `json:"instrument"`
	SamplesPerArm         int      `json:"samples_per_arm"`
	CandidateExperiment   string   `json:"candidate_experiment"`
	CandidateRef          string   `json:"candidate_ref,omitempty"`
	CandidateSHA          string   `json:"candidate_sha,omitempty"`
	BaselineExperiment    string   `json:"baseline_experiment"`
	BaselineRef           string   `json:"baseline_ref,omitempty"`
	BaselineSHA           string   `json:"baseline_sha,omitempty"`
	Observations          []string `json:"observations"`
	CandidateObservations []string `json:"candidate_observations"`
	BaselineObservations  []string `json:"baseline_observations"`
	BaselineBefore        []string `json:"baseline_before_observations,omitempty"`
	BaselineAfter         []string `json:"baseline_after_observations,omitempty"`
}

func observePairCommands() []*cobra.Command {
	var (
		baselineExpID  string
		baselineRef    string
		instName       string
		samples        int
		mode           string
		candidateRef   string
		author         string
		force          bool
		allowUnchanged bool
	)
	c := &cobra.Command{
		Use:   "observe-pair <candidate-exp>",
		Short: "Record paired baseline/candidate observations as one measurement unit",
		Long: `Record a paired observation for small effects where baseline drift matters.
The command measures a candidate experiment and a baseline experiment under a
single pair id, preserving raw per-arm samples and recording pair metadata on
each observation.

Mode bracket records baseline -> candidate -> baseline. Mode interleave records
alternating one-sample baseline and candidate observations.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			candidateExpID := args[0]
			if strings.TrimSpace(baselineExpID) == "" {
				return errors.New("--baseline is required")
			}
			if strings.TrimSpace(instName) == "" {
				return errors.New("--instrument is required")
			}
			mode = strings.TrimSpace(mode)
			if mode == "" {
				mode = observePairModeInterleave
			}
			if err := validateObservePairMode(mode); err != nil {
				return err
			}

			s, err := openStoreLive()
			if err != nil {
				return err
			}
			cfg, err := s.Config()
			if err != nil {
				return err
			}
			candidateExp, err := s.ReadExperiment(candidateExpID)
			if err != nil {
				return err
			}
			baselineExp, err := s.ReadExperiment(baselineExpID)
			if err != nil {
				return err
			}
			if candidateExp.Worktree == "" {
				return fmt.Errorf("experiment %s has no worktree; run `autoresearch experiment implement %s` first", candidateExpID, candidateExpID)
			}
			if baselineExp.Worktree == "" {
				return fmt.Errorf("baseline experiment %s has no worktree; run `autoresearch experiment implement %s` first", baselineExpID, baselineExpID)
			}
			if !allowUnchanged {
				if err := ensureObservePairCandidateHasCommits(candidateExp, instName); err != nil {
					return err
				}
			}

			strict := cfg.Mode == "" || cfg.Mode == "strict"
			if err := firewall.CheckObservationRequest(instName, samples, candidateExp, cfg, strict); err != nil {
				return err
			}
			if err := firewall.CheckObservationRequest(instName, samples, baselineExp, cfg, strict); err != nil {
				return err
			}
			candidateScope, candidatePrior, err := loadCurrentObservations(s, candidateExp, candidateRef)
			if err != nil {
				return err
			}
			baselineScope, baselinePrior, err := loadCurrentObservations(s, baselineExp, baselineRef)
			if err != nil {
				return err
			}
			if !force {
				if err := firewall.CheckInstrumentDependencies(instName, cfg, candidatePrior); err != nil {
					return err
				}
				if err := firewall.CheckInstrumentDependencies(instName, cfg, baselinePrior); err != nil {
					return err
				}
			}
			plan := instrument.PlanSamples(cfg.Instruments[instName], samples)
			actionPayload := map[string]any{
				"candidate_experiment": candidateExp.ID,
				"baseline_experiment":  baselineExp.ID,
				"instrument":           instName,
				"mode":                 mode,
				"samples_per_arm":      plan.Target,
			}
			if candidateScope.CandidateRef != "" {
				actionPayload["candidate_ref"] = candidateScope.CandidateRef
				actionPayload["candidate_sha"] = candidateScope.CandidateSHA
			}
			if ref := observePairBaselineRef(baselineExp, baselineScope); ref != "" {
				actionPayload["baseline_ref"] = ref
			}
			if baselineScope.CandidateSHA != "" {
				actionPayload["baseline_sha"] = baselineScope.CandidateSHA
			}
			if err := dryRun(w, fmt.Sprintf("record paired %s observation on %s vs %s", instName, candidateExp.ID, baselineExp.ID), actionPayload); err != nil {
				return err
			}

			pairID, err := s.AllocID(store.KindPair)
			if err != nil {
				return err
			}
			steps := buildObservePairSteps(mode, plan.Target, candidateExp, candidateScope, baselineExp, baselineScope)
			resp := observePairResponse{
				Status:              "ok",
				PairID:              pairID,
				Mode:                mode,
				Instrument:          instName,
				SamplesPerArm:       plan.Target,
				CandidateExperiment: candidateExp.ID,
				CandidateRef:        candidateScope.CandidateRef,
				CandidateSHA:        candidateScope.CandidateSHA,
				BaselineExperiment:  baselineExp.ID,
				BaselineRef:         observePairBaselineRef(baselineExp, baselineScope),
				BaselineSHA:         baselineScope.CandidateSHA,
			}
			for i, step := range steps {
				meta := readmodel.PairedObservationMeta{
					PairID:              pairID,
					Mode:                mode,
					Arm:                 step.arm,
					Segment:             step.segment,
					Order:               i + 1,
					Instrument:          instName,
					CandidateExperiment: candidateExp.ID,
					CandidateAttempt:    candidateScope.Attempt,
					CandidateRef:        candidateScope.CandidateRef,
					CandidateSHA:        candidateScope.CandidateSHA,
					BaselineExperiment:  baselineExp.ID,
					BaselineAttempt:     baselineScope.Attempt,
					BaselineRef:         observePairBaselineRef(baselineExp, baselineScope),
					BaselineSHA:         baselineScope.CandidateSHA,
				}
				obs, err := runAndRecordObservationWithDecorator(s, cfg, step.exp, step.scope, instName, step.samples, or(author, "agent:observer"), func(o *entity.Observation) {
					if o.Aux == nil {
						o.Aux = map[string]any{}
					}
					o.Aux[entity.ObservationAuxPair] = meta
				})
				if err != nil {
					return err
				}
				resp.Observations = append(resp.Observations, obs.ID)
				switch step.arm {
				case readmodel.PairArmCandidate:
					resp.CandidateObservations = append(resp.CandidateObservations, obs.ID)
				case readmodel.PairArmBaseline:
					resp.BaselineObservations = append(resp.BaselineObservations, obs.ID)
					switch step.segment {
					case readmodel.PairSegmentBefore:
						resp.BaselineBefore = append(resp.BaselineBefore, obs.ID)
					case readmodel.PairSegmentAfter:
						resp.BaselineAfter = append(resp.BaselineAfter, obs.ID)
					}
				}
			}
			if err := markExperimentMeasuredIfNeeded(s, candidateExp); err != nil {
				return err
			}
			if baselineExp.ID != candidateExp.ID {
				if err := markExperimentMeasuredIfNeeded(s, baselineExp); err != nil {
					return err
				}
			}
			if err := emitEvent(s, "observation.pair_record", or(author, "agent:observer"), pairID, observePairEventData(resp)); err != nil {
				return err
			}

			if w.IsJSON() {
				return w.JSON(resp)
			}
			renderObservePairText(w, resp)
			return nil
		},
	}
	c.Flags().StringVar(&baselineExpID, "baseline", "", "baseline experiment id to measure in the pair (required)")
	c.Flags().StringVar(&baselineRef, "baseline-ref", "", "reviewable git ref for a non-baseline baseline experiment")
	c.Flags().StringVar(&instName, "instrument", "", "registered instrument name (required)")
	c.Flags().IntVar(&samples, "samples", 0, "samples per arm; 0 uses the instrument default/min_samples")
	c.Flags().StringVar(&mode, "mode", observePairModeInterleave, "pairing mode: interleave or bracket")
	c.Flags().StringVar(&candidateRef, "candidate-ref", "", "reviewable git ref naming the clean candidate being measured (required for non-baseline experiments)")
	addAuthorFlag(c, &author, "")
	c.Flags().BoolVar(&force, "force", false, "bypass instrument dependency checks")
	c.Flags().BoolVar(&allowUnchanged, "allow-unchanged", false, "proceed even when the candidate branch has no commits above baseline")
	return []*cobra.Command{c}
}

func analyzePairCommands() []*cobra.Command {
	var iters int
	c := &cobra.Command{
		Use:   "analyze-pair <pair-id>",
		Short: "Analyze a paired baseline/candidate observation",
		Long: `Analyze a paired observation created by observe-pair. The command is
read-only and reports the primary candidate-vs-baseline delta plus drift
diagnostics such as baseline-before/after movement, monotonic drift, range
overlap, variance changes, and drift-to-effect size warnings.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			analysis, err := readmodel.AnalyzePairedObservation(s, args[0], iters)
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(analysis)
			}
			renderAnalyzePairText(w, analysis)
			return nil
		},
	}
	c.Flags().IntVar(&iters, "iters", 0, "bootstrap iterations (0 uses default 2000)")
	return []*cobra.Command{c}
}

func validateObservePairMode(mode string) error {
	switch mode {
	case observePairModeBracket, observePairModeInterleave:
		return nil
	default:
		return fmt.Errorf("--mode must be %q or %q, got %q", observePairModeInterleave, observePairModeBracket, mode)
	}
}

func ensureObservePairCandidateHasCommits(exp *entity.Experiment, instName string) error {
	if exp.IsBaseline || exp.Branch == "" || exp.Baseline.SHA == "" {
		return nil
	}
	hasCommits, err := worktree.HasCommitsAbove(globalProjectDir, exp.Branch, exp.Baseline.SHA)
	if err != nil || hasCommits {
		return err
	}
	return fmt.Errorf(
		"experiment %s branch %s has no commits above baseline %s — "+
			"the implementation may not have succeeded\n"+
			"  reset:   autoresearch experiment reset %s --reason \"...\"\n"+
			"  proceed: autoresearch observe-pair %s --baseline <baseline-exp> --instrument %s --candidate-ref <ref> --allow-unchanged",
		exp.ID, exp.Branch, exp.Baseline.SHA[:12], exp.ID, exp.ID, instName)
}

func buildObservePairSteps(
	mode string,
	samples int,
	candidateExp *entity.Experiment,
	candidateScope observeScope,
	baselineExp *entity.Experiment,
	baselineScope observeScope,
) []observePairStep {
	switch mode {
	case observePairModeBracket:
		return []observePairStep{
			{arm: readmodel.PairArmBaseline, segment: readmodel.PairSegmentBefore, exp: baselineExp, scope: baselineScope, samples: samples},
			{arm: readmodel.PairArmCandidate, segment: readmodel.PairSegmentCandidate, exp: candidateExp, scope: candidateScope, samples: samples},
			{arm: readmodel.PairArmBaseline, segment: readmodel.PairSegmentAfter, exp: baselineExp, scope: baselineScope, samples: samples},
		}
	default:
		steps := make([]observePairStep, 0, samples*2)
		for i := 0; i < samples; i++ {
			steps = append(steps,
				observePairStep{arm: readmodel.PairArmBaseline, segment: readmodel.PairSegmentInterleave, exp: baselineExp, scope: baselineScope, samples: 1},
				observePairStep{arm: readmodel.PairArmCandidate, segment: readmodel.PairSegmentInterleave, exp: candidateExp, scope: candidateScope, samples: 1},
			)
		}
		return steps
	}
}

func observePairBaselineRef(exp *entity.Experiment, scope observeScope) string {
	if strings.TrimSpace(scope.CandidateRef) != "" {
		return scope.CandidateRef
	}
	return strings.TrimSpace(exp.Baseline.Ref)
}

func observePairEventData(resp observePairResponse) map[string]any {
	data := map[string]any{
		"mode":                   resp.Mode,
		"instrument":             resp.Instrument,
		"samples_per_arm":        resp.SamplesPerArm,
		"candidate_experiment":   resp.CandidateExperiment,
		"baseline_experiment":    resp.BaselineExperiment,
		"observations":           resp.Observations,
		"candidate_observations": resp.CandidateObservations,
		"baseline_observations":  resp.BaselineObservations,
	}
	if resp.CandidateRef != "" {
		data["candidate_ref"] = resp.CandidateRef
	}
	if resp.CandidateSHA != "" {
		data["candidate_sha"] = resp.CandidateSHA
	}
	if resp.BaselineRef != "" {
		data["baseline_ref"] = resp.BaselineRef
	}
	if resp.BaselineSHA != "" {
		data["baseline_sha"] = resp.BaselineSHA
	}
	if len(resp.BaselineBefore) > 0 {
		data["baseline_before_observations"] = resp.BaselineBefore
	}
	if len(resp.BaselineAfter) > 0 {
		data["baseline_after_observations"] = resp.BaselineAfter
	}
	return data
}

func renderObservePairText(w *output.Writer, resp observePairResponse) {
	w.Textf("recorded paired observation %s\n", resp.PairID)
	w.Textf("  mode:        %s\n", resp.Mode)
	w.Textf("  instrument:  %s\n", resp.Instrument)
	w.Textf("  samples:     %d per arm\n", resp.SamplesPerArm)
	w.Textf("  candidate:   %s", resp.CandidateExperiment)
	if resp.CandidateRef != "" {
		w.Textf(" ref=%s", resp.CandidateRef)
	}
	if resp.CandidateSHA != "" {
		w.Textf(" sha=%s", shortSHA(resp.CandidateSHA))
	}
	w.Textln("")
	w.Textf("  baseline:    %s", resp.BaselineExperiment)
	if resp.BaselineRef != "" {
		w.Textf(" ref=%s", resp.BaselineRef)
	}
	if resp.BaselineSHA != "" {
		w.Textf(" sha=%s", shortSHA(resp.BaselineSHA))
	}
	w.Textln("")
	w.Textln("  observations:")
	for _, id := range resp.Observations {
		w.Textf("    - %s\n", id)
	}
}

func renderAnalyzePairText(w *output.Writer, analysis *readmodel.PairedObservationAnalysis) {
	w.Textf("pair: %s (%s)\n", analysis.PairID, analysis.Mode)
	w.Textf("instrument: %s\n", analysis.Instrument)
	w.Textf("candidate:  %s", analysis.CandidateExperiment)
	if analysis.CandidateRef != "" {
		w.Textf(" ref=%s", analysis.CandidateRef)
	}
	if analysis.CandidateSHA != "" {
		w.Textf(" sha=%s", shortSHA(analysis.CandidateSHA))
	}
	w.Textln("")
	w.Textf("baseline:   %s", analysis.BaselineExperiment)
	if analysis.BaselineRef != "" {
		w.Textf(" ref=%s", analysis.BaselineRef)
	}
	if analysis.BaselineSHA != "" {
		w.Textf(" sha=%s", shortSHA(analysis.BaselineSHA))
	}
	w.Textln("")
	w.Textf("candidate: n=%d  mean=%.6g  [%.6g, %.6g]  (stddev=%.4g)\n",
		analysis.Candidate.Summary.N, analysis.Candidate.Summary.Mean, analysis.Candidate.Summary.CILow, analysis.Candidate.Summary.CIHigh, analysis.Candidate.Summary.StdDev)
	w.Textf("baseline:  n=%d  mean=%.6g  [%.6g, %.6g]  (stddev=%.4g)\n",
		analysis.Baseline.Summary.N, analysis.Baseline.Summary.Mean, analysis.Baseline.Summary.CILow, analysis.Baseline.Summary.CIHigh, analysis.Baseline.Summary.StdDev)
	cmp := analysis.Comparison
	w.Textf("delta_abs:  %+.6g  95%% CI [%+.6g, %+.6g]\n", cmp.DeltaAbs, cmp.CILowAbs, cmp.CIHighAbs)
	w.Textf("delta_frac: %+.4f  95%% CI [%+.4f, %+.4f]\n", cmp.DeltaFrac, cmp.CILowFrac, cmp.CIHighFrac)
	w.Textf("mann-whitney U=%.1f  p=%.4f\n", cmp.UStat, cmp.PValue)
	if analysis.BaselineBefore != nil && analysis.BaselineAfter != nil {
		w.Textf("baseline drift: before=%.6g after=%.6g delta=%+.6g\n",
			analysis.BaselineBefore.Mean, analysis.BaselineAfter.Mean, analysis.Drift.BaselineDriftAbs)
	} else if analysis.Drift.BaselineDriftAbs != 0 {
		w.Textf("baseline drift: first-to-last delta=%+.6g\n", analysis.Drift.BaselineDriftAbs)
	}
	if analysis.Drift.MonotonicDrift != "" {
		w.Textf("monotonic drift: %s\n", analysis.Drift.MonotonicDrift)
	}
	w.Textf("range overlap: %t\n", analysis.Drift.RangeOverlap)
	if analysis.Drift.VarianceRatio != 0 {
		w.Textf("variance ratio: %.4g (%s)\n", analysis.Drift.VarianceRatio, analysis.Drift.VarianceChange)
	}
	for _, warning := range analysis.Warnings {
		w.Textf("warning: %s\n", warning)
	}
}
