package cli

import (
	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// registerExtraInstrument adds an unreferenced instrument named `name` to
// the store so tests can exercise delete without having to design a new
// goal around it.
func registerExtraInstrument(s *store.Store, name string) {
	GinkgoHelper()
	Expect(s.RegisterInstrument(name, store.Instrument{
		Cmd: []string{"true"}, Parser: "builtin:passfail", Unit: "bool",
	})).To(Succeed())
}

var _ = Describe("instrument delete", func() {
	BeforeEach(saveGlobals)

	It("deletes an unreferenced instrument and records the audit payload", func() {
		dir, s := setupGoalStore()
		registerExtraInstrument(s, "extra")

		runCLI(dir, "instrument", "delete", "extra", "--reason", "typo")

		insts, err := s.ListInstruments()
		Expect(err).NotTo(HaveOccurred())
		Expect(insts).NotTo(HaveKey("extra"))

		event := findLastEvent(s, "instrument.delete")
		Expect(event).NotTo(BeNil())
		payload := decodePayload(event)
		Expect(payload).To(HaveKeyWithValue("name", "extra"))
		Expect(payload).To(HaveKeyWithValue("reason", "typo"))
		Expect(payload).To(HaveKeyWithValue("forced", false))
	})

	DescribeTable("refuses unsafe deletes",
		func(name string, extraArgs []string, wantErr string) {
			dir, _ := setupGoalStore()
			root := Root()
			root.SetArgs(append([]string{"-C", dir, "instrument", "delete", name}, extraArgs...))
			Expect(root.Execute()).To(MatchError(ContainSubstring(wantErr)))
		},
		Entry("unknown instrument", "nonexistent", nil, "not found"),
		Entry("goal objective", "timing", nil, "active goal objective"),
		Entry("forced goal objective", "timing", []string{"--force"}, "active goal objective"),
	)

	It("requires --force before orphaning a referenced instrument", func() {
		dir, s := setupGoalStore()
		h := &entity.Hypothesis{
			ID:     "H-0001",
			GoalID: "G-0001",
			Claim:  "shrink",
			Predicts: entity.Predicts{
				Instrument: "binary_size",
				Target:     "firmware",
				Direction:  "decrease",
				MinEffect:  0.05,
			},
			KillIf: []string{"tests fail"},
			Status: entity.StatusOpen,
			Author: "human",
		}
		Expect(s.WriteHypothesis(h)).To(Succeed())

		root := Root()
		root.SetArgs([]string{"-C", dir, "instrument", "delete", "binary_size"})
		Expect(root.Execute()).To(MatchError(ContainSubstring("--force")))
	})

	It("can force-delete goal-constraint-only instruments and reports orphans", func() {
		dir, s := setupGoalStore()

		runCLI(dir, "instrument", "delete", "host_test", "--force", "--reason", "deprecated")

		insts, err := s.ListInstruments()
		Expect(err).NotTo(HaveOccurred())
		Expect(insts).NotTo(HaveKey("host_test"))

		event := findLastEvent(s, "instrument.delete")
		Expect(event).NotTo(BeNil())
		payload := decodePayload(event)
		Expect(payload).To(HaveKeyWithValue("forced", true))
		orphaned, ok := payload["orphaned"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(orphaned["goal_constraints"]).NotTo(BeEmpty())
	})
})
