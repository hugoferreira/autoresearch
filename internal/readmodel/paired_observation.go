package readmodel

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/stats"
	"github.com/bytter/autoresearch/internal/store"
)

const (
	PairArmBaseline  = "baseline"
	PairArmCandidate = "candidate"

	PairSegmentBefore     = "before"
	PairSegmentAfter      = "after"
	PairSegmentInterleave = "interleave"
	PairSegmentCandidate  = "candidate"
)

type PairedObservationMeta struct {
	PairID              string `json:"pair_id"`
	Mode                string `json:"mode"`
	Arm                 string `json:"arm"`
	Segment             string `json:"segment,omitempty"`
	Order               int    `json:"order"`
	Instrument          string `json:"instrument"`
	CandidateExperiment string `json:"candidate_experiment"`
	CandidateAttempt    int    `json:"candidate_attempt,omitempty"`
	CandidateRef        string `json:"candidate_ref,omitempty"`
	CandidateSHA        string `json:"candidate_sha,omitempty"`
	BaselineExperiment  string `json:"baseline_experiment"`
	BaselineAttempt     int    `json:"baseline_attempt,omitempty"`
	BaselineRef         string `json:"baseline_ref,omitempty"`
	BaselineSHA         string `json:"baseline_sha,omitempty"`
}

type PairedObservationAnalysis struct {
	PairID              string                     `json:"pair_id"`
	Mode                string                     `json:"mode"`
	Instrument          string                     `json:"instrument"`
	CandidateExperiment string                     `json:"candidate_experiment"`
	CandidateRef        string                     `json:"candidate_ref,omitempty"`
	CandidateSHA        string                     `json:"candidate_sha,omitempty"`
	BaselineExperiment  string                     `json:"baseline_experiment"`
	BaselineRef         string                     `json:"baseline_ref,omitempty"`
	BaselineSHA         string                     `json:"baseline_sha,omitempty"`
	Observations        []string                   `json:"observations"`
	Limits              map[string]any             `json:"limits"`
	Candidate           PairedObservationArm       `json:"candidate"`
	Baseline            PairedObservationArm       `json:"baseline"`
	BaselineBefore      *stats.Summary             `json:"baseline_before,omitempty"`
	BaselineAfter       *stats.Summary             `json:"baseline_after,omitempty"`
	Comparison          stats.Comparison           `json:"comparison"`
	Drift               PairedObservationDrift     `json:"drift"`
	Warnings            []string                   `json:"warnings,omitempty"`
	Rows                []PairedObservationRunView `json:"rows"`
}

type PairedObservationArm struct {
	Observations []string      `json:"observations"`
	Samples      []float64     `json:"samples"`
	Summary      stats.Summary `json:"summary"`
}

type PairedObservationDrift struct {
	BaselineBeforeMean      *float64 `json:"baseline_before_mean,omitempty"`
	BaselineAfterMean       *float64 `json:"baseline_after_mean,omitempty"`
	BaselineDriftAbs        float64  `json:"baseline_drift_abs,omitempty"`
	BaselineDriftFrac       float64  `json:"baseline_drift_frac,omitempty"`
	MonotonicDrift          string   `json:"monotonic_drift,omitempty"`
	RangeOverlap            bool     `json:"range_overlap"`
	StdDevRatio             float64  `json:"stddev_ratio,omitempty"`
	VarianceRatio           float64  `json:"variance_ratio,omitempty"`
	VarianceChange          string   `json:"variance_change,omitempty"`
	EffectSmallerThanDrift  bool     `json:"effect_smaller_than_drift"`
	DriftComparableToEffect bool     `json:"drift_comparable_to_effect"`
}

type PairedObservationRunView struct {
	ID      string                `json:"id"`
	Arm     string                `json:"arm"`
	Segment string                `json:"segment,omitempty"`
	Order   int                   `json:"order"`
	Samples []float64             `json:"samples"`
	Mean    float64               `json:"mean"`
	Meta    PairedObservationMeta `json:"meta"`
}

type pairedObservationRun struct {
	obs  *entity.Observation
	meta PairedObservationMeta
}

func ObservationPairMeta(o *entity.Observation) (*PairedObservationMeta, bool) {
	if o == nil || o.Aux == nil {
		return nil, false
	}
	raw, ok := o.Aux[entity.ObservationAuxPair]
	if !ok {
		return nil, false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var meta PairedObservationMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, false
	}
	if strings.TrimSpace(meta.PairID) == "" {
		return nil, false
	}
	return &meta, true
}

func AnalyzePairedObservation(s *store.Store, pairID string, iters int) (*PairedObservationAnalysis, error) {
	pairID = strings.TrimSpace(pairID)
	if pairID == "" {
		return nil, fmt.Errorf("pair id is required")
	}
	all, err := s.ListObservations()
	if err != nil {
		return nil, err
	}

	var runs []pairedObservationRun
	for _, obs := range all {
		meta, ok := ObservationPairMeta(obs)
		if !ok || meta.PairID != pairID {
			continue
		}
		runs = append(runs, pairedObservationRun{obs: obs, meta: *meta})
	}
	if len(runs) == 0 {
		return nil, fmt.Errorf("paired observation %s not found", pairID)
	}
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].meta.Order != runs[j].meta.Order {
			return runs[i].meta.Order < runs[j].meta.Order
		}
		return runs[i].obs.ID < runs[j].obs.ID
	})

	first := runs[0].meta
	out := &PairedObservationAnalysis{
		PairID:              pairID,
		Mode:                first.Mode,
		Instrument:          first.Instrument,
		CandidateExperiment: first.CandidateExperiment,
		CandidateRef:        first.CandidateRef,
		CandidateSHA:        first.CandidateSHA,
		BaselineExperiment:  first.BaselineExperiment,
		BaselineRef:         first.BaselineRef,
		BaselineSHA:         first.BaselineSHA,
		Limits:              map[string]any{"iters": resolvedIterations(iters)},
	}
	var before, after []float64
	var baselineMeans []float64
	for _, run := range runs {
		if err := validatePairedRun(first, run.meta); err != nil {
			return nil, err
		}
		samples := observationSamples(run.obs)
		row := PairedObservationRunView{
			ID:      run.obs.ID,
			Arm:     run.meta.Arm,
			Segment: run.meta.Segment,
			Order:   run.meta.Order,
			Samples: samples,
			Mean:    stats.Mean(samples),
			Meta:    run.meta,
		}
		out.Observations = append(out.Observations, run.obs.ID)
		out.Rows = append(out.Rows, row)
		switch run.meta.Arm {
		case PairArmCandidate:
			out.Candidate.Observations = append(out.Candidate.Observations, run.obs.ID)
			out.Candidate.Samples = append(out.Candidate.Samples, samples...)
		case PairArmBaseline:
			out.Baseline.Observations = append(out.Baseline.Observations, run.obs.ID)
			out.Baseline.Samples = append(out.Baseline.Samples, samples...)
			baselineMeans = append(baselineMeans, row.Mean)
			switch run.meta.Segment {
			case PairSegmentBefore:
				before = append(before, samples...)
			case PairSegmentAfter:
				after = append(after, samples...)
			}
		default:
			return nil, fmt.Errorf("paired observation %s has unsupported arm %q on %s", pairID, run.meta.Arm, run.obs.ID)
		}
	}
	if len(out.Candidate.Samples) == 0 {
		return nil, fmt.Errorf("paired observation %s has no candidate samples", pairID)
	}
	if len(out.Baseline.Samples) == 0 {
		return nil, fmt.Errorf("paired observation %s has no baseline samples", pairID)
	}

	out.Candidate.Summary = stats.Summarize(out.Candidate.Samples, iters, 0)
	out.Baseline.Summary = stats.Summarize(out.Baseline.Samples, iters, 0)
	if len(before) > 0 {
		sum := stats.Summarize(before, iters, 0)
		out.BaselineBefore = &sum
	}
	if len(after) > 0 {
		sum := stats.Summarize(after, iters, 0)
		out.BaselineAfter = &sum
	}
	out.Comparison = stats.CompareSamples(out.Candidate.Samples, out.Baseline.Samples, iters, 0)
	out.Drift, out.Warnings = pairedDriftDiagnostics(out, baselineMeans)
	return out, nil
}

func validatePairedRun(first, next PairedObservationMeta) error {
	if next.Instrument != first.Instrument {
		return fmt.Errorf("paired observation %s mixes instruments %q and %q", first.PairID, first.Instrument, next.Instrument)
	}
	if next.CandidateExperiment != first.CandidateExperiment {
		return fmt.Errorf("paired observation %s mixes candidate experiments %q and %q", first.PairID, first.CandidateExperiment, next.CandidateExperiment)
	}
	if next.BaselineExperiment != first.BaselineExperiment {
		return fmt.Errorf("paired observation %s mixes baseline experiments %q and %q", first.PairID, first.BaselineExperiment, next.BaselineExperiment)
	}
	return nil
}

func observationSamples(o *entity.Observation) []float64 {
	if o == nil {
		return nil
	}
	if len(o.PerSample) > 0 {
		out := make([]float64, len(o.PerSample))
		copy(out, o.PerSample)
		return out
	}
	return []float64{o.Value}
}

func pairedDriftDiagnostics(a *PairedObservationAnalysis, baselineMeans []float64) (PairedObservationDrift, []string) {
	drift := PairedObservationDrift{
		RangeOverlap: rangesOverlap(a.Candidate.Summary.Min, a.Candidate.Summary.Max, a.Baseline.Summary.Min, a.Baseline.Summary.Max),
	}
	if a.Baseline.Summary.StdDev > 0 {
		drift.StdDevRatio = a.Candidate.Summary.StdDev / a.Baseline.Summary.StdDev
		drift.VarianceRatio = drift.StdDevRatio * drift.StdDevRatio
		switch {
		case drift.StdDevRatio >= 2:
			drift.VarianceChange = "candidate_higher"
		case drift.StdDevRatio <= 0.5:
			drift.VarianceChange = "candidate_lower"
		default:
			drift.VarianceChange = "none"
		}
	} else if a.Candidate.Summary.StdDev > 0 {
		drift.VarianceChange = "candidate_higher"
	}
	if a.BaselineBefore != nil && a.BaselineAfter != nil {
		beforeMean := a.BaselineBefore.Mean
		afterMean := a.BaselineAfter.Mean
		drift.BaselineBeforeMean = &beforeMean
		drift.BaselineAfterMean = &afterMean
		drift.BaselineDriftAbs = afterMean - beforeMean
		if beforeMean != 0 {
			drift.BaselineDriftFrac = drift.BaselineDriftAbs / beforeMean
		}
	} else if len(baselineMeans) >= 2 {
		drift.BaselineDriftAbs = baselineMeans[len(baselineMeans)-1] - baselineMeans[0]
		if baselineMeans[0] != 0 {
			drift.BaselineDriftFrac = drift.BaselineDriftAbs / baselineMeans[0]
		}
	}
	drift.MonotonicDrift = monotonicDrift(baselineMeans)
	effectAbs := math.Abs(a.Comparison.DeltaAbs)
	driftAbs := math.Abs(drift.BaselineDriftAbs)
	if effectAbs == 0 {
		drift.EffectSmallerThanDrift = driftAbs > 0
		drift.DriftComparableToEffect = driftAbs > 0
	} else {
		drift.EffectSmallerThanDrift = driftAbs >= effectAbs
		drift.DriftComparableToEffect = driftAbs >= effectAbs*0.5
	}

	var warnings []string
	if drift.DriftComparableToEffect {
		warnings = append(warnings, fmt.Sprintf("baseline drift %.6g is comparable to candidate effect %.6g", drift.BaselineDriftAbs, a.Comparison.DeltaAbs))
	}
	if drift.EffectSmallerThanDrift {
		warnings = append(warnings, "candidate effect is smaller than observed baseline drift")
	}
	if drift.MonotonicDrift == "increase" || drift.MonotonicDrift == "decrease" {
		warnings = append(warnings, "baseline samples show monotonic "+drift.MonotonicDrift+" drift")
	}
	if drift.VarianceChange == "candidate_higher" || drift.VarianceChange == "candidate_lower" {
		if drift.VarianceRatio != 0 {
			warnings = append(warnings, fmt.Sprintf("candidate/baseline variance changed materially (ratio %.4g)", drift.VarianceRatio))
		} else {
			warnings = append(warnings, "candidate/baseline variance changed materially")
		}
	}
	return drift, warnings
}

func rangesOverlap(aMin, aMax, bMin, bMax float64) bool {
	return aMin <= bMax && bMin <= aMax
}

func monotonicDrift(xs []float64) string {
	if len(xs) < 2 {
		return ""
	}
	inc, dec := true, true
	for i := 1; i < len(xs); i++ {
		if xs[i] < xs[i-1] {
			inc = false
		}
		if xs[i] > xs[i-1] {
			dec = false
		}
	}
	switch {
	case inc && dec:
		return "flat"
	case inc:
		return "increase"
	case dec:
		return "decrease"
	default:
		return "mixed"
	}
}

func resolvedIterations(iters int) int {
	if iters <= 0 {
		return stats.DefaultIterations
	}
	return iters
}
