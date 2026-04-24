package cli

import (
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/testkit"
)

func testReportWithObservationEvidenceFailures(failures ...entity.EvidenceFailure) *reportData {
	now := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	return &reportData{
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
				ID:               "O-0001",
				Experiment:       "E-0001",
				Instrument:       "timing",
				Value:            80,
				Unit:             "ns",
				Samples:          1,
				EvidenceFailures: failures,
			}},
		}},
	}
}

var _ = testkit.Spec("TestRenderReportMarkdown_ShowsObservationEvidenceFailures", func(t testkit.T) {
	report := testReportWithObservationEvidenceFailures(entity.EvidenceFailure{
		Name:     testEvidenceName,
		ExitCode: 7,
		Error:    "trace collection failed",
	})

	out := renderReportMarkdown(report)
	for _, want := range []string{
		"## Experiments",
		"O-0001 `timing` = 80 ns",
		"Evidence failures:",
		testEvidenceName + " (exit 7): trace collection failed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q:\n%s", want, out)
		}
	}
})

var _ = testkit.Spec("TestRenderReportMarkdown_ShowsObservationEvidenceSpawnFailure", func(t testkit.T) {
	report := testReportWithObservationEvidenceFailures(entity.EvidenceFailure{
		Name:  testEvidenceName,
		Error: testEvidenceSpawnTraceErr,
	})

	out := renderReportMarkdown(report)
	for _, want := range []string{
		"## Experiments",
		"O-0001 `timing` = 80 ns",
		"Evidence failures:",
		testEvidenceName + ": " + testEvidenceSpawnTraceErr,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q:\n%s", want, out)
		}
	}
})
