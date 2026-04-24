package cli

import (
	"bytes"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("frontier rendering", func() {
	It("uses the hypothesis status marker instead of the dead classification label", func() {
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
		Expect(out).To(ContainSubstring("[supported]"))
		Expect(out).NotTo(ContainSubstring("[dead]"))
	})
})
