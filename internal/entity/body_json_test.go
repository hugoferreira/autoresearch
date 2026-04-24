package entity_test

import (
	"encoding/json"

	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Body fields on Goal, Hypothesis, Experiment, and Conclusion must survive
// JSON round-trip. They are `yaml:"-"` (the markdown body is stored out-of-band
// in the .md file) but `json:"body,omitempty"` so that agents calling
// `<entity> show --json` see the prose, not just the frontmatter fields.

var _ = Describe("entity JSON body fields", func() {
	DescribeTable("include non-empty markdown bodies",
		func(v any, freshFn func() any, wantBody string) {
			expectJSONBodyRoundTrip(v, freshFn, wantBody)
		},
		Entry("goal",
			&entity.Goal{
				Objective: entity.Objective{
					Instrument: "qemu_cycles", Direction: "decrease",
				},
				Body: "# Steering\n\nFocus on dsp_fir.\n",
			},
			func() any { return &entity.Goal{} },
			"# Steering\n\nFocus on dsp_fir.\n",
		),
		Entry("hypothesis",
			&entity.Hypothesis{
				ID: "H-0001", Claim: "unroll 4x",
				Body: "# Rationale\n\nCache-friendly stride.\n",
			},
			func() any { return &entity.Hypothesis{} },
			"# Rationale\n\nCache-friendly stride.\n",
		),
		Entry("experiment",
			&entity.Experiment{
				ID: "E-0001", Hypothesis: "H-0001",
				Body: "# Design notes\n\nHost tier only, 30 samples.\n",
			},
			func() any { return &entity.Experiment{} },
			"# Design notes\n\nHost tier only, 30 samples.\n",
		),
		Entry("conclusion",
			&entity.Conclusion{
				ID: "C-0001", Hypothesis: "H-0001", Verdict: entity.VerdictSupported,
				Body: "# Interpretation\n\nDelta -14.3%, CI clean.\n",
			},
			func() any { return &entity.Conclusion{} },
			"# Interpretation\n\nDelta -14.3%, CI clean.\n",
		),
	)

	It("omits empty bodies from JSON", func() {
		cases := []any{
			&entity.Goal{Objective: entity.Objective{Instrument: "x", Direction: "decrease"}},
			&entity.Hypothesis{ID: "H-1", Claim: "x"},
			&entity.Experiment{ID: "E-1", Hypothesis: "H-1"},
			&entity.Conclusion{ID: "C-1", Hypothesis: "H-1", Verdict: entity.VerdictInconclusive},
		}
		for _, v := range cases {
			data, err := json.Marshal(v)
			Expect(err).NotTo(HaveOccurred(), "marshal %T", v)
			Expect(string(data)).NotTo(ContainSubstring(`"body"`), "%T should omit empty Body", v)
		}
	})
})

func expectJSONBodyRoundTrip(v any, freshFn func() any, wantBody string) {
	GinkgoHelper()
	data, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	Expect(string(data)).To(ContainSubstring(`"body"`))

	fresh := freshFn()
	Expect(json.Unmarshal(data, fresh)).To(Succeed())
	Expect(bodyOf(fresh)).To(Equal(wantBody))
}

func bodyOf(v any) string {
	GinkgoHelper()
	switch x := v.(type) {
	case *entity.Goal:
		return x.Body
	case *entity.Hypothesis:
		return x.Body
	case *entity.Experiment:
		return x.Body
	case *entity.Conclusion:
		return x.Body
	default:
		Fail("unknown entity body type")
		return ""
	}
}
