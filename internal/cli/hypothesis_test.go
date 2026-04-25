package cli

import (
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
})
