package cli

import (
	"testing"

	"github.com/bytter/autoresearch/internal/entity"
)

func TestFmtNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    float64
		want string
	}{
		{name: "zero", v: 0, want: "0"},
		{name: "whole number", v: 80, want: "80"},
		{name: "two decimal medium value", v: 80.123456, want: "80.12"},
		{name: "four decimal subunit value", v: 0.123456, want: "0.1235"},
		{name: "tiny value keeps signal", v: 0.00987654, want: "0.009877"},
		{name: "large value rounds compactly", v: 1234.56, want: "1235"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fmtNumber(tc.v); got != tc.want {
				t.Fatalf("fmtNumber(%v) = %q, want %q", tc.v, got, tc.want)
			}
		})
	}
}

func TestFmtSignedNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    float64
		want string
	}{
		{name: "positive", v: 0.123456, want: "+0.1235"},
		{name: "negative", v: -80.123456, want: "-80.12"},
		{name: "zero", v: 0, want: "0"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fmtSignedNumber(tc.v); got != tc.want {
				t.Fatalf("fmtSignedNumber(%v) = %q, want %q", tc.v, got, tc.want)
			}
		})
	}
}

func TestFmtValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    float64
		unit string
		want string
	}{
		{name: "seconds scale to ms", v: 0.0801234, unit: "seconds", want: "80.12ms"},
		{name: "seconds scale to us", v: 0.0001234, unit: "s", want: "123.4us"},
		{name: "bytes scale to KB", v: 1536, unit: "bytes", want: "1.5KB"},
		{name: "cycles abbreviate unit", v: 80.123456, unit: "cycles", want: "80.12cyc"},
		{name: "integers keep unit", v: 42, unit: "widgets", want: "42widgets"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fmtValue(tc.v, tc.unit); got != tc.want {
				t.Fatalf("fmtValue(%v, %q) = %q, want %q", tc.v, tc.unit, got, tc.want)
			}
		})
	}
}

func TestFormatHelpers(t *testing.T) {
	t.Parallel()

	if got, want := formatGoalThresholdDecision(0.2, "ask_human"), "threshold=0.2 -> ask_human"; got != want {
		t.Fatalf("formatGoalThresholdDecision() = %q, want %q", got, want)
	}
	if got, want := formatSignedCI95(-0.123456, -0.2, -0.05), "-0.1235  95% CI [-0.2, -0.05]"; got != want {
		t.Fatalf("formatSignedCI95() = %q, want %q", got, want)
	}
	if got, want := formatSignedCI(0.123456, 0.05, 0.2), "+0.1235  CI [+0.05, +0.2]"; got != want {
		t.Fatalf("formatSignedCI() = %q, want %q", got, want)
	}

	pe := &entity.PredictedEffect{
		Instrument: "timing",
		Direction:  "decrease",
		MinEffect:  0.05,
		MaxEffect:  0.1,
	}
	if got, want := formatPredictedEffect(pe), "decrease timing by ≥0.05 (up to 0.1)"; got != want {
		t.Fatalf("formatPredictedEffect() = %q, want %q", got, want)
	}
	if got, want := formatPredictedEffectRange(pe), "0.05–0.1"; got != want {
		t.Fatalf("formatPredictedEffectRange() = %q, want %q", got, want)
	}
}
