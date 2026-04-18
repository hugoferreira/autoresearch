package cli

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
)

// Human-facing CLI/TUI/report surfaces intentionally round more aggressively
// than JSON. JSON stays the machine contract; these helpers are for readable
// text where six significant digits on 0-100 scale values is usually noise.

func fmtNumber(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case v == 0:
		return "0"
	case v == math.Trunc(v) && math.Abs(v) < 1e15:
		return strconv.FormatInt(int64(v), 10)
	}

	abs := math.Abs(v)
	switch {
	case abs < 0.01 || abs >= 1e6:
		return strconv.FormatFloat(v, 'g', 4, 64)
	case abs >= 1000:
		return trimTrailingZeros(strconv.FormatFloat(v, 'f', 0, 64))
	case abs >= 100:
		return trimTrailingZeros(strconv.FormatFloat(v, 'f', 1, 64))
	case abs >= 10:
		return trimTrailingZeros(strconv.FormatFloat(v, 'f', 2, 64))
	case abs >= 1:
		return trimTrailingZeros(strconv.FormatFloat(v, 'f', 3, 64))
	default:
		return trimTrailingZeros(strconv.FormatFloat(v, 'f', 4, 64))
	}
}

func fmtSignedNumber(v float64) string {
	if v > 0 {
		return "+" + fmtNumber(v)
	}
	return fmtNumber(v)
}

func fmtRange(low, high float64) string {
	return fmt.Sprintf("[%s, %s]", fmtNumber(low), fmtNumber(high))
}

func fmtSignedRange(low, high float64) string {
	return fmt.Sprintf("[%s, %s]", fmtSignedNumber(low), fmtSignedNumber(high))
}

func formatGoalThresholdDecision(threshold float64, onThreshold string) string {
	return fmt.Sprintf("threshold=%s -> %s", fmtNumber(threshold), onThreshold)
}

func formatSignedCI95(delta, low, high float64) string {
	return fmt.Sprintf("%s  95%% CI %s", fmtSignedNumber(delta), fmtSignedRange(low, high))
}

func formatSignedCI(delta, low, high float64) string {
	return fmt.Sprintf("%s  CI %s", fmtSignedNumber(delta), fmtSignedRange(low, high))
}

// fmtValue formats a numeric value with a compact unit suffix.
// Seconds are scaled to ns/us/ms/s; bytes shortened to B/KB/MB; everything
// else gets compact human precision plus the unit suffix.
func fmtValue(v float64, unit string) string {
	switch unit {
	case "seconds", "s":
		switch {
		case v == 0:
			return "0s"
		case math.Abs(v) < 1e-6:
			return trimTrailingZeros(strconv.FormatFloat(v*1e9, 'f', 2, 64)) + "ns"
		case math.Abs(v) < 1e-3:
			return trimTrailingZeros(strconv.FormatFloat(v*1e6, 'f', 2, 64)) + "us"
		case math.Abs(v) < 1:
			return trimTrailingZeros(strconv.FormatFloat(v*1e3, 'f', 2, 64)) + "ms"
		default:
			return trimTrailingZeros(strconv.FormatFloat(v, 'f', 2, 64)) + "s"
		}
	case "bytes", "byte", "B":
		abs := math.Abs(v)
		switch {
		case abs >= 1024*1024:
			return trimTrailingZeros(strconv.FormatFloat(v/(1024*1024), 'f', 1, 64)) + "MB"
		case abs >= 1024:
			return trimTrailingZeros(strconv.FormatFloat(v/1024, 'f', 1, 64)) + "KB"
		default:
			return trimTrailingZeros(strconv.FormatFloat(v, 'f', 0, 64)) + "B"
		}
	default:
		return fmtNumber(v) + shortUnit(unit)
	}
}

func fmtValueRange(low, high float64, unit string) string {
	return fmt.Sprintf("[%s, %s]", fmtValue(low, unit), fmtValue(high, unit))
}

// formatPredictedEffect renders a PredictedEffect as a human-readable string
// like "decrease host_timing by ≥0.05 (up to 0.1)".
func formatPredictedEffect(pe *entity.PredictedEffect) string {
	s := fmt.Sprintf("%s %s by ≥%s", pe.Direction, pe.Instrument, fmtNumber(pe.MinEffect))
	if pe.MaxEffect > 0 {
		s += fmt.Sprintf(" (up to %s)", fmtNumber(pe.MaxEffect))
	}
	return s
}

func formatPredictedEffectRange(pe *entity.PredictedEffect) string {
	if pe.MaxEffect > 0 {
		return fmt.Sprintf("%s–%s", fmtNumber(pe.MinEffect), fmtNumber(pe.MaxEffect))
	}
	return fmtNumber(pe.MinEffect)
}

func shortUnit(u string) string {
	switch u {
	case "cycles":
		return "cyc"
	case "instructions":
		return "ins"
	case "pass":
		return ""
	default:
		return u
	}
}

func trimTrailingZeros(s string) string {
	if strings.ContainsAny(s, "eE") {
		return s
	}
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "-0" || s == "+0" || s == "" {
		return "0"
	}
	return s
}
