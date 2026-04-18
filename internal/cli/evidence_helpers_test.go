package cli

import (
	"testing"

	"github.com/bytter/autoresearch/internal/entity"
)

func TestFormatEvidenceFailure(t *testing.T) {
	tests := []struct {
		name string
		in   entity.EvidenceFailure
		want string
	}{
		{
			name: "exit only",
			in: entity.EvidenceFailure{
				Name:     "mechanism",
				ExitCode: 7,
			},
			want: "mechanism (exit 7)",
		},
		{
			name: "spawn error omits meaningless zero exit",
			in: entity.EvidenceFailure{
				Name:  "mechanism",
				Error: `spawn "sh -c echo trace": exec: "sh": executable file not found in $PATH`,
			},
			want: `mechanism: spawn "sh -c echo trace": exec: "sh": executable file not found in $PATH`,
		},
		{
			name: "exit and error",
			in: entity.EvidenceFailure{
				Name:     "mechanism",
				ExitCode: 7,
				Error:    "trace command failed",
			},
			want: "mechanism (exit 7): trace command failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatEvidenceFailure(tt.in); got != tt.want {
				t.Fatalf("formatEvidenceFailure(%+v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
