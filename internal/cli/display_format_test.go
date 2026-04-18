package cli

import "testing"

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
