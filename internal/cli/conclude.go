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
	CandidateRef          string                       `json:"candidate_ref,omitempty"`
	CandidateSHA          string                       `json:"candidate_sha,omitempty"`
	CandidateSource       string                       `json:"candidate_source"`
	BaselineExperiment    string                       `json:"baseline_experiment,omitempty"`
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
baseline. If no usable comparator can be inferred, "supported" is
automatically downgraded since there is no comparison. CLI and JSON
output report which baseline source was used and any fallback note.`,
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

			requestedObsIDs, candObs, ignoredObs, candExp, candProv, err := resolveConcludeObservations(s, hyp, obsList)
			if err != nil {
				return err
			}
			candExpRec, err := s.ReadExperiment(candExp)
			if err != nil {
				return err
			}

			resolution := concludeResolution{
				RequestedObservations: requestedObsIDs,
				UsedObservations:      observationIDs(candObs),
				IgnoredObservations:   ignoredObs,
				CandidateExperiment:   candExp,
				CandidateRef:          candProv.Ref,
				CandidateSHA:          candProv.SHA,
				CandidateSource:       concludeCandidateSourceObservations,
				BaselineSource:        concludeBaselineSourceNone,
			}

			var (
				baseObs     []*entity.Observation
				baselineRes *readmodel.BaselineResolution
			)
			if baselineExp = strings.TrimSpace(baselineExp); baselineExp != "" {
				baseObs, err = baselineObservationsForExperiment(s, baselineExp, hyp.Predicts.Instrument)
				if err != nil {
					return err
				}
				baselineRes = &readmodel.BaselineResolution{
					ExperimentID: baselineExp,
					Source:       readmodel.BaselineSourceExplicit,
				}
			} else {
				baselineRes, err = readmodel.ResolveInferredBaseline(s, hyp, candExpRec, hyp.Predicts.Instrument)
				if err != nil {
					return err
				}
				if baselineRes != nil {
					baselineExp = baselineRes.ExperimentID
				}
				if baselineExp != "" {
					baseObs, err = baselineObservationsForExperiment(s, baselineExp, hyp.Predicts.Instrument)
					if err != nil {
						return err
					}
				}
			}
			applyBaselineResolution(&resolution, baselineRes, hyp.Predicts.Instrument)

			// Resolve incremental baseline: the frontier best within the same goal.
			var incrementalExp string
			var incrObs []*entity.Observation
			if goal != nil {
				concls, err := s.ListConclusions()
				if err != nil {
					return err
				}
				scopedConcls, err := conclusionsForGoal(s, hyp.GoalID, concls)
				if err != nil {
					return err
				}
				frontierRows, _ := readmodel.ComputeFrontier(s, goal, scopedConcls)
				if len(frontierRows) > 0 {
					best := frontierRows[0].Candidate
					// Only compute incremental if it differs from the absolute baseline.
					if best != baselineExp && best != candExp {
						incrementalExp = best
						all, err := s.ListObservationsForExperiment(best)
						if err == nil {
							for _, o := range all {
								if o.Instrument == hyp.Predicts.Instrument {
									incrObs = append(incrObs, o)
								}
							}
						}
					}
				}
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

			// Compute comparison against incremental baseline (frontier best).
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
				candAll, _ := s.ListObservationsForExperiment(candExp)
				var baseAll []*entity.Observation
				if baselineExp != "" {
					baseAll, _ = s.ListObservationsForExperiment(baselineExp)
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
				Hypothesis:      hypID,
				Verdict:         decision.FinalVerdict,
				Observations:    resolution.UsedObservations,
				CandidateExp:    candExp,
				CandidateRef:    candProv.Ref,
				CandidateSHA:    candProv.SHA,
				BaselineExp:     baselineExp,
				Effect:          effect,
				IncrementalExp:  incrementalExp,
				SecondaryChecks: decision.ClauseChecks,
				StatTest:        "mann_whitney_u",
				Strict:          strictRec,
				Author:          or(author, "agent:analyst"),
				ReviewedBy:      reviewedBy,
				CreatedAt:       nowUTC(),
				Body:            interpretationBody(interpretation, verdict, decision),
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
				hyp.Status = entity.StatusUnreviewed
			default:
				hyp.Status = decision.FinalVerdict
			}
			if err := s.WriteHypothesis(hyp); err != nil {
				return fmt.Errorf("update hypothesis status: %w", err)
			}

			// Event.
			eventData := map[string]any{
				"verdict":          decision.FinalVerdict,
				"requested":        verdict,
				"downgraded":       decision.Downgraded,
				"reasons":          decision.Reasons,
				"candidate":        candExp,
				"candidate_ref":    candProv.Ref,
				"candidate_sha":    candProv.SHA,
				"candidate_source": resolution.CandidateSource,
				"baseline":         baselineExp,
				"baseline_source":  resolution.BaselineSource,
				"observations":     resolution.UsedObservations,
				"delta_frac":       effect.DeltaFrac,
				"ci_low_frac":      effect.CILowFrac,
				"ci_high_frac":     effect.CIHighFrac,
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

			// Report.
			if w.IsJSON() {
				return w.JSON(map[string]any{
					"status":     "ok",
					"id":         id,
					"conclusion": concl,
					"decision":   decision,
					"resolution": resolution,
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
			w.Textf("wrote %s\n", id)
			w.Textf("  hypothesis:  %s (now %s)\n", hypID, hyp.Status)
			w.Textf("  candidate:   %s  (source=%s, n=%d)\n", candExp, resolution.CandidateSource, effect.NCandidate)
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
				w.Textf("  incremental: %s  delta_frac=%+.4f  CI [%+.4f, %+.4f]  (vs frontier best)\n",
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
	c.Flags().StringVar(&baselineExp, "baseline-experiment", "", "baseline experiment id (strict override; no fallback if it lacks the predicted instrument)")
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

func resolveConcludeObservations(s *store.Store, hyp *entity.Hypothesis, rawIDs []string) ([]string, []*entity.Observation, []concludeIgnoredObservation, string, observationProvenance, error) {
	requested := normalizeObservationIDs(rawIDs)
	if len(requested) == 0 {
		return nil, nil, nil, "", observationProvenance{}, errors.New("--observations is required (at least one observation id)")
	}

	var (
		used     []*entity.Observation
		ignored  []concludeIgnoredObservation
		candExp  string
		candProv observationProvenance
	)
	for _, oid := range requested {
		o, err := s.ReadObservation(oid)
		if err != nil {
			return nil, nil, nil, "", observationProvenance{}, err
		}
		if o.Instrument != hyp.Predicts.Instrument {
			ignored = append(ignored, concludeIgnoredObservation{
				ID:         o.ID,
				Instrument: o.Instrument,
				Reason:     fmt.Sprintf("instrument %q does not match predicted instrument %q", o.Instrument, hyp.Predicts.Instrument),
			})
			continue
		}
		if candExp == "" {
			candExp = o.Experiment
			candProv = observationProvenanceFromObservation(o)
		} else if candExp != o.Experiment {
			return nil, nil, nil, "", observationProvenance{}, fmt.Errorf("observations belong to different experiments (%s and %s); pass only observations from a single candidate", candExp, o.Experiment)
		}
		if err := validateObservationProvenance(candProv, o); err != nil {
			return nil, nil, nil, "", observationProvenance{}, err
		}
		used = append(used, o)
	}
	if len(used) == 0 {
		var ignoredIDs []string
		for _, ignoredObs := range ignored {
			ignoredIDs = append(ignoredIDs, ignoredObs.ID)
		}
		if len(ignoredIDs) > 0 {
			return nil, nil, nil, "", observationProvenance{}, fmt.Errorf("none of the requested observations use predicted instrument %q; ignored: %s", hyp.Predicts.Instrument, strings.Join(ignoredIDs, ", "))
		}
		return nil, nil, nil, "", observationProvenance{}, fmt.Errorf("none of the requested observations use predicted instrument %q", hyp.Predicts.Instrument)
	}
	return requested, used, ignored, candExp, candProv, nil
}

func observationProvenanceFromObservation(o *entity.Observation) observationProvenance {
	if o == nil {
		return observationProvenance{}
	}
	return observationProvenance{
		Ref: strings.TrimSpace(o.CandidateRef),
		SHA: strings.TrimSpace(o.CandidateSHA),
	}
}

func validateObservationProvenance(expected observationProvenance, o *entity.Observation) error {
	actual := observationProvenanceFromObservation(o)
	if actual == expected {
		return nil
	}
	return fmt.Errorf(
		"observations mix candidate provenance: expected %s, got %s on %s",
		formatSingleObservationProvenance(expected),
		formatSingleObservationProvenance(actual),
		o.ID,
	)
}

func formatSingleObservationProvenance(p observationProvenance) string {
	switch {
	case p.Ref != "" && p.SHA != "":
		return fmt.Sprintf("%s@%s", p.Ref, shortSHA(p.SHA))
	case p.Ref != "":
		return p.Ref
	case p.SHA != "":
		return shortSHA(p.SHA)
	default:
		return "(legacy)"
	}
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

func baselineObservationsForExperiment(s *store.Store, expID, instrument string) ([]*entity.Observation, error) {
	if expID == "" {
		return nil, nil
	}
	if _, err := s.ReadExperiment(expID); err != nil {
		return nil, fmt.Errorf("baseline experiment %s: %w", expID, err)
	}
	all, err := s.ListObservationsForExperiment(expID)
	if err != nil {
		return nil, err
	}
	var filtered []*entity.Observation
	for _, o := range all {
		if o.Instrument == instrument {
			filtered = append(filtered, o)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("baseline experiment %s has no observations on instrument %q", expID, instrument)
	}
	return filtered, nil
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

func conclusionsForGoal(s *store.Store, goalID string, concls []*entity.Conclusion) ([]*entity.Conclusion, error) {
	if goalID == "" {
		return nil, nil
	}
	goalByHypothesis := map[string]string{}
	out := make([]*entity.Conclusion, 0, len(concls))
	for _, c := range concls {
		if c == nil || c.Hypothesis == "" {
			continue
		}
		gid, ok := goalByHypothesis[c.Hypothesis]
		if !ok {
			h, err := s.ReadHypothesis(c.Hypothesis)
			if err != nil {
				return nil, err
			}
			gid = h.GoalID
			goalByHypothesis[c.Hypothesis] = gid
		}
		if gid == goalID {
			out = append(out, c)
		}
	}
	return out, nil
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
