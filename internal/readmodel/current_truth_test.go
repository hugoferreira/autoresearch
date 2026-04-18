package readmodel

import (
	"testing"

	"github.com/bytter/autoresearch/internal/entity"
)

func TestSupportedConclusionCountsForReadSurface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{name: "missing legacy status", status: "", want: true},
		{name: "supported", status: entity.StatusSupported, want: true},
		{name: "unreviewed", status: entity.StatusUnreviewed, want: true},
		{name: "legacy killed", status: entity.StatusKilled, want: true},
		{name: "refuted", status: entity.StatusRefuted, want: false},
		{name: "inconclusive", status: entity.StatusInconclusive, want: false},
		{name: "open", status: entity.StatusOpen, want: false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := SupportedConclusionCountsForReadSurface(tc.status); got != tc.want {
				t.Fatalf("SupportedConclusionCountsForReadSurface(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}
