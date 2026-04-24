package entity_test

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/testkit"
)

var _ = testkit.Spec("TestConclusionRoundTrip", func(t testkit.T) {
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
	if err != nil {
		t.Fatal(err)
	}
	back, err := entity.ParseConclusion(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Verdict != entity.VerdictSupported {
		t.Errorf("verdict: %q", back.Verdict)
	}
	if back.Effect.DeltaFrac != -0.143 {
		t.Errorf("effect delta_frac: %v", back.Effect.DeltaFrac)
	}
	if len(back.Observations) != 2 {
		t.Errorf("observations: %+v", back.Observations)
	}
	if back.CandidateRef != c.CandidateRef {
		t.Errorf("candidate_ref: %q", back.CandidateRef)
	}
	if back.CandidateSHA != c.CandidateSHA {
		t.Errorf("candidate_sha: %q", back.CandidateSHA)
	}
	if back.CandidateAttempt != c.CandidateAttempt {
		t.Errorf("candidate_attempt: %d", back.CandidateAttempt)
	}
	if back.BaselineAttempt != c.BaselineAttempt {
		t.Errorf("baseline_attempt: %d", back.BaselineAttempt)
	}
	if back.BaselineRef != c.BaselineRef {
		t.Errorf("baseline_ref: %q", back.BaselineRef)
	}
	if back.BaselineSHA != c.BaselineSHA {
		t.Errorf("baseline_sha: %q", back.BaselineSHA)
	}
	if back.Body != c.Body {
		t.Errorf("body round-trip:\n want: %q\n  got: %q", c.Body, back.Body)
	}
})

var _ = testkit.Spec("TestConclusionDowngradeSerialized", func(t testkit.T) {
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
	if err != nil {
		t.Fatal(err)
	}
	back, err := entity.ParseConclusion(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Strict.Passed || back.Strict.RequestedFrom != entity.VerdictSupported {
		t.Errorf("strict state lost: %+v", back.Strict)
	}
	if len(back.Strict.Reasons) != 2 {
		t.Errorf("reasons: %+v", back.Strict.Reasons)
	}
})
