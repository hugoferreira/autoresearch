// Package stats provides the statistical primitives used across analyze and
// conclude. Two sample sizes matter here:
//
//   - Summarize(xs): single-sample summary with a bias-corrected and
//     accelerated (BCa) 95% bootstrap CI. Used by the timing instrument.
//   - CompareSamples(candidate, baseline): paired comparison producing an
//     absolute and a fractional delta, a percentile-bootstrap 95% CI on the
//     fractional delta, and a two-tailed Mann–Whitney U p-value. Used by
//     conclude to decide whether a hypothesis is supported.
//
// Bootstrapping uses a seeded PRNG so runs are reproducible given the same
// input samples. The default seed is derived from a stable constant; callers
// can pass their own seed when determinism across process restarts matters.
package stats

import (
	"math"
	"math/rand/v2"
	"sort"

	"gonum.org/v1/gonum/stat/distuv"
)

const DefaultIterations = 2000
const defaultSeedLo uint64 = 0x6a09e667f3bcc908
const defaultSeedHi uint64 = 0xbb67ae8584caa73b

type Summary struct {
	N        int     `json:"n"`
	Mean     float64 `json:"mean"`
	StdDev   float64 `json:"stddev"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	CILow    float64 `json:"ci_low"`
	CIHigh   float64 `json:"ci_high"`
	CIMethod string  `json:"ci_method"`
	Iters    int     `json:"iters"`
}

type Comparison struct {
	NCandidate    int `json:"n_candidate"`
	NBaseline     int `json:"n_baseline"`
	MeanCandidate float64 `json:"mean_candidate"`
	MeanBaseline  float64 `json:"mean_baseline"`

	DeltaAbs   float64 `json:"delta_abs"`
	CILowAbs   float64 `json:"ci_low_abs"`
	CIHighAbs  float64 `json:"ci_high_abs"`

	DeltaFrac  float64 `json:"delta_frac"`
	CILowFrac  float64 `json:"ci_low_frac"`
	CIHighFrac float64 `json:"ci_high_frac"`

	CIMethod string `json:"ci_method"`
	Iters    int    `json:"iters"`

	TestName string  `json:"test_name"`
	UStat    float64 `json:"u_stat"`
	PValue   float64 `json:"p_value"`
}

func Mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func StdDev(xs []float64) float64 {
	n := len(xs)
	if n < 2 {
		return 0
	}
	m := Mean(xs)
	s := 0.0
	for _, x := range xs {
		d := x - m
		s += d * d
	}
	return math.Sqrt(s / float64(n-1))
}

func minMax(xs []float64) (float64, float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	lo, hi := xs[0], xs[0]
	for _, x := range xs[1:] {
		if x < lo {
			lo = x
		}
		if x > hi {
			hi = x
		}
	}
	return lo, hi
}

// Percentile returns the linearly-interpolated value at fraction p (0..1) of a
// sorted slice.
func Percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[n-1]
	}
	idx := p * float64(n-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}

func defaultRNG(seed uint64) *rand.Rand {
	if seed == 0 {
		return rand.New(rand.NewPCG(defaultSeedLo, defaultSeedHi))
	}
	return rand.New(rand.NewPCG(seed, defaultSeedHi))
}

// Summarize returns a single-sample summary with a BCa 95% bootstrap CI for
// the mean. For n < 2, CI collapses to the mean.
func Summarize(xs []float64, iters int, seed uint64) Summary {
	n := len(xs)
	if iters <= 0 {
		iters = DefaultIterations
	}
	mean := Mean(xs)
	stddev := StdDev(xs)
	lo, hi := minMax(xs)
	out := Summary{
		N:        n,
		Mean:     mean,
		StdDev:   stddev,
		Min:      lo,
		Max:      hi,
		CILow:    mean,
		CIHigh:   mean,
		CIMethod: "bootstrap_bca_95",
		Iters:    iters,
	}
	if n < 2 {
		return out
	}

	rng := defaultRNG(seed)
	means := make([]float64, iters)
	for b := 0; b < iters; b++ {
		sum := 0.0
		for i := 0; i < n; i++ {
			sum += xs[rng.IntN(n)]
		}
		means[b] = sum / float64(n)
	}
	sort.Float64s(means)

	// Bias correction: z0 = Φ^-1(fraction of bootstrap means below the
	// observed mean).
	count := 0
	for _, m := range means {
		if m < mean {
			count++
		}
	}
	p0 := float64(count) / float64(iters)
	// Nudge away from exact 0/1 so Quantile doesn't return ±Inf.
	if p0 <= 0 {
		p0 = 0.5 / float64(iters)
	}
	if p0 >= 1 {
		p0 = 1 - 0.5/float64(iters)
	}
	normal := distuv.Normal{Mu: 0, Sigma: 1}
	z0 := normal.Quantile(p0)

	// Acceleration via jackknife.
	jackMeans := make([]float64, n)
	total := 0.0
	for _, x := range xs {
		total += x
	}
	for i := 0; i < n; i++ {
		jackMeans[i] = (total - xs[i]) / float64(n-1)
	}
	jMean := Mean(jackMeans)
	num, den := 0.0, 0.0
	for _, jm := range jackMeans {
		d := jMean - jm
		num += d * d * d
		den += d * d
	}
	a := 0.0
	if den > 0 {
		a = num / (6 * math.Pow(den, 1.5))
	}

	// Adjusted percentiles for alpha=0.05 two-sided.
	zAlphaLo := normal.Quantile(0.025)
	zAlphaHi := normal.Quantile(0.975)
	alphaLo := normal.CDF(z0 + (z0+zAlphaLo)/(1-a*(z0+zAlphaLo)))
	alphaHi := normal.CDF(z0 + (z0+zAlphaHi)/(1-a*(z0+zAlphaHi)))
	out.CILow = Percentile(means, alphaLo)
	out.CIHigh = Percentile(means, alphaHi)
	return out
}

// CompareSamples returns a comparison of candidate vs baseline samples,
// including absolute and fractional deltas with percentile-bootstrap 95% CIs
// and a two-tailed Mann–Whitney U p-value.
func CompareSamples(candidate, baseline []float64, iters int, seed uint64) Comparison {
	if iters <= 0 {
		iters = DefaultIterations
	}
	nC, nB := len(candidate), len(baseline)
	mC := Mean(candidate)
	mB := Mean(baseline)
	c := Comparison{
		NCandidate:    nC,
		NBaseline:     nB,
		MeanCandidate: mC,
		MeanBaseline:  mB,
		DeltaAbs:      mC - mB,
		CIMethod:      "bootstrap_percentile_95",
		Iters:         iters,
		TestName:      "mann_whitney_u",
	}
	if mB != 0 {
		c.DeltaFrac = (mC - mB) / mB
	}
	if nC < 2 || nB < 2 {
		return c
	}

	rng := defaultRNG(seed)
	absD := make([]float64, iters)
	fracD := make([]float64, iters)
	for i := 0; i < iters; i++ {
		var sumC, sumB float64
		for j := 0; j < nC; j++ {
			sumC += candidate[rng.IntN(nC)]
		}
		for j := 0; j < nB; j++ {
			sumB += baseline[rng.IntN(nB)]
		}
		mc := sumC / float64(nC)
		mb := sumB / float64(nB)
		absD[i] = mc - mb
		if mb != 0 {
			fracD[i] = (mc - mb) / mb
		}
	}
	sort.Float64s(absD)
	sort.Float64s(fracD)
	c.CILowAbs = Percentile(absD, 0.025)
	c.CIHighAbs = Percentile(absD, 0.975)
	c.CILowFrac = Percentile(fracD, 0.025)
	c.CIHighFrac = Percentile(fracD, 0.975)

	u, p := MannWhitneyU(candidate, baseline)
	c.UStat = u
	c.PValue = p
	return c
}

// MannWhitneyU returns the U statistic and two-tailed p-value for the null
// hypothesis that a and b are drawn from the same distribution. Uses the
// normal approximation with tie and continuity corrections; adequate for
// n1, n2 >= 5 and serviceable for smaller samples (slightly conservative).
func MannWhitneyU(a, b []float64) (uStat, pValue float64) {
	n1, n2 := len(a), len(b)
	if n1 == 0 || n2 == 0 {
		return 0, 1
	}
	type item struct {
		v float64
		g int
	}
	combined := make([]item, 0, n1+n2)
	for _, v := range a {
		combined = append(combined, item{v, 0})
	}
	for _, v := range b {
		combined = append(combined, item{v, 1})
	}
	sort.Slice(combined, func(i, j int) bool { return combined[i].v < combined[j].v })

	ranks := make([]float64, len(combined))
	var tieCorrection float64
	i := 0
	for i < len(combined) {
		j := i
		for j < len(combined) && combined[j].v == combined[i].v {
			j++
		}
		avg := float64(i+j+1) / 2.0 // 1-indexed ranks
		for k := i; k < j; k++ {
			ranks[k] = avg
		}
		t := j - i
		if t > 1 {
			tieCorrection += float64(t*t*t - t)
		}
		i = j
	}

	var rankSumA float64
	for k := range combined {
		if combined[k].g == 0 {
			rankSumA += ranks[k]
		}
	}
	u1 := rankSumA - float64(n1*(n1+1))/2.0
	u2 := float64(n1*n2) - u1
	uStat = math.Min(u1, u2)

	meanU := float64(n1*n2) / 2.0
	N := float64(n1 + n2)
	varU := float64(n1*n2) / 12.0 * (N + 1 - tieCorrection/(N*(N-1)))
	if varU <= 0 {
		return uStat, 1
	}
	z := (uStat - meanU) / math.Sqrt(varU)
	// Continuity correction.
	switch {
	case z < 0:
		z += 0.5 / math.Sqrt(varU)
	case z > 0:
		z -= 0.5 / math.Sqrt(varU)
	}
	normal := distuv.Normal{Mu: 0, Sigma: 1}
	pValue = 2 * normal.CDF(-math.Abs(z))
	return uStat, pValue
}
