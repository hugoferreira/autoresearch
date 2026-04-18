package cli

import (
	"testing"

	"github.com/bytter/autoresearch/internal/entity"
)

const (
	testEvidenceName          = "mechanism"
	testEvidenceSpawnTraceErr = `spawn "sh -c echo trace": exec: "sh": executable file not found in $PATH`
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
				Name:     testEvidenceName,
				ExitCode: 7,
			},
			want: testEvidenceName + " (exit 7)",
		},
		{
			name: "spawn error omits meaningless zero exit",
			in: entity.EvidenceFailure{
				Name:  testEvidenceName,
				Error: testEvidenceSpawnTraceErr,
			},
			want: testEvidenceName + ": " + testEvidenceSpawnTraceErr,
		},
		{
			name: "exit and error",
			in: entity.EvidenceFailure{
				Name:     testEvidenceName,
				ExitCode: 7,
				Error:    "trace command failed",
			},
			want: testEvidenceName + " (exit 7): trace command failed",
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
