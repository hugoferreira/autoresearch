package entity_test

import (
	"bytes"
	"encoding/json"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

// Body fields on Goal, Hypothesis, Experiment, and Conclusion must survive
// JSON round-trip. They are `yaml:"-"` (the markdown body is stored out-of-band
// in the .md file) but `json:"body,omitempty"` so that agents calling
// `<entity> show --json` see the prose, not just the frontmatter fields.

var _ = ginkgo.Describe("TestGoalBodyJSON", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		g := &entity.Goal{
			Objective: entity.Objective{
				Instrument: "qemu_cycles", Direction: "decrease",
			},
			Body: "# Steering\n\nFocus on dsp_fir.\n",
		}
		assertJSONBodyRoundTrip(t, g, func() any { return &entity.Goal{} }, g.Body)
	})
})

var _ = ginkgo.Describe("TestHypothesisBodyJSON", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		h := &entity.Hypothesis{
			ID: "H-0001", Claim: "unroll 4x",
			Body: "# Rationale\n\nCache-friendly stride.\n",
		}
		assertJSONBodyRoundTrip(t, h, func() any { return &entity.Hypothesis{} }, h.Body)
	})
})

var _ = ginkgo.Describe("TestExperimentBodyJSON", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		e := &entity.Experiment{
			ID: "E-0001", Hypothesis: "H-0001",
			Body: "# Design notes\n\nHost tier only, 30 samples.\n",
		}
		assertJSONBodyRoundTrip(t, e, func() any { return &entity.Experiment{} }, e.Body)
	})
})

var _ = ginkgo.Describe("TestConclusionBodyJSON", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		c := &entity.Conclusion{
			ID: "C-0001", Hypothesis: "H-0001", Verdict: entity.VerdictSupported,
			Body: "# Interpretation\n\nDelta -14.3%, CI clean.\n",
		}
		assertJSONBodyRoundTrip(t, c, func() any { return &entity.Conclusion{} }, c.Body)
	})
})

// Empty Body must omit the key entirely (json:"body,omitempty"), so existing
// callers that never set Body don't see a new dangling field.
var _ = ginkgo.Describe("TestEmptyBodyOmitted", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		cases := []any{
			&entity.Goal{Objective: entity.Objective{Instrument: "x", Direction: "decrease"}},
			&entity.Hypothesis{ID: "H-1", Claim: "x"},
			&entity.Experiment{ID: "E-1", Hypothesis: "H-1"},
			&entity.Conclusion{ID: "C-1", Hypothesis: "H-1", Verdict: entity.VerdictInconclusive},
		}
		for _, v := range cases {
			data, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("marshal %T: %v", v, err)
			}
			if bytes.Contains(data, []byte(`"body"`)) {
				t.Errorf("%T: empty Body must be omitted, got %s", v, data)
			}
		}
	})
})

func assertJSONBodyRoundTrip(t testkit.T, v any, freshFn func() any, wantBody string) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(data, []byte(`"body"`)) {
		t.Fatalf("json output missing body key: %s", data)
	}
	fresh := freshFn()
	if err := json.Unmarshal(data, fresh); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := ""
	switch x := fresh.(type) {
	case *entity.Goal:
		got = x.Body
	case *entity.Hypothesis:
		got = x.Body
	case *entity.Experiment:
		got = x.Body
	case *entity.Conclusion:
		got = x.Body
	default:
		t.Fatalf("unknown type %T", x)
	}
	if got != wantBody {
		t.Errorf("body round-trip mismatch:\n want: %q\n  got: %q", wantBody, got)
	}
}
