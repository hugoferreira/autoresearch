package stats_test

import (
	"math"

	"github.com/bytter/autoresearch/internal/stats"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("descriptive statistics", func() {
	Describe("Mean and StdDev", func() {
		It("computes sample mean and sample standard deviation", func() {
			xs := []float64{1, 2, 3, 4, 5}

			Expect(stats.Mean(xs)).To(Equal(3.0))
			Expect(stats.StdDev(xs)).To(BeNumerically("~", math.Sqrt(2.5), 1e-9))
		})
	})

	Describe("Percentile", func() {
		It("interpolates percentile positions across sorted samples", func() {
			xs := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

			Expect(stats.Percentile(xs, 0)).To(Equal(1.0))
			Expect(stats.Percentile(xs, 0.5)).To(Equal(5.5))
			Expect(stats.Percentile(xs, 1)).To(Equal(10.0))
		})
	})
})

var _ = Describe("bootstrap summaries", func() {
	It("keeps a tight sample CI narrow while bracketing the mean", func() {
		xs := []float64{
			1.001, 0.999, 1.002, 0.998, 1.000,
			1.003, 0.997, 1.001, 0.999, 1.000,
			1.002, 0.998, 1.001, 0.999, 1.000,
		}

		summary := stats.Summarize(xs, 2000, 1)

		Expect(summary.Mean).To(BeNumerically("~", 1.0, 1e-3))
		Expect(summary.CILow).To(BeNumerically("<=", summary.Mean))
		Expect(summary.CIHigh).To(BeNumerically(">=", summary.Mean))
		Expect(summary.CIHigh - summary.CILow).To(BeNumerically("<=", 0.01))
	})

	It("does not collapse the CI for a noisy sample", func() {
		xs := []float64{1.0, 2.0, 0.5, 3.0, 0.8, 2.5, 1.2, 0.7, 2.8, 1.5}

		summary := stats.Summarize(xs, 2000, 1)

		Expect(summary.CIHigh - summary.CILow).To(BeNumerically(">=", 0.1))
	})

	It("is deterministic for a fixed seed", func() {
		xs := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

		first := stats.Summarize(xs, 500, 42)
		second := stats.Summarize(xs, 500, 42)

		Expect(first.CILow).To(Equal(second.CILow))
		Expect(first.CIHigh).To(Equal(second.CIHigh))
	})
})

var _ = Describe("sample comparisons", func() {
	It("detects a clear candidate decrease against baseline", func() {
		baseline := []float64{
			1.00, 1.01, 0.99, 1.02, 0.98, 1.01, 0.99, 1.00, 1.02, 0.98,
			1.01, 0.99, 1.00, 1.02, 0.98, 1.01, 0.99, 1.00, 1.02, 0.98,
		}
		candidate := []float64{
			0.80, 0.81, 0.79, 0.82, 0.78, 0.81, 0.79, 0.80, 0.82, 0.78,
			0.81, 0.79, 0.80, 0.82, 0.78, 0.81, 0.79, 0.80, 0.82, 0.78,
		}

		comparison := stats.CompareSamples(candidate, baseline, 2000, 1)

		Expect(comparison.DeltaFrac).To(BeNumerically("~", -0.20, 0.01))
		Expect(comparison.CIHighFrac).To(BeNumerically("<", 0))
		Expect(comparison.PValue).To(BeNumerically("<=", 0.01))
	})

	It("keeps same-distribution samples inconclusive", func() {
		xs := []float64{1.00, 1.01, 0.99, 1.02, 0.98, 1.01, 0.99, 1.00, 1.02, 0.98}
		ys := []float64{1.00, 1.02, 0.99, 1.01, 0.98, 1.00, 0.99, 1.01, 1.02, 0.98}

		comparison := stats.CompareSamples(xs, ys, 2000, 1)

		Expect(comparison.CILowFrac).To(BeNumerically("<=", 0))
		Expect(comparison.CIHighFrac).To(BeNumerically(">=", 0))
		Expect(comparison.PValue).To(BeNumerically(">=", 0.05))
	})

	It("computes Mann-Whitney U for complete separation", func() {
		u, p := stats.MannWhitneyU(
			[]float64{1, 2, 3, 4, 5},
			[]float64{6, 7, 8, 9, 10},
		)

		Expect(u).To(Equal(0.0))
		Expect(p).To(BeNumerically("<=", 0.02))
	})
})
