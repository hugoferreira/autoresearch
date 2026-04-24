package stats_test

import (
	"math"

	"github.com/bytter/autoresearch/internal/stats"
	"github.com/bytter/autoresearch/internal/testkit"
)

func approx(a, b, eps float64) bool {
	return math.Abs(a-b) < eps
}

var _ = testkit.Spec("TestMeanStdDev", func(t testkit.T) {
	xs := []float64{1, 2, 3, 4, 5}
	if m := stats.Mean(xs); m != 3.0 {
		t.Errorf("mean: got %v, want 3", m)
	}
	if sd := stats.StdDev(xs); !approx(sd, math.Sqrt(2.5), 1e-9) {
		t.Errorf("stddev: got %v", sd)
	}
})

var _ = testkit.Spec("TestPercentile", func(t testkit.T) {
	xs := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if got := stats.Percentile(xs, 0.5); got != 5.5 {
		t.Errorf("median: got %v, want 5.5", got)
	}
	if got := stats.Percentile(xs, 0); got != 1 {
		t.Errorf("0th: %v", got)
	}
	if got := stats.Percentile(xs, 1); got != 10 {
		t.Errorf("100th: %v", got)
	}
})

var _ = testkit.Spec("TestSummarizeBracketsMean", func(t testkit.T) {
	// A tight distribution: CI should bracket the mean and be narrow.
	xs := []float64{
		1.001, 0.999, 1.002, 0.998, 1.000,
		1.003, 0.997, 1.001, 0.999, 1.000,
		1.002, 0.998, 1.001, 0.999, 1.000,
	}
	s := stats.Summarize(xs, 2000, 1)
	if !approx(s.Mean, 1.0, 1e-3) {
		t.Errorf("mean: %v", s.Mean)
	}
	if s.CILow > s.Mean || s.CIHigh < s.Mean {
		t.Errorf("CI does not bracket mean: low=%v mean=%v high=%v", s.CILow, s.Mean, s.CIHigh)
	}
	if s.CIHigh-s.CILow > 0.01 {
		t.Errorf("CI too wide for a tight sample: [%v, %v]", s.CILow, s.CIHigh)
	}
})

var _ = testkit.Spec("TestSummarizeNoiseDoesNotCollapseCI", func(t testkit.T) {
	// Noisy sample — CI should be meaningfully wide.
	xs := []float64{1.0, 2.0, 0.5, 3.0, 0.8, 2.5, 1.2, 0.7, 2.8, 1.5}
	s := stats.Summarize(xs, 2000, 1)
	if s.CIHigh-s.CILow < 0.1 {
		t.Errorf("CI implausibly narrow: [%v, %v]", s.CILow, s.CIHigh)
	}
})

var _ = testkit.Spec("TestCompareSamplesClearDecrease", func(t testkit.T) {
	// Baseline centered around 1.0; candidate around 0.8 (20% decrease).
	baseline := []float64{1.00, 1.01, 0.99, 1.02, 0.98, 1.01, 0.99, 1.00, 1.02, 0.98,
		1.01, 0.99, 1.00, 1.02, 0.98, 1.01, 0.99, 1.00, 1.02, 0.98}
	candidate := []float64{0.80, 0.81, 0.79, 0.82, 0.78, 0.81, 0.79, 0.80, 0.82, 0.78,
		0.81, 0.79, 0.80, 0.82, 0.78, 0.81, 0.79, 0.80, 0.82, 0.78}
	c := stats.CompareSamples(candidate, baseline, 2000, 1)
	if !approx(c.DeltaFrac, -0.20, 0.01) {
		t.Errorf("delta_frac: got %v, want ≈ -0.20", c.DeltaFrac)
	}
	if c.CIHighFrac >= 0 {
		t.Errorf("CI high should be negative for clean decrease: %v", c.CIHighFrac)
	}
	if c.PValue > 0.01 {
		t.Errorf("p-value should be very small for clean decrease: %v", c.PValue)
	}
})

var _ = testkit.Spec("TestCompareSamplesNoDifference", func(t testkit.T) {
	// Two samples from the same distribution — CI should cross zero, p large.
	xs := []float64{1.00, 1.01, 0.99, 1.02, 0.98, 1.01, 0.99, 1.00, 1.02, 0.98}
	ys := []float64{1.00, 1.02, 0.99, 1.01, 0.98, 1.00, 0.99, 1.01, 1.02, 0.98}
	c := stats.CompareSamples(xs, ys, 2000, 1)
	if c.CILowFrac > 0 || c.CIHighFrac < 0 {
		t.Errorf("CI should straddle zero: [%v, %v]", c.CILowFrac, c.CIHighFrac)
	}
	if c.PValue < 0.05 {
		t.Errorf("p-value should not be significant: %v", c.PValue)
	}
})

var _ = testkit.Spec("TestMannWhitneyUKnown", func(t testkit.T) {
	// Textbook example: clear separation.
	a := []float64{1, 2, 3, 4, 5}
	b := []float64{6, 7, 8, 9, 10}
	u, p := stats.MannWhitneyU(a, b)
	if u != 0 {
		t.Errorf("U: got %v, want 0 (complete separation)", u)
	}
	if p > 0.02 {
		t.Errorf("p: got %v, want small for complete separation", p)
	}
})

var _ = testkit.Spec("TestDeterministicSeed", func(t testkit.T) {
	xs := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	s1 := stats.Summarize(xs, 500, 42)
	s2 := stats.Summarize(xs, 500, 42)
	if s1.CILow != s2.CILow || s1.CIHigh != s2.CIHigh {
		t.Errorf("same seed should give same CI: %+v vs %+v", s1, s2)
	}
})
