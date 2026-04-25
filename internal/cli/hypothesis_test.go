package cli

import (
	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("hypothesis add", func() {
	BeforeEach(saveGlobals)

	It("rejects predicted instruments outside the active goal boundary", func() {
		dir, s := setupGoalStore()
		Expect(s.RegisterInstrument("qemu_cycles", store.Instrument{Unit: "cycles"})).To(Succeed())

		root := Root()
		root.SetArgs([]string{
			"-C", dir,
			"hypothesis", "add",
			"--claim", "improve qemu cycle count",
			"--predicts-instrument", "qemu_cycles",
			"--predicts-target", "firmware",
			"--predicts-direction", "decrease",
			"--predicts-min-effect", "0.1",
			"--kill-if", "tests fail",
		})

		Expect(root.Execute()).To(MatchError(ContainSubstring("goal objective or an explicit constraint instrument")))
	})

	It("rejects invalidated inspired-by lessons with a scannable override hint", func() {
		dir, s := setupGoalStore()
		Expect(s.WriteLesson(&entity.Lesson{
			ID:     "L-0001",
			Claim:  "old recommendation was refuted",
			Scope:  entity.LessonScopeSystem,
			Status: entity.LessonStatusInvalidated,
		})).To(Succeed())

		_, _, err := runCLIResult(dir,
			"hypothesis", "add",
			"--claim", "try the opposite mechanism",
			"--predicts-instrument", "timing",
			"--predicts-target", "runtime",
			"--predicts-direction", "decrease",
			"--predicts-min-effect", "0.01",
			"--kill-if", "no timing improvement",
			"--inspired-by", "L-0001",
		)
		Expect(err).To(MatchError(And(
			ContainSubstring("lesson L-0001 is invalidated"),
			ContainSubstring("--allow-invalidated"),
		)))
	})

	It("allows invalidated inspired-by lessons when explicitly requested", func() {
		dir, s := setupGoalStore()
		Expect(s.WriteLesson(&entity.Lesson{
			ID:     "L-0001",
			Claim:  "old recommendation was refuted",
			Scope:  entity.LessonScopeSystem,
			Status: entity.LessonStatusInvalidated,
		})).To(Succeed())

		resp := runCLIJSON[cliIDResponse](dir,
			"hypothesis", "add",
			"--claim", "try the opposite mechanism",
			"--predicts-instrument", "timing",
			"--predicts-target", "runtime",
			"--predicts-direction", "decrease",
			"--predicts-min-effect", "0.01",
			"--kill-if", "no timing improvement",
			"--inspired-by", "L-0001",
			"--allow-invalidated",
		)
		Expect(resp.ID).To(Equal("H-0001"))
	})
})
