package entity_test

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Observation JSON serialization", func() {
	It("round-trips measurements, candidate provenance, and artifact links", func() {
		pass := true
		low := 0.0045
		high := 0.0051
		o := &entity.Observation{
			ID:         "O-0001",
			Experiment: "E-0001",
			Instrument: "host_timing",
			MeasuredAt: time.Date(2026, 4, 11, 14, 0, 0, 0, time.UTC),
			Value:      0.0048,
			Unit:       "seconds",
			Samples:    10,
			PerSample:  []float64{0.0045, 0.0046, 0.0050, 0.0047, 0.0048, 0.0049, 0.0051, 0.0046, 0.0047, 0.0051},
			CILow:      &low,
			CIHigh:     &high,
			CIMethod:   "bootstrap_bca_95",
			Pass:       &pass,
			Artifacts: []entity.Artifact{
				{Name: "timing", SHA: "abcd1234", Path: "artifacts/ab/cd/timing.json", Bytes: 480},
			},
			Command:      "./a.out",
			ExitCode:     0,
			Attempt:      2,
			CandidateRef: "refs/heads/candidate/E-0001-a2",
			CandidateSHA: "0123456789abcdef0123456789abcdef01234567",
			Author:       "agent:observer",
		}

		data, err := o.Marshal()
		Expect(err).NotTo(HaveOccurred())
		back, err := entity.ParseObservation(data)
		Expect(err).NotTo(HaveOccurred())

		Expect(back.Value).To(Equal(0.0048))
		Expect(back.Samples).To(Equal(10))
		Expect(back.CILow).NotTo(BeNil())
		Expect(*back.CILow).To(Equal(0.0045))
		Expect(back.CIHigh).NotTo(BeNil())
		Expect(*back.CIHigh).To(Equal(0.0051))
		Expect(back.Attempt).To(Equal(2))
		Expect(back.CandidateRef).To(Equal("refs/heads/candidate/E-0001-a2"))
		Expect(back.CandidateSHA).To(Equal("0123456789abcdef0123456789abcdef01234567"))
		Expect(back.Artifacts).To(HaveLen(1))
		Expect(back.Artifacts[0].Name).To(Equal("timing"))
		Expect(back.RawSHA).To(Equal("abcd1234"))
		Expect(back.RawArtifact).To(Equal("artifacts/ab/cd/timing.json"))
	})

	It("keeps all artifacts while exposing the first artifact as primary", func() {
		o := &entity.Observation{
			ID:         "O-0005",
			Experiment: "E-0002",
			Instrument: "objdump",
			Value:      1247,
			Unit:       "instructions",
			Samples:    1,
			Artifacts: []entity.Artifact{
				{Name: "disasm", SHA: "aaaa", Path: "artifacts/aa/aa/disasm.txt", Bytes: 18234112},
				{Name: "symbols", SHA: "bbbb", Path: "artifacts/bb/bb/symbols.txt", Bytes: 42310},
				{Name: "sections", SHA: "cccc", Path: "artifacts/cc/cc/sections.txt", Bytes: 1120},
			},
		}

		data, err := o.Marshal()
		Expect(err).NotTo(HaveOccurred())
		back, err := entity.ParseObservation(data)
		Expect(err).NotTo(HaveOccurred())

		Expect(back.Artifacts).To(HaveLen(3))
		Expect(back.Primary()).NotTo(BeNil())
		Expect(*back.Primary()).To(Equal(entity.Artifact{Name: "disasm", SHA: "aaaa", Path: "artifacts/aa/aa/disasm.txt", Bytes: 18234112}))
		Expect(back.RawSHA).To(Equal("aaaa"))
	})

	It("backfills legacy raw artifact fields into the artifact list", func() {
		// An observation written by M5 (before the Artifacts field existed).
		legacy := []byte(`{
		"id": "O-0001",
		"experiment": "E-0001",
		"instrument": "host_timing",
		"measured_at": "2026-04-11T14:00:00Z",
		"value": 0.002,
		"unit": "seconds",
		"samples": 5,
		"raw_artifact": "artifacts/ab/cd/timing.json",
		"raw_sha": "abcd1234",
		"command": "./a.out",
		"exit_code": 0,
		"author": "agent:observer"
	}`)
		o, err := entity.ParseObservation(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(o.Artifacts).To(Equal([]entity.Artifact{{
			Name: "primary", SHA: "abcd1234", Path: "artifacts/ab/cd/timing.json",
		}}))
	})

	It("preserves evidence capture failures beside primary observations", func() {
		ciLow := 95.0
		ciHigh := 105.0
		obs := &entity.Observation{
			ID:         "O-0001",
			Experiment: "E-0001",
			Instrument: "timing",
			MeasuredAt: time.Date(2026, 4, 18, 11, 0, 0, 0, time.UTC),
			Value:      100,
			Unit:       "cycles",
			Samples:    3,
			PerSample:  []float64{98, 100, 102},
			CILow:      &ciLow,
			CIHigh:     &ciHigh,
			CIMethod:   "bootstrap_bca_95",
			Artifacts: []entity.Artifact{{
				Name:  "scalar",
				SHA:   "abc123",
				Path:  "artifacts/ab/c123/scalar.json",
				Bytes: 42,
				Mime:  "application/json",
			}},
			EvidenceFailures: []entity.EvidenceFailure{{
				Name:     "mechanism",
				ExitCode: 7,
			}},
			Command:     "echo cycles: 100",
			ExitCode:    0,
			Worktree:    "/tmp/worktree",
			BaselineSHA: "deadbeef",
			Author:      "agent:observer",
		}

		data, err := obs.Marshal()
		Expect(err).NotTo(HaveOccurred())
		back, err := entity.ParseObservation(data)
		Expect(err).NotTo(HaveOccurred())

		Expect(back.EvidenceFailures).To(Equal([]entity.EvidenceFailure{{Name: "mechanism", ExitCode: 7}}))
		Expect(back.Artifacts).To(HaveLen(1))
		Expect(back.RawArtifact).To(Equal(obs.Artifacts[0].Path))
		Expect(back.RawSHA).To(Equal(obs.Artifacts[0].SHA))
	})
})
