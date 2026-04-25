package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("report markdown rendering", func() {
	DescribeTable("shows observation evidence failures",
		func(failure entity.EvidenceFailure, wantFailure string) {
			out := renderReportMarkdown(testReportWithObservationEvidenceFailures(failure))
			Expect(out).To(ContainSubstring("## Experiments"))
			Expect(out).To(ContainSubstring("O-0001 `timing` = 80 ns"))
			Expect(out).To(ContainSubstring("Evidence failures:"))
			Expect(out).To(ContainSubstring(wantFailure))
		},
		Entry("exit plus stderr",
			entity.EvidenceFailure{Name: testEvidenceName, ExitCode: 7, Error: "trace collection failed"},
			testEvidenceName+" (exit 7): trace collection failed",
		),
		Entry("spawn failure without exit code",
			entity.EvidenceFailure{Name: testEvidenceName, Error: testEvidenceSpawnTraceErr},
			testEvidenceName+": "+testEvidenceSpawnTraceErr,
		),
	)
})
