package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/stats"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

const (
	concludeCandidateSourceObservations = "observations"
	concludeBaselineSourceNone          = "none"
	concludeBaselineAuto                = "auto"
)

type concludeIgnoredObservation struct {
	ID         string `json:"id"`
	Instrument string `json:"instrument,omitempty"`
	Reason     string `json:"reason"`
}

type concludeResolution struct {
	RequestedObservations []string                     `json:"requested_observations"`
	UsedObservations      []string                     `json:"used_observations"`
	IgnoredObservations   []concludeIgnoredObservation `json:"ignored_observations,omitempty"`
	CandidateExperiment   string                       `json:"candidate_experiment"`
	CandidateAttempt      int                          `json:"candidate_attempt,omitempty"`
	CandidateRef          string                       `json:"candidate_ref,omitempty"`
	CandidateSHA          string                       `json:"candidate_sha,omitempty"`
	CandidateSource       string                       `json:"candidate_source"`
	BaselineExperiment    string                       `json:"baseline_experiment,omitempty"`
	BaselineAttempt       int                          `json:"baseline_attempt,omitempty"`
	BaselineRef           string                       `json:"baseline_ref,omitempty"`
	BaselineSHA           string                       `json:"baseline_sha,omitempty"`
	BaselineSource        string                       `json:"baseline_source"`
	BaselineNote          string                       `json:"baseline_note,omitempty"`
	AncestorHypothesis    string                       `json:"ancestor_hypothesis,omitempty"`
	AncestorConclusion    string                       `json:"ancestor_conclusion,omitempty"`
	IncrementalExperiment string                       `json:"incremental_experiment,omitempty"`
}

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
inferred from the observations. Requested observation ids on other
instruments are ignored and reported in the CLI output; the remaining
predicted-instrument observations must all belong to the same candidate
experiment.

The absolute baseline is resolved in this order: explicit
--baseline-experiment (strict; no fallback), the candidate's recorded
baseline if it has matching instrument data, the nearest accepted
supported ancestor conclusion candidate, then the current goal's mapped
baseline. The explicit value "auto" resolves to that nearest accepted
supported lineage predecessor. If no usable comparator can be inferred, "supported" is
automatically downgraded since there is no comparison. CLI and JSON
output report which baseline source was used, the automatic incremental
lineage predecessor, and any fallback note.`,
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

			goal, err := goalForHypothesis(s, hyp)
			if err != nil {
				return err
			}

			requestedObsIDs, candObs, ignoredObs, candScope, err := resolveConcludeObservations(s, hyp, obsList)
			if err != nil {
				return err
			}
			candExp := candScope.Experiment
			candExpRec, err := s.ReadExperiment(candExp)
			if err != nil {
				return err
			}
			obsIdx, err := readmodel.LoadObservationIndexStrict(s)
			if err != nil {
				return err
			}

			resolution := concludeResolution{
				RequestedObservations: requestedObsIDs,
				UsedObservations:      observationIDs(candObs),
				IgnoredObservations:   ignoredObs,
				CandidateExperiment:   candExp,
				CandidateAttempt:      candScope.Attempt,
				CandidateRef:          candScope.Ref,
				CandidateSHA:          candScope.SHA,
				CandidateSource:       concludeCandidateSourceObservations,
				BaselineSource:        concludeBaselineSourceNone,
			}

			var (
				baseObs     []*entity.Observation
				baselineRes *readmodel.BaselineResolution
			)
			switch baselineExp = strings.TrimSpace(baselineExp); baselineExp {
			case "":
				baselineRes, err = readmodel.ResolveInferredBaselineWithIndex(s, obsIdx, hyp, candExpRec, hyp.Predicts.Instrument)
				if err != nil {
					return err
				}
				if baselineRes != nil {
					baselineExp = baselineRes.ExperimentID
				}
				if baselineExp != "" {
					baseObs = filterObservationsByInstrument(obsIdx.ObservationsForScope(baselineRes.Scope()), hyp.Predicts.Instrument)
				}
			case concludeBaselineAuto:
				baselineRes, err = readmodel.ResolveLineageSupportedBaselineWithIndex(s, obsIdx, hyp, hyp.Predicts.Instrument)
				if err != nil {
					return err
				}
				if baselineRes == nil || baselineRes.ExperimentID == "" {
					note := "no usable lineage baseline"
					if baselineRes != nil && baselineRes.Note != "" {
						note = baselineRes.Note
					}
					return fmt.Errorf("--baseline-experiment auto could not resolve a supported ancestor for %s: %s", hypID, note)
				}
				baselineExp = baselineRes.ExperimentID
				baseObs = filterObservationsByInstrument(obsIdx.ObservationsForScope(baselineRes.Scope()), hyp.Predicts.Instrument)
			default:
				baseObs, baselineRes, err = baselineObservationsForExperiment(s, obsIdx, baselineExp, hyp.Predicts.Instrument)
				if err != nil {
					return err
				}
			}
			applyBaselineResolution(&resolution, baselineRes, hyp.Predicts.Instrument)

			// Resolve incremental baseline: the accepted supported predecessor
			// on this hypothesis lineage.
			var incrementalExp string
			var incrObs []*entity.Observation
			incrementalRes, err := readmodel.ResolveLineageSupportedBaselineWithIndex(s, obsIdx, hyp, hyp.Predicts.Instrument)
			if err != nil {
				return err
			}
			if incrementalRes != nil && incrementalRes.ExperimentID != "" && incrementalRes.Scope() != candScope {
				incrementalExp = incrementalRes.ExperimentID
				incrObs = filterObservationsByInstrument(obsIdx.ObservationsForScope(incrementalRes.Scope()), hyp.Predicts.Instrument)
			}
			if incrementalExp != "" {
				resolution.IncrementalExperiment = incrementalExp
			}

			// Compute comparison against absolute baseline.
			cSamples := flattenSamples(candObs)
			bSamples := flattenSamples(baseObs)
			var cmp *stats.Comparison
			if len(bSamples) > 0 {
				v := stats.CompareSamples(cSamples, bSamples, iters, 0)
				cmp = &v
			}

			// Compute comparison against incremental baseline (lineage predecessor).
			var incrCmp *stats.Comparison
			if len(incrObs) > 0 {
				incrSamples := flattenSamples(incrObs)
				if len(incrSamples) > 0 {
					v := stats.CompareSamples(cSamples, incrSamples, iters, 0)
					incrCmp = &v
				}
			}

			// Apply strict firewall (always against absolute baseline). When an
			// active goal declares rescuers, build a callback that lets the
			// firewall compute the same-baseline comparison on any rescuer
			// instrument; the firewall uses it to decide whether a failing
			// primary can be rescued by a goal-level secondary.
			strictCtx := firewall.StrictContext{}
			if goal != nil && len(goal.Rescuers) > 0 && goal.NeutralBandFrac > 0 {
				candAll := obsIdx.ObservationsForScope(candScope)
				var baseAll []*entity.Observation
				if baselineRes != nil {
					baseAll = obsIdx.ObservationsForScope(baselineRes.Scope())
				}
				strictCtx = firewall.StrictContext{
					Goal: goal,
					RescuerComparison: func(instrument string) (*stats.Comparison, string) {
						cs := samplesForInstrument(candAll, instrument)
						if len(cs) == 0 {
							return nil, fmt.Sprintf("no observations on %q for candidate %s", instrument, candExp)
						}
						bs := samplesForInstrument(baseAll, instrument)
						if len(bs) == 0 {
							return nil, fmt.Sprintf("no observations on %q for baseline %s", instrument, baselineExp)
						}
						v := stats.CompareSamples(cs, bs, iters, 0)
						return &v, ""
					},
				}
			}
			decision := firewall.CheckStrictVerdictWithContext(verdict, hyp, cmp, strictCtx)

			// Build conclusion record.
			effect := buildEffect(hyp.Predicts.Instrument, cSamples, bSamples, cmp)

			strictRec := entity.Strict{
				Passed:      decision.Passed,
				RescuedBy:   decision.RescuedBy,
				Directional: hyp.Predicts.MinEffect == 0,
				Reasons:     decision.Reasons,
			}
			if decision.Downgraded {
				strictRec.RequestedFrom = verdict
			}

			concl := &entity.Conclusion{
				Hypothesis:       hypID,
				Verdict:          decision.FinalVerdict,
				Observations:     resolution.UsedObservations,
				CandidateExp:     candExp,
				CandidateAttempt: candScope.Attempt,
				CandidateRef:     candScope.Ref,
				CandidateSHA:     candScope.SHA,
				BaselineExp:      baselineExp,
				BaselineAttempt:  resolution.BaselineAttempt,
				BaselineRef:      resolution.BaselineRef,
				BaselineSHA:      resolution.BaselineSHA,
				Effect:           effect,
				IncrementalExp:   incrementalExp,
				SecondaryChecks:  decision.ClauseChecks,
				StatTest:         "mann_whitney_u",
				Strict:           strictRec,
				Author:           or(author, "agent:analyst"),
				ReviewedBy:       reviewedBy,
				CreatedAt:        nowUTC(),
				Body:             interpretationBody(interpretation, verdict, decision),
			}
			if incrCmp != nil {
				incrEffect := buildEffect(hyp.Predicts.Instrument, cSamples, flattenSamples(incrObs), incrCmp)
				concl.IncrementalEffect = &incrEffect
			}

			if err := dryRun(w, fmt.Sprintf("%sconclude %s with verdict=%s", downgradeLabel(decision), hypID, decision.FinalVerdict), map[string]any{
				"conclusion": concl,
				"resolution": resolution,
			}); err != nil {
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
			switch decision.FinalVerdict {
			case entity.VerdictSupported, entity.VerdictRefuted:
				if strings.TrimSpace(reviewedBy) != "" {
					hyp.Status = decision.FinalVerdict
				} else {
					hyp.Status = entity.StatusUnreviewed
				}
			default:
				hyp.Status = decision.FinalVerdict
			}
			if err := s.WriteHypothesis(hyp); err != nil {
				return fmt.Errorf("update hypothesis status: %w", err)
			}

			// Event.
			eventData := map[string]any{
				"verdict":           decision.FinalVerdict,
				"requested":         verdict,
				"downgraded":        decision.Downgraded,
				"reasons":           decision.Reasons,
				"candidate":         candExp,
				"candidate_attempt": candScope.Attempt,
				"candidate_ref":     candScope.Ref,
				"candidate_sha":     candScope.SHA,
				"candidate_source":  resolution.CandidateSource,
				"baseline":          baselineExp,
				"baseline_source":   resolution.BaselineSource,
				"observations":      resolution.UsedObservations,
				"delta_frac":        effect.DeltaFrac,
				"ci_low_frac":       effect.CILowFrac,
				"ci_high_frac":      effect.CIHighFrac,
			}
			if !slices.Equal(resolution.RequestedObservations, resolution.UsedObservations) {
				eventData["requested_observations"] = resolution.RequestedObservations
			}
			if len(resolution.IgnoredObservations) > 0 {
				eventData["ignored_observations"] = resolution.IgnoredObservations
			}
			if resolution.BaselineNote != "" {
				eventData["baseline_note"] = resolution.BaselineNote
			}
			if resolution.BaselineAttempt > 0 {
				eventData["baseline_attempt"] = resolution.BaselineAttempt
			}
			if resolution.BaselineRef != "" {
				eventData["baseline_ref"] = resolution.BaselineRef
			}
			if resolution.BaselineSHA != "" {
				eventData["baseline_sha"] = resolution.BaselineSHA
			}
			if resolution.AncestorHypothesis != "" {
				eventData["ancestor_hypothesis"] = resolution.AncestorHypothesis
			}
			if resolution.AncestorConclusion != "" {
				eventData["ancestor_conclusion"] = resolution.AncestorConclusion
			}
			if incrementalExp != "" {
				eventData["incremental_experiment"] = incrementalExp
				if concl.IncrementalEffect != nil {
					eventData["incremental_delta_frac"] = concl.IncrementalEffect.DeltaFrac
				}
			}
			if decision.RescuedBy != "" {
				eventData["rescued_by"] = decision.RescuedBy
			}
			if strings.TrimSpace(reviewedBy) != "" {
				eventData["reviewed_by"] = reviewedBy
			}
			if strictRec.Directional {
				eventData["directional"] = true
			}
			kind := "conclusion.write"
			if decision.Downgraded {
				kind = "conclusion.downgrade"
			}
			if err := emitEvent(s, kind, or(author, "agent:analyst"), id, eventData); err != nil {
				return err
			}
			lintReport, err := readmodel.LintConclusion(s, id)
			if err != nil {
				return fmt.Errorf("lint conclusion: %w", err)
			}

			// Report.
			if w.IsJSON() {
				return w.JSON(map[string]any{
					"status":     "ok",
					"id":         id,
					"conclusion": concl,
					"decision":   decision,
					"resolution": resolution,
					"lint":       lintReport,
				})
			}
			if decision.Downgraded {
				w.Textf("⚠ DOWNGRADED: requested %q → %q\n", verdict, decision.FinalVerdict)
				for _, r := range decision.Reasons {
					w.Textf("  - %s\n", r)
				}
				w.Textln("")
			} else if decision.RescuedBy != "" {
				w.Textf("⚕ RESCUED: primary was neutral; verdict supported by %s\n", decision.RescuedBy)
				for _, r := range decision.Reasons {
					w.Textf("  - %s\n", r)
				}
				w.Textln("")
			}
			renderConclusionLintWarningText(w, lintReport)
			w.Textf("wrote %s\n", id)
			w.Textf("  hypothesis:  %s (now %s)\n", hypID, hyp.Status)
			w.Textf("  candidate:   %s  (source=%s, n=%d)\n", candExp, resolution.CandidateSource, effect.NCandidate)
			if resolution.CandidateAttempt > 0 {
				w.Textf("  candidate attempt: %d\n", resolution.CandidateAttempt)
			}
			if resolution.CandidateRef != "" {
				w.Textf("  candidate ref: %s\n", resolution.CandidateRef)
			}
			if resolution.CandidateSHA != "" {
				w.Textf("  candidate sha: %s\n", resolution.CandidateSHA)
			}
			w.Textf("  observations: %s\n", strings.Join(resolution.UsedObservations, ", "))
			if !slices.Equal(resolution.RequestedObservations, resolution.UsedObservations) {
				w.Textf("  requested:   %s\n", strings.Join(resolution.RequestedObservations, ", "))
			}
			for i, ignored := range resolution.IgnoredObservations {
				label := "  ignored:     "
				if i > 0 {
					label = "               "
				}
				w.Textf("%s%s (%s)\n", label, ignored.ID, ignored.Reason)
			}
			if baselineExp != "" {
				w.Textf("  baseline:    %s  (n=%d, source=%s)\n", baselineExp, effect.NBaseline, formatBaselineSource(resolution))
				if resolution.BaselineAttempt > 0 {
					w.Textf("  baseline attempt: %d\n", resolution.BaselineAttempt)
				}
				if resolution.BaselineRef != "" {
					w.Textf("  baseline ref: %s\n", resolution.BaselineRef)
				}
				if resolution.BaselineSHA != "" {
					w.Textf("  baseline sha: %s\n", resolution.BaselineSHA)
				}
			} else {
				w.Textf("  baseline:    none  (source=%s)\n", resolution.BaselineSource)
			}
			if resolution.BaselineNote != "" {
				w.Textf("  baseline note: %s\n", resolution.BaselineNote)
			}
			if cmp != nil {
				w.Textf("  delta_frac:  %+.4f  95%% CI [%+.4f, %+.4f]  (vs absolute baseline)\n", effect.DeltaFrac, effect.CILowFrac, effect.CIHighFrac)
				w.Textf("  p-value:     %.4g  (%s)\n", effect.PValue, concl.StatTest)
			}
			if concl.IncrementalEffect != nil {
				ie := concl.IncrementalEffect
				w.Textf("  incremental: %s  delta_frac=%+.4f  CI [%+.4f, %+.4f]  (vs lineage predecessor)\n",
					incrementalExp, ie.DeltaFrac, ie.CILowFrac, ie.CIHighFrac)
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
	c.Flags().StringVar(&baselineExp, "baseline-experiment", "", "baseline experiment id, or 'auto' for the supported lineage predecessor (strict override; no fallback if unusable)")
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
	if d.RescuedBy != "" {
		return "(rescued) "
	}
	return ""
}

func goalForHypothesis(s *store.Store, hyp *entity.Hypothesis) (*entity.Goal, error) {
	if s == nil || hyp == nil || strings.TrimSpace(hyp.GoalID) == "" {
		return nil, nil
	}
	goal, err := s.ReadGoal(hyp.GoalID)
	if errors.Is(err, store.ErrGoalNotFound) {
		return nil, nil
	}
	return goal, err
}

func resolveConcludeObservations(s *store.Store, hyp *entity.Hypothesis, rawIDs []string) ([]string, []*entity.Observation, []concludeIgnoredObservation, readmodel.ObservationScope, error) {
	requested := normalizeObservationIDs(rawIDs)
	if len(requested) == 0 {
		return nil, nil, nil, readmodel.ObservationScope{}, errors.New("--observations is required (at least one observation id)")
	}

	var (
		used      []*entity.Observation
		ignored   []concludeIgnoredObservation
		candScope readmodel.ObservationScope
	)
	for _, oid := range requested {
		o, err := s.ReadObservation(oid)
		if err != nil {
			return nil, nil, nil, readmodel.ObservationScope{}, err
		}
		if o.Instrument != hyp.Predicts.Instrument {
			ignored = append(ignored, concludeIgnoredObservation{
				ID:         o.ID,
				Instrument: o.Instrument,
				Reason:     fmt.Sprintf("instrument %q does not match predicted instrument %q", o.Instrument, hyp.Predicts.Instrument),
			})
			continue
		}
		scope := readmodel.ObservationScopeFromObservation(o)
		if candScope.Experiment == "" {
			candScope = scope
		} else if candScope.Experiment != scope.Experiment {
			return nil, nil, nil, readmodel.ObservationScope{}, fmt.Errorf("observations belong to different experiments (%s and %s); pass only observations from a single candidate", candScope.Experiment, scope.Experiment)
		}
		if err := validateObservationScope(candScope, o); err != nil {
			return nil, nil, nil, readmodel.ObservationScope{}, err
		}
		used = append(used, o)
	}
	if len(used) == 0 {
		var ignoredIDs []string
		for _, ignoredObs := range ignored {
			ignoredIDs = append(ignoredIDs, ignoredObs.ID)
		}
		if len(ignoredIDs) > 0 {
			return nil, nil, nil, readmodel.ObservationScope{}, fmt.Errorf("none of the requested observations use predicted instrument %q; ignored: %s", hyp.Predicts.Instrument, strings.Join(ignoredIDs, ", "))
		}
		return nil, nil, nil, readmodel.ObservationScope{}, fmt.Errorf("none of the requested observations use predicted instrument %q", hyp.Predicts.Instrument)
	}
	return requested, used, ignored, candScope, nil
}

func validateObservationScope(expected readmodel.ObservationScope, o *entity.Observation) error {
	actual := readmodel.ObservationScopeFromObservation(o)
	if actual == expected {
		return nil
	}
	return fmt.Errorf(
		"observations mix candidate scope: expected %s, got %s on %s",
		readmodel.FormatObservationScope(expected),
		readmodel.FormatObservationScope(actual),
		o.ID,
	)
}

func normalizeObservationIDs(rawIDs []string) []string {
	var ids []string
	for _, raw := range rawIDs {
		if id := strings.TrimSpace(raw); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func observationIDs(obs []*entity.Observation) []string {
	ids := make([]string, 0, len(obs))
	for _, o := range obs {
		if o != nil {
			ids = append(ids, o.ID)
		}
	}
	return ids
}

func baselineObservationsForExperiment(s *store.Store, obs *readmodel.ObservationIndex, expID, instrument string) ([]*entity.Observation, *readmodel.BaselineResolution, error) {
	if expID == "" {
		return nil, nil, nil
	}
	scope, ok, note, err := readmodel.ResolveExperimentInstrumentScope(s, obs, expID, instrument, "baseline experiment")
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, fmt.Errorf("%s", note)
	}
	filtered := filterObservationsByInstrument(obs.ObservationsForScope(scope), instrument)
	return filtered, &readmodel.BaselineResolution{
		ExperimentID: expID,
		Attempt:      scope.Attempt,
		Ref:          scope.Ref,
		SHA:          scope.SHA,
		Source:       readmodel.BaselineSourceExplicit,
	}, nil
}

func applyBaselineResolution(res *concludeResolution, baseline *readmodel.BaselineResolution, instrument string) {
	if res == nil {
		return
	}
	res.BaselineSource = concludeBaselineSourceNone
	if baseline == nil {
		res.BaselineNote = fmt.Sprintf("no usable baseline could be inferred for instrument %q", instrument)
		return
	}
	res.BaselineExperiment = baseline.ExperimentID
	res.BaselineAttempt = baseline.Attempt
	res.BaselineRef = baseline.Ref
	res.BaselineSHA = baseline.SHA
	if baseline.Source != "" {
		res.BaselineSource = baseline.Source
	}
	res.BaselineNote = baseline.Note
	res.AncestorHypothesis = baseline.AncestorHypothesis
	res.AncestorConclusion = baseline.AncestorConclusion
	if res.BaselineExperiment == "" && res.BaselineNote == "" {
		res.BaselineNote = fmt.Sprintf("no usable baseline could be inferred for instrument %q", instrument)
	}
}

func formatBaselineSource(res concludeResolution) string {
	if res.BaselineSource == readmodel.BaselineSourceAncestorSupported &&
		res.AncestorHypothesis != "" &&
		res.AncestorConclusion != "" {
		return fmt.Sprintf("%s via %s/%s", res.BaselineSource, res.AncestorHypothesis, res.AncestorConclusion)
	}
	return res.BaselineSource
}

func filterObservationsByInstrument(obs []*entity.Observation, instrument string) []*entity.Observation {
	var filtered []*entity.Observation
	for _, o := range obs {
		if o != nil && o.Instrument == instrument {
			filtered = append(filtered, o)
		}
	}
	return filtered
}

// samplesForInstrument flattens every per-sample slice across observations
// on the given instrument into a single float slice. Used to assemble the
// candidate/baseline sample pair for a goal rescuer at conclude time.
func samplesForInstrument(obs []*entity.Observation, instrument string) []float64 {
	var filtered []*entity.Observation
	for _, o := range obs {
		if o.Instrument == instrument {
			filtered = append(filtered, o)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return flattenSamples(filtered)
}

// buildEffect constructs an Effect from samples and an optional comparison.
func buildEffect(instrument string, candSamples, baseSamples []float64, cmp *stats.Comparison) entity.Effect {
	e := entity.Effect{
		Instrument: instrument,
		NCandidate: len(candSamples),
		NBaseline:  len(baseSamples),
	}
	if cmp != nil {
		e.DeltaAbs = cmp.DeltaAbs
		e.DeltaFrac = cmp.DeltaFrac
		e.CILowAbs = cmp.CILowAbs
		e.CIHighAbs = cmp.CIHighAbs
		e.CILowFrac = cmp.CILowFrac
		e.CIHighFrac = cmp.CIHighFrac
		e.PValue = cmp.PValue
		e.CIMethod = cmp.CIMethod
	}
	return e
}
