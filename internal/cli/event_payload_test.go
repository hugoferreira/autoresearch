package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("event payloads", func() {
	BeforeEach(saveGlobals)

	DescribeTable("records hypothesis status transition payloads",
		func(action, from, to, reason string) {
			dir, s := setupGoalStore()
			Expect(s.WriteHypothesis(&entity.Hypothesis{
				ID:     "H-0001",
				GoalID: "G-0001",
				Claim:  "tighten loop",
				Predicts: entity.Predicts{
					Instrument: "timing", Target: "fir", Direction: "decrease", MinEffect: 0.1,
				},
				KillIf:    []string{"tests fail"},
				Status:    from,
				Author:    "human",
				CreatedAt: time.Now().UTC(),
			})).To(Succeed())
			Expect(s.UpdateState(func(st *store.State) error {
				st.Counters["H"] = 1
				return nil
			})).To(Succeed())

			runCLI(dir, "hypothesis", action, "H-0001", "--reason", reason)

			event := findLastEvent(s, "hypothesis."+action)
			Expect(event).NotTo(BeNil())
			payload := decodePayload(event)
			Expect(payload).To(HaveKeyWithValue("from", from))
			Expect(payload).To(HaveKeyWithValue("to", to))
			Expect(payload).To(HaveKeyWithValue("reason", reason))
		},
		Entry("kill", "kill", entity.StatusOpen, entity.StatusKilled, "obsolete"),
		Entry("reopen", "reopen", entity.StatusKilled, entity.StatusOpen, "new evidence"),
	)

	It("emits a lowercase field map for instrument registration", func() {
		dir := GinkgoT().TempDir()
		_, err := store.Create(dir, store.Config{
			Build: store.CommandSpec{Command: "true"},
			Test:  store.CommandSpec{Command: "true"},
		})
		Expect(err).NotTo(HaveOccurred())

		runCLI(dir,
			"instrument", "register", "host_test",
			"--cmd", "go,test,./...",
			"--parser", "builtin:passfail",
			"--unit", "bool",
			"--min-samples", "1",
			"--requires", "build=pass",
			"--evidence", "mechanism=printf trace",
		)

		s, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		event := findLastEvent(s, "instrument.register")
		Expect(event).NotTo(BeNil())
		payload := decodePayload(event)

		Expect(payload).To(HaveKey("cmd"))
		Expect(payload).To(HaveKey("parser"))
		Expect(payload).To(HaveKey("unit"))
		Expect(payload).To(HaveKey("min_samples"))
		Expect(payload).To(HaveKey("requires"))
		Expect(payload).To(HaveKey("evidence"))
		Expect(payload).NotTo(HaveKey("Cmd"))
		Expect(payload).NotTo(HaveKey("Parser"))
		Expect(payload).NotTo(HaveKey("Unit"))
		Expect(payload).NotTo(HaveKey("MinSamples"))
		Expect(payload).NotTo(HaveKey("Requires"))
		Expect(payload).NotTo(HaveKey("Evidence"))
		Expect(payload).To(HaveKeyWithValue("parser", "builtin:passfail"))
		Expect(payload).To(HaveKeyWithValue("unit", "bool"))
		Expect(payload["requires"]).To(ConsistOf("build=pass"))

		evidence, ok := payload["evidence"].([]any)
		Expect(ok).To(BeTrue())
		Expect(evidence).To(HaveLen(1))
		ev, ok := evidence[0].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(ev).To(HaveKeyWithValue("name", "mechanism"))
		Expect(ev).To(HaveKeyWithValue("cmd", "printf trace"))
		Expect(ev).NotTo(HaveKey("Name"))
	})

	It("includes evidence failures and candidate scope in observation events", func() {
		dir := gitInitScenarioRepo()
		_, err := store.Create(dir, store.Config{
			Build:     store.CommandSpec{Command: "true"},
			Test:      store.CommandSpec{Command: "true"},
			Worktrees: store.WorktreesConfig{Root: GinkgoT().TempDir()},
		})
		Expect(err).NotTo(HaveOccurred())

		registerScenarioTimingInstrument(dir, "broken=echo nope >&2; exit 7")
		registerScenarioSupportInstruments(dir)
		goal := runCLIJSON[cliIDResponse](dir,
			"goal", "set",
			"--objective-instrument", "timing",
			"--objective-target", "kernel",
			"--objective-direction", "decrease",
			"--constraint-require", "host_test=pass",
		)
		Expect(goal.ID).NotTo(BeEmpty())

		hyp := runCLIJSON[cliIDResponse](dir,
			"hypothesis", "add",
			"--claim", "tighten the hot loop",
			"--predicts-instrument", "timing",
			"--predicts-target", "kernel",
			"--predicts-direction", "decrease",
			"--predicts-min-effect", "0.1",
			"--kill-if", "tests fail",
		)
		exp := runCLIJSON[cliIDResponse](dir,
			"experiment", "design", hyp.ID,
			"--baseline", "HEAD",
			"--instruments", "timing",
		)
		impl := runCLIJSON[cliImplementResponse](dir, "experiment", "implement", exp.ID)
		candidateRef := commitScenarioMetricsCandidate(impl.Worktree, "candidate/event-timing", "improve timing", "80\n", "900\n")
		runCLI(dir, "observe", exp.ID, "--instrument", "timing", "--candidate-ref", candidateRef)

		s, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		event := findLastEvent(s, "observation.record")
		Expect(event).NotTo(BeNil())
		payload := decodePayload(event)
		failures, ok := payload["evidence_failures"].([]any)
		Expect(ok).To(BeTrue())
		Expect(failures).To(HaveLen(1))
		failure, ok := failures[0].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(failure).To(HaveKeyWithValue("name", "broken"))
		Expect(failure).To(HaveKeyWithValue("exit_code", float64(7)))
		Expect(payload).To(HaveKeyWithValue("attempt", float64(1)))
		Expect(payload["candidate_ref"]).To(BeAssignableToTypeOf(""))
		Expect(payload["candidate_ref"]).NotTo(BeEmpty())
		Expect(payload["candidate_sha"]).To(BeAssignableToTypeOf(""))
		Expect(payload["candidate_sha"]).NotTo(BeEmpty())
	})
})
