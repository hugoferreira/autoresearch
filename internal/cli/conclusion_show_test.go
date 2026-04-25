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
		now := time.Now().UTC()
		writeObservation := func(id string, value float64, artifacts []entity.Artifact, failures []entity.EvidenceFailure) {
			GinkgoHelper()
			Expect(s.WriteObservation(&entity.Observation{
				ID:               id,
				Experiment:       "E-0001",
				Instrument:       "timing",
				MeasuredAt:       now,
				Value:            value,
				Unit:             "ns",
				Samples:          1,
				Artifacts:        artifacts,
				EvidenceFailures: failures,
				Command:          "echo cycles",
				Author:           "test",
			})).To(Succeed())
		}

		writeObservation("O-0001", 100, []entity.Artifact{{
			Name:  "scalar",
			SHA:   "abc123",
			Path:  "artifacts/ab/c123/scalar.json",
			Bytes: 42,
			Mime:  "application/json",
		}}, nil)
		writeObservation("O-0002", 101, nil, nil)
		writeObservation("O-0003", 102, []entity.Artifact{{
			Name:  "evidence/mechanism",
			SHA:   "def456",
			Path:  "artifacts/de/f456/mechanism.txt",
			Bytes: 64,
			Mime:  "text/plain",
		}}, []entity.EvidenceFailure{{
			Name:     "profile",
			ExitCode: 7,
			Error:    "tool crashed",
		}})
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
			CreatedAt: now,
		})).To(Succeed())

		got := runCLIJSON[conclusionShowJSON](dir, "conclusion", "show", "C-0001")
		Expect(got.Conclusion).NotTo(BeNil())
		Expect(got.Observations).To(Equal([]string{"O-0001", "O-0002", "O-0003", "O-9999"}))
		Expect(got.ObservationArtifacts).To(HaveKeyWithValue("O-0001", ConsistOf(SatisfyAll(
			HaveField("Name", "scalar"),
			HaveField("Bytes", int64(42)),
		))))
		Expect(got.ObservationArtifacts).To(HaveKeyWithValue("O-0002", BeEmpty()))
		Expect(got.ObservationArtifacts).To(HaveKeyWithValue("O-0003", ConsistOf(SatisfyAll(
			HaveField("Name", "evidence/mechanism"),
			HaveField("Bytes", int64(64)),
		))))
		Expect(got.ObservationEvidenceFailures).To(HaveKeyWithValue("O-0003", ConsistOf(SatisfyAll(
			HaveField("Name", "profile"),
			HaveField("ExitCode", 7),
		))))
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
