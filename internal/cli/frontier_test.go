package cli

import (
	"bytes"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("TestRenderFrontierSection_UsesHypothesisStatusMarker", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		var buf bytes.Buffer
		w := output.New(&buf, &bytes.Buffer{}, false)

		renderFrontierSection(w, goalFrontier{
			Goal: &entity.Goal{
				ID:        "G-0001",
				Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"},
			},
			Rows: []frontierRow{{
				Conclusion: "C-0001", Hypothesis: "H-0001", Value: 750067,
				Classification: experimentClassificationDead, HypothesisStatus: entity.StatusSupported,
			}},
			Assessment: frontierGoalAssessment{Mode: "open_ended", RecommendedAction: "continue"},
		}, 0)

		out := buf.String()
		if !strings.Contains(out, "[supported]") {
			t.Fatalf("frontier output missing supported marker:\n%s", out)
		}
		if strings.Contains(out, "[dead]") {
			t.Fatalf("frontier output should not show dead marker for supported hypotheses:\n%s", out)
		}
	})
})
