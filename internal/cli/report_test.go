package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
)

func TestRenderReportMarkdown_ShowsObservationEvidenceFailures(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	report := &reportData{
		Hypothesis: &entity.Hypothesis{
			ID:        "H-0001",
			Claim:     "tighten the hot loop",
			Status:    entity.StatusOpen,
			Author:    "agent:orchestrator",
			CreatedAt: now,
			Predicts: entity.Predicts{
				Instrument: "timing",
				Target:     "kernel",
				Direction:  "decrease",
				MinEffect:  0.1,
			},
			KillIf: []string{"tests fail"},
		},
		Experiments: []*experimentBlock{{
			Experiment: &entity.Experiment{
				ID:          "E-0001",
				Status:      entity.ExpMeasured,
				Instruments: []string{"timing"},
				Author:      "agent:impl",
				Baseline:    entity.Baseline{Ref: "HEAD", SHA: "abcdef1234567890"},
			},
			Observations: []*entity.Observation{{
				ID:         "O-0001",
				Experiment: "E-0001",
				Instrument: "timing",
				Value:      80,
				Unit:       "ns",
				Samples:    1,
				EvidenceFailures: []entity.EvidenceFailure{{
					Name:     "mechanism",
					ExitCode: 7,
					Error:    "trace collection failed",
				}},
			}},
		}},
	}

	out := renderReportMarkdown(report)
	for _, want := range []string{
		"## Experiments",
		"O-0001 `timing` = 80 ns",
		"Evidence failures:",
		"mechanism (exit 7): trace collection failed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q:\n%s", want, out)
		}
	}
}

func TestRenderReportMarkdown_ShowsObservationEvidenceSpawnFailure(t *testing.T) {
	now := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	report := &reportData{
		Hypothesis: &entity.Hypothesis{
			ID:        "H-0001",
			Claim:     "tighten the hot loop",
			Status:    entity.StatusOpen,
			Author:    "agent:orchestrator",
			CreatedAt: now,
			Predicts: entity.Predicts{
				Instrument: "timing",
				Target:     "kernel",
				Direction:  "decrease",
				MinEffect:  0.1,
			},
			KillIf: []string{"tests fail"},
		},
		Experiments: []*experimentBlock{{
			Experiment: &entity.Experiment{
				ID:          "E-0001",
				Status:      entity.ExpMeasured,
				Instruments: []string{"timing"},
				Author:      "agent:impl",
				Baseline:    entity.Baseline{Ref: "HEAD", SHA: "abcdef1234567890"},
			},
			Observations: []*entity.Observation{{
				ID:         "O-0001",
				Experiment: "E-0001",
				Instrument: "timing",
				Value:      80,
				Unit:       "ns",
				Samples:    1,
				EvidenceFailures: []entity.EvidenceFailure{{
					Name:  "mechanism",
					Error: `spawn "sh -c echo trace": exec: "sh": executable file not found in $PATH`,
				}},
			}},
		}},
	}

	out := renderReportMarkdown(report)
	for _, want := range []string{
		"## Experiments",
		"O-0001 `timing` = 80 ns",
		"Evidence failures:",
		`mechanism: spawn "sh -c echo trace": exec: "sh": executable file not found in $PATH`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q:\n%s", want, out)
		}
	}
}
