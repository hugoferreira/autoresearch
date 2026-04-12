package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/stats"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func concludeCommands() []*cobra.Command {
	return []*cobra.Command{concludeCmd()}
}

func concludeCmd() *cobra.Command {
	var (
		verdict        string
		obsList        []string
		baselineExp    string
		interpretation string
		author         string
		reviewedBy     string
		iters          int
	)
	c := &cobra.Command{
		Use:   "conclude <hyp-id>",
		Short: "Draw a supported/refuted/inconclusive verdict from observations",
		Long: `Conclude a hypothesis. In strict mode (the default), the CLI computes a
bootstrap CI and Mann–Whitney p-value on the hypothesis's predicted
instrument and DOWNGRADES a requested "supported" to "inconclusive" if
the evidence doesn't justify it — if the CI crosses zero in the wrong
direction, or if |delta_frac| < hypothesis.predicts.min_effect. Every
downgrade is recorded in the conclusion's strict_check block and in
events.jsonl.

Observations are concatenated by instrument. The candidate experiment is
inferred from the observations; the baseline experiment is taken from
--baseline-experiment, or from the candidate's recorded baseline if set,
or omitted entirely (in which case "supported" is automatically
downgraded since there is no comparator).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			hypID := args[0]
			if verdict == "" {
				return errors.New("--verdict is required (supported|refuted|inconclusive)")
			}
			if len(obsList) == 0 {
				return errors.New("--observations is required (at least one observation id)")
			}
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			hyp, err := s.ReadHypothesis(hypID)
			if err != nil {
				return err
			}
			if hyp.Status == entity.StatusKilled {
				return fmt.Errorf("hypothesis %s is killed; reopen before concluding", hypID)
			}

			// Load observations, enforce they target the hypothesis's predicted instrument
			// and all belong to the same candidate experiment.
			var candObs []*entity.Observation
			candExp := ""
			for _, oid := range obsList {
				o, err := s.ReadObservation(strings.TrimSpace(oid))
				if err != nil {
					return err
				}
				if o.Instrument != hyp.Predicts.Instrument {
					return fmt.Errorf("observation %s uses instrument %q but hypothesis predicts on %q", o.ID, o.Instrument, hyp.Predicts.Instrument)
				}
				if candExp == "" {
					candExp = o.Experiment
				} else if candExp != o.Experiment {
					return fmt.Errorf("observations belong to different experiments (%s and %s); pass only observations from a single candidate", candExp, o.Experiment)
				}
				candObs = append(candObs, o)
			}

			// Resolve baseline experiment.
			if baselineExp == "" {
				candExpRec, err := s.ReadExperiment(candExp)
				if err != nil {
					return err
				}
				if candExpRec.Baseline.Experiment != "" {
					baselineExp = candExpRec.Baseline.Experiment
				}
			}

			var baseObs []*entity.Observation
			if baselineExp != "" {
				all, err := s.ListObservationsForExperiment(baselineExp)
				if err != nil {
					return err
				}
				for _, o := range all {
					if o.Instrument == hyp.Predicts.Instrument {
						baseObs = append(baseObs, o)
					}
				}
				if len(baseObs) == 0 {
					return fmt.Errorf("baseline experiment %s has no observations on instrument %q", baselineExp, hyp.Predicts.Instrument)
				}
			}

			// Compute comparison when a baseline exists.
			cSamples := flattenSamples(candObs)
			bSamples := flattenSamples(baseObs)
			var cmp *stats.Comparison
			if len(bSamples) > 0 {
				v := stats.CompareSamples(cSamples, bSamples, iters, 0)
				cmp = &v
			}

			// Apply strict firewall.
			decision := firewall.CheckStrictVerdict(verdict, hyp, cmp)

			// Build conclusion record.
			effect := entity.Effect{
				Instrument: hyp.Predicts.Instrument,
				NCandidate: len(cSamples),
				NBaseline:  len(bSamples),
			}
			if cmp != nil {
				effect.DeltaAbs = cmp.DeltaAbs
				effect.DeltaFrac = cmp.DeltaFrac
				effect.CILowAbs = cmp.CILowAbs
				effect.CIHighAbs = cmp.CIHighAbs
				effect.CILowFrac = cmp.CILowFrac
				effect.CIHighFrac = cmp.CIHighFrac
				effect.PValue = cmp.PValue
				effect.CIMethod = cmp.CIMethod
			}

			strictRec := entity.Strict{
				Passed:  decision.Passed,
				Reasons: decision.Reasons,
			}
			if decision.Downgraded {
				strictRec.RequestedFrom = verdict
			}

			concl := &entity.Conclusion{
				Hypothesis:   hypID,
				Verdict:      decision.FinalVerdict,
				Observations: obsList,
				CandidateExp: candExp,
				BaselineExp:  baselineExp,
				Effect:       effect,
				StatTest:     "mann_whitney_u",
				Strict:       strictRec,
				Author:       or(author, "agent:analyst"),
				ReviewedBy:   reviewedBy,
				CreatedAt:    nowUTC(),
				Body:         interpretationBody(interpretation, verdict, decision),
			}

			if err := dryRun(w, fmt.Sprintf("%sconclude %s with verdict=%s", downgradeLabel(decision), hypID, decision.FinalVerdict), map[string]any{"conclusion": concl}); err != nil {
				return err
			}

			id, err := s.AllocID(store.KindConclusion)
			if err != nil {
				return err
			}
			concl.ID = id
			if err := s.WriteConclusion(concl); err != nil {
				return err
			}

			// State transitions.
			if candExp != "" {
				if exp, err := s.ReadExperiment(candExp); err == nil {
					if exp.Status == entity.ExpMeasured {
						exp.Status = entity.ExpAnalyzed
						_ = s.WriteExperiment(exp)
					}
				}
			}
			// Back-reference: mark the baseline experiment as "done its job
			// as a comparator" so the dashboard stops showing it as in-flight.
			// The baseline's own status stays truthful — it was analyzed as a
			// baseline, not as a candidate for some hypothesis — but the
			// denormalized ReferencedAsBaselineBy list lets the dashboard
			// filter on "has been referenced" without joining against every
			// conclusion on every refresh. One baseline reused across N
			// candidates accumulates N entries here over time.
			if baselineExp != "" && baselineExp != candExp {
				if base, err := s.ReadExperiment(baselineExp); err == nil {
					if !slices.Contains(base.ReferencedAsBaselineBy, id) {
						base.ReferencedAsBaselineBy = append(base.ReferencedAsBaselineBy, id)
						_ = s.WriteExperiment(base)
					}
				}
			}
			hyp.Status = decision.FinalVerdict
			if err := s.WriteHypothesis(hyp); err != nil {
				return fmt.Errorf("update hypothesis status: %w", err)
			}

			// Event.
			eventData := map[string]any{
				"verdict":      decision.FinalVerdict,
				"requested":    verdict,
				"downgraded":   decision.Downgraded,
				"reasons":      decision.Reasons,
				"candidate":    candExp,
				"baseline":     baselineExp,
				"observations": obsList,
				"delta_frac":   effect.DeltaFrac,
				"ci_low_frac":  effect.CILowFrac,
				"ci_high_frac": effect.CIHighFrac,
			}
			kind := "conclusion.write"
			if decision.Downgraded {
				kind = "conclusion.downgrade"
			}
			if err := emitEvent(s, kind, or(author, "agent:analyst"), id, eventData); err != nil {
				return err
			}

			// Report.
			if w.IsJSON() {
				return w.JSON(map[string]any{
					"status":     "ok",
					"id":         id,
					"conclusion": concl,
					"decision":   decision,
				})
			}
			if decision.Downgraded {
				w.Textf("⚠ DOWNGRADED: requested %q → %q\n", verdict, decision.FinalVerdict)
				for _, r := range decision.Reasons {
					w.Textf("  - %s\n", r)
				}
				w.Textln("")
			}
			w.Textf("wrote %s\n", id)
			w.Textf("  hypothesis:  %s (now %s)\n", hypID, decision.FinalVerdict)
			w.Textf("  candidate:   %s  (n=%d)\n", candExp, effect.NCandidate)
			if baselineExp != "" {
				w.Textf("  baseline:    %s  (n=%d)\n", baselineExp, effect.NBaseline)
			}
			if cmp != nil {
				w.Textf("  delta_frac:  %+.4f  95%% CI [%+.4f, %+.4f]\n", effect.DeltaFrac, effect.CILowFrac, effect.CIHighFrac)
				w.Textf("  p-value:     %.4g  (%s)\n", effect.PValue, concl.StatTest)
			}
			if len(decision.Reasons) > 0 && !decision.Downgraded {
				w.Textln("  notes:")
				for _, r := range decision.Reasons {
					w.Textf("    - %s\n", r)
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&verdict, "verdict", "", "supported | refuted | inconclusive (required)")
	c.Flags().StringSliceVar(&obsList, "observations", nil, "comma-separated observation ids (required)")
	c.Flags().StringVar(&baselineExp, "baseline-experiment", "", "baseline experiment id (overrides candidate.baseline.experiment)")
	c.Flags().StringVar(&interpretation, "interpretation", "", "optional prose interpretation")
	addAuthorFlag(c, &author, "")
	c.Flags().StringVar(&reviewedBy, "reviewed-by", "", "critic or human who reviewed")
	c.Flags().IntVar(&iters, "iters", 0, "bootstrap iterations (0 uses default 2000)")
	return c
}

func interpretationBody(interp, requested string, d firewall.VerdictDecision) string {
	var sb strings.Builder
	sb.WriteString("# Interpretation\n\n")
	if interp != "" {
		sb.WriteString(interp)
		sb.WriteString("\n")
	} else {
		sb.WriteString("_No interpretation provided._\n")
	}
	if d.Downgraded {
		sb.WriteString("\n## Strict-mode downgrade\n\n")
		sb.WriteString(fmt.Sprintf("Requested verdict %q was downgraded to %q:\n\n", requested, d.FinalVerdict))
		for _, r := range d.Reasons {
			sb.WriteString("- ")
			sb.WriteString(r)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func downgradeLabel(d firewall.VerdictDecision) string {
	if d.Downgraded {
		return "(downgraded) "
	}
	return ""
}
