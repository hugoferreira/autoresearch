package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/stats"
	"github.com/bytter/autoresearch/internal/worktree"
	"github.com/spf13/cobra"
)

func analyzeCommands() []*cobra.Command {
	return []*cobra.Command{analyzeCmd()}
}

func analyzeCmd() *cobra.Command {
	var (
		baselineExp  string
		instName     string
		iters        int
		candidateRef string
	)
	c := &cobra.Command{
		Use:   "analyze <exp-id>",
		Short: "Compute per-instrument stats for an experiment (optionally vs a baseline)",
		Long: `Summarize observations attached to an experiment. For each instrument,
reports sample count, mean, BCa 95% CI, stddev, min/max.

If --baseline is provided, also compares each instrument's samples against
the baseline experiment's samples for the same instrument using percentile
bootstrap (for the delta CI) and Mann–Whitney U (for p-value).

analyze is read-only: no store writes, no state transitions. Use conclude
to persist a verdict.

For non-baseline experiments that have observations on multiple candidate
refs, pass --candidate-ref to analyze the specific measured candidate.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			expID := args[0]

			s, err := openStore()
			if err != nil {
				return err
			}
			exp, err := s.ReadExperiment(expID)
			if err != nil {
				return err
			}

			candObs, err := s.ListObservationsForExperiment(expID)
			if err != nil {
				return err
			}
			if len(candObs) == 0 {
				return fmt.Errorf("experiment %s has no observations", expID)
			}
			if !exp.IsBaseline {
				if strings.TrimSpace(candidateRef) != "" {
					ref, err := normalizeAnalyzeCandidateRef(exp, candidateRef)
					if err != nil {
						return err
					}
					candObs, err = filterAnalyzeObservationsByCandidateRef(candObs, ref)
					if err != nil {
						return fmt.Errorf("experiment %s: %w", expID, err)
					}
				} else {
					if provs := distinctObservationProvenances(candObs); len(provs) > 1 {
						return fmt.Errorf(
							"experiment %s has observations for multiple candidate refs/shas (%s); rerun analyze with --candidate-ref <ref>",
							expID, formatObservationProvenances(provs),
						)
					}
				}
			}

			// Optional baseline.
			var baseObs []*entity.Observation
			if baselineExp != "" {
				baseObs, err = s.ListObservationsForExperiment(baselineExp)
				if err != nil {
					return err
				}
				if len(baseObs) == 0 {
					return fmt.Errorf("baseline experiment %s has no observations", baselineExp)
				}
			}

			// Bucket by instrument.
			candBy := groupByInstrument(candObs)
			baseBy := groupByInstrument(baseObs)

			instruments := sortedKeys(candBy)
			if instName != "" {
				instruments = []string{instName}
			}

			limits := map[string]any{"iters": iters}
			if iters == 0 {
				limits["iters"] = stats.DefaultIterations
			}

			type instRow struct {
				Instrument string            `json:"instrument"`
				Candidate  stats.Summary     `json:"candidate"`
				Baseline   *stats.Summary    `json:"baseline,omitempty"`
				Comparison *stats.Comparison `json:"comparison,omitempty"`
			}
			var rows []instRow
			for _, name := range instruments {
				cs := flattenSamples(candBy[name])
				if len(cs) == 0 {
					continue
				}
				summary := stats.Summarize(cs, iters, 0)
				row := instRow{Instrument: name, Candidate: summary}
				if bs := flattenSamples(baseBy[name]); len(bs) > 0 {
					baseSum := stats.Summarize(bs, iters, 0)
					row.Baseline = &baseSum
					cmp := stats.CompareSamples(cs, bs, iters, 0)
					row.Comparison = &cmp
				}
				rows = append(rows, row)
			}

			if w.IsJSON() {
				return w.JSON(map[string]any{
					"experiment": expID,
					"baseline":   baselineExp,
					"limits":     limits,
					"rows":       rows,
				})
			}
			printLimits(w, limits)
			w.Textf("[experiment: %s", expID)
			if baselineExp != "" {
				w.Textf(", baseline: %s", baselineExp)
			}
			w.Textln("]")
			if len(rows) == 0 {
				w.Textln("(no instruments to summarize)")
				return nil
			}
			for _, r := range rows {
				w.Textln("")
				w.Textf("instrument: %s\n", r.Instrument)
				w.Textf("  candidate: n=%d  mean=%.6g  [%.6g, %.6g]  (stddev=%.4g)\n",
					r.Candidate.N, r.Candidate.Mean, r.Candidate.CILow, r.Candidate.CIHigh, r.Candidate.StdDev)
				if r.Baseline != nil {
					w.Textf("  baseline:  n=%d  mean=%.6g  [%.6g, %.6g]  (stddev=%.4g)\n",
						r.Baseline.N, r.Baseline.Mean, r.Baseline.CILow, r.Baseline.CIHigh, r.Baseline.StdDev)
				}
				if r.Comparison != nil {
					cmp := r.Comparison
					w.Textf("  delta_abs:  %+.6g  95%% CI [%+.6g, %+.6g]\n",
						cmp.DeltaAbs, cmp.CILowAbs, cmp.CIHighAbs)
					w.Textf("  delta_frac: %+.4f  95%% CI [%+.4f, %+.4f]\n",
						cmp.DeltaFrac, cmp.CILowFrac, cmp.CIHighFrac)
					w.Textf("  mann–whitney U=%.1f  p=%.4f\n", cmp.UStat, cmp.PValue)
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&baselineExp, "baseline", "", "baseline experiment id to compare against")
	c.Flags().StringVar(&instName, "instrument", "", "only analyze this instrument")
	c.Flags().IntVar(&iters, "iters", 0, "bootstrap iterations (0 uses default 2000)")
	c.Flags().StringVar(&candidateRef, "candidate-ref", "", "for non-baseline experiments, restrict analysis to observations recorded on this candidate ref")
	return c
}

type observationProvenance struct {
	Ref string
	SHA string
}

func normalizeAnalyzeCandidateRef(exp *entity.Experiment, candidateRef string) (string, error) {
	ref := strings.TrimSpace(candidateRef)
	if ref == "" {
		return "", fmt.Errorf("--candidate-ref is required")
	}
	repoDir := exp.Worktree
	if strings.TrimSpace(repoDir) == "" {
		repoDir = globalProjectDir
	}
	sym, err := worktree.SymbolicFullName(repoDir, ref)
	if err != nil {
		return "", fmt.Errorf("resolve candidate ref %q for %s: %w", ref, exp.ID, err)
	}
	if !strings.HasPrefix(sym, "refs/") {
		return "", fmt.Errorf("candidate ref %q is not a named git ref", ref)
	}
	return sym, nil
}

func filterAnalyzeObservationsByCandidateRef(obs []*entity.Observation, candidateRef string) ([]*entity.Observation, error) {
	filtered := make([]*entity.Observation, 0, len(obs))
	for _, o := range obs {
		if o == nil {
			continue
		}
		if strings.TrimSpace(o.CandidateRef) == candidateRef {
			filtered = append(filtered, o)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no observations recorded for candidate ref %s", candidateRef)
	}
	if provs := distinctObservationProvenances(filtered); len(provs) > 1 {
		return nil, fmt.Errorf(
			"candidate ref %s maps to multiple recorded SHAs (%s); use a unique candidate ref per measured candidate",
			candidateRef, formatObservationProvenances(provs),
		)
	}
	return filtered, nil
}

func distinctObservationProvenances(obs []*entity.Observation) []observationProvenance {
	seen := map[observationProvenance]struct{}{}
	out := make([]observationProvenance, 0)
	for _, o := range obs {
		if o == nil {
			continue
		}
		p := observationProvenance{
			Ref: strings.TrimSpace(o.CandidateRef),
			SHA: strings.TrimSpace(o.CandidateSHA),
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ref == out[j].Ref {
			return out[i].SHA < out[j].SHA
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

func formatObservationProvenances(provs []observationProvenance) string {
	parts := make([]string, 0, len(provs))
	for _, p := range provs {
		switch {
		case p.Ref != "" && p.SHA != "":
			parts = append(parts, fmt.Sprintf("%s@%s", p.Ref, shortSHA(p.SHA)))
		case p.Ref != "":
			parts = append(parts, p.Ref)
		case p.SHA != "":
			parts = append(parts, shortSHA(p.SHA))
		default:
			parts = append(parts, "(legacy)")
		}
	}
	return strings.Join(parts, ", ")
}

func groupByInstrument(obs []*entity.Observation) map[string][]*entity.Observation {
	out := map[string][]*entity.Observation{}
	for _, o := range obs {
		out[o.Instrument] = append(out[o.Instrument], o)
	}
	return out
}

func flattenSamples(obs []*entity.Observation) []float64 {
	var out []float64
	for _, o := range obs {
		if len(o.PerSample) > 0 {
			out = append(out, o.PerSample...)
		} else {
			// Single-value observation: treat the scalar value as one sample.
			out = append(out, o.Value)
		}
	}
	return out
}

func sortedKeys(m map[string][]*entity.Observation) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
