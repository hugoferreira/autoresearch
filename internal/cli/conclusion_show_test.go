package cli

import (
	"encoding/json"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("conclusion show JSON", func() {
	BeforeEach(saveGlobals)

	It("joins artifacts, evidence failures, and read issues for cited observations", func() {
		dir, s := createCLIStoreDir()

		Expect(s.WriteObservation(&entity.Observation{
			ID:         "O-0001",
			Experiment: "E-0001",
			Instrument: "timing",
			MeasuredAt: time.Now().UTC(),
			Value:      100,
			Unit:       "ns",
			Samples:    1,
			Artifacts: []entity.Artifact{{
				Name:  "scalar",
				SHA:   "abc123",
				Path:  "artifacts/ab/c123/scalar.json",
				Bytes: 42,
				Mime:  "application/json",
			}},
			Command: "echo cycles: 100",
			Author:  "test",
		})).To(Succeed())
		Expect(s.WriteObservation(&entity.Observation{
			ID:         "O-0002",
			Experiment: "E-0001",
			Instrument: "timing",
			MeasuredAt: time.Now().UTC(),
			Value:      101,
			Unit:       "ns",
			Samples:    1,
			Command:    "echo cycles: 101",
			Author:     "test",
		})).To(Succeed())
		Expect(s.WriteObservation(&entity.Observation{
			ID:         "O-0003",
			Experiment: "E-0001",
			Instrument: "timing",
			MeasuredAt: time.Now().UTC(),
			Value:      102,
			Unit:       "ns",
			Samples:    1,
			Artifacts: []entity.Artifact{{
				Name:  "evidence/mechanism",
				SHA:   "def456",
				Path:  "artifacts/de/f456/mechanism.txt",
				Bytes: 64,
				Mime:  "text/plain",
			}},
			EvidenceFailures: []entity.EvidenceFailure{{
				Name:     "profile",
				ExitCode: 7,
				Error:    "tool crashed",
			}},
			Command: "echo cycles: 102",
			Author:  "test",
		})).To(Succeed())
		Expect(s.WriteConclusion(&entity.Conclusion{
			ID:               "C-0001",
			Hypothesis:       "H-0001",
			Verdict:          entity.VerdictSupported,
			Observations:     []string{"O-0001", "O-0002", "O-0003", "O-9999"},
			CandidateExp:     "E-0001",
			CandidateAttempt: 2,
			CandidateRef:     "refs/heads/candidate/E-0001-a1",
			CandidateSHA:     "0123456789abcdef0123456789abcdef01234567",
			BaselineExp:      "E-0000",
			BaselineAttempt:  1,
			BaselineRef:      "refs/heads/baseline/E-0000-a1",
			BaselineSHA:      "89abcdef0123456789abcdef0123456789abcdef",
			Effect: entity.Effect{
				Instrument: "timing",
				DeltaAbs:   -20,
				DeltaFrac:  -0.2,
				CILowAbs:   -25,
				CIHighAbs:  -15,
				CILowFrac:  -0.25,
				CIHighFrac: -0.15,
				PValue:     0.01,
				CIMethod:   "bootstrap_bca_95",
				NCandidate: 3,
				NBaseline:  3,
			},
			StatTest:  "mann_whitney_u",
			Strict:    entity.Strict{Passed: true},
			Author:    "agent:orchestrator",
			CreatedAt: time.Now().UTC(),
		})).To(Succeed())

		got := runCLIJSON[conclusionShowJSON](dir, "conclusion", "show", "C-0001")
		Expect(got.Conclusion).NotTo(BeNil())
		Expect(got.ID).To(Equal("C-0001"))
		Expect(got.CandidateRef).To(Equal("refs/heads/candidate/E-0001-a1"))
		Expect(got.CandidateSHA).To(Equal("0123456789abcdef0123456789abcdef01234567"))
		Expect(got.CandidateAttempt).To(Equal(2))
		Expect(got.BaselineAttempt).To(Equal(1))
		Expect(got.BaselineRef).To(Equal("refs/heads/baseline/E-0000-a1"))
		Expect(got.BaselineSHA).To(Equal("89abcdef0123456789abcdef0123456789abcdef"))
		Expect(got.Observations).To(HaveLen(4))
		Expect(got.ObservationArtifacts).To(HaveKey("O-0001"))
		Expect(got.ObservationArtifacts["O-0001"]).To(HaveLen(1))
		Expect(got.ObservationArtifacts).To(HaveKey("O-0002"))
		Expect(got.ObservationArtifacts["O-0002"]).To(BeEmpty())
		Expect(got.ObservationArtifacts).To(HaveKey("O-0003"))
		Expect(got.ObservationArtifacts["O-0003"]).To(HaveLen(1))
		Expect(got.ObservationEvidenceFailures["O-0003"]).To(HaveLen(1))
		Expect(got.ObservationEvidenceFailures["O-0003"][0].Name).To(Equal("profile"))
		Expect(got.ObservationEvidenceFailures["O-0003"][0].ExitCode).To(Equal(7))
		Expect(got.ObservationReadIssues).To(HaveKeyWithValue("O-9999", "observation not found"))
		Expect(got.ObservationReadIssues).NotTo(HaveKey("O-0001"))
	})

	It("omits observation join fields when the conclusion cites no observations", func() {
		dir, s := createCLIStoreDir()
		Expect(s.WriteConclusion(&entity.Conclusion{
			ID:         "C-0002",
			Hypothesis: "H-0002",
			Verdict:    entity.VerdictInconclusive,
			Effect: entity.Effect{
				Instrument: "timing",
				CIMethod:   "bootstrap_bca_95",
			},
			StatTest:  "mann_whitney_u",
			Strict:    entity.Strict{Passed: true},
			Author:    "agent:orchestrator",
			CreatedAt: time.Now().UTC(),
		})).To(Succeed())

		raw := runCLI(dir, "--json", "conclusion", "show", "C-0002")
		var payload map[string]any
		Expect(json.Unmarshal([]byte(raw), &payload)).To(Succeed())
		Expect(payload).NotTo(HaveKey("observation_artifacts"))
		Expect(payload).NotTo(HaveKey("observation_evidence_failures"))
		Expect(payload).NotTo(HaveKey("observation_read_issues"))
	})
})
