package entity_test

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Conclusion markdown serialization", func() {
	It("round-trips verdict evidence, comparison provenance, and body", func() {
		c := &entity.Conclusion{
			ID:               "C-0001",
			Hypothesis:       "H-0001",
			Verdict:          entity.VerdictSupported,
			Observations:     []string{"O-0001", "O-0002"},
			CandidateExp:     "E-0002",
			CandidateAttempt: 2,
			CandidateRef:     "refs/heads/candidate/E-0002-a1",
			CandidateSHA:     "0123456789abcdef0123456789abcdef01234567",
			BaselineExp:      "E-0001",
			BaselineAttempt:  1,
			BaselineRef:      "refs/heads/baseline/E-0001-a1",
			BaselineSHA:      "89abcdef0123456789abcdef0123456789abcdef",
			Effect: entity.Effect{
				Instrument: "host_timing",
				DeltaAbs:   -0.0005,
				DeltaFrac:  -0.143,
				CILowFrac:  -0.181,
				CIHighFrac: -0.098,
				PValue:     0.003,
				CIMethod:   "bootstrap_percentile_95",
				NCandidate: 20,
				NBaseline:  20,
			},
			StatTest: "mann_whitney_u",
			Strict: entity.Strict{
				Passed: true,
			},
			Author:    "agent:analyst",
			CreatedAt: time.Date(2026, 4, 11, 15, 0, 0, 0, time.UTC),
			Body:      "# Interpretation\n\nInner loop vectorized by the compiler.\n",
		}

		data, err := c.Marshal()
		Expect(err).NotTo(HaveOccurred())
		back, err := entity.ParseConclusion(data)
		Expect(err).NotTo(HaveOccurred())

		Expect(back.Verdict).To(Equal(entity.VerdictSupported))
		Expect(back.Effect.DeltaFrac).To(Equal(-0.143))
		Expect(back.Observations).To(Equal([]string{"O-0001", "O-0002"}))
		Expect(back.CandidateRef).To(Equal(c.CandidateRef))
		Expect(back.CandidateSHA).To(Equal(c.CandidateSHA))
		Expect(back.CandidateAttempt).To(Equal(c.CandidateAttempt))
		Expect(back.BaselineAttempt).To(Equal(c.BaselineAttempt))
		Expect(back.BaselineRef).To(Equal(c.BaselineRef))
		Expect(back.BaselineSHA).To(Equal(c.BaselineSHA))
		Expect(back.Body).To(Equal(c.Body))
	})

	It("persists strict downgrade reasons", func() {
		c := &entity.Conclusion{
			ID:         "C-0002",
			Hypothesis: "H-0001",
			Verdict:    entity.VerdictInconclusive,
			Strict: entity.Strict{
				Passed:        false,
				RequestedFrom: entity.VerdictSupported,
				Reasons:       []string{"CI high_frac -0.02 crosses zero", "|delta_frac| 0.04 < min_effect 0.10"},
			},
			CreatedAt: time.Now().UTC(),
		}

		data, err := c.Marshal()
		Expect(err).NotTo(HaveOccurred())
		back, err := entity.ParseConclusion(data)
		Expect(err).NotTo(HaveOccurred())

		Expect(back.Strict.Passed).To(BeFalse())
		Expect(back.Strict.RequestedFrom).To(Equal(entity.VerdictSupported))
		Expect(back.Strict.Reasons).To(Equal(c.Strict.Reasons))
	})
})
