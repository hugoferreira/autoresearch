package cli

import (
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type observePairJSON struct {
	PairID                string   `json:"pair_id"`
	Mode                  string   `json:"mode"`
	Instrument            string   `json:"instrument"`
	SamplesPerArm         int      `json:"samples_per_arm"`
	CandidateExperiment   string   `json:"candidate_experiment"`
	BaselineExperiment    string   `json:"baseline_experiment"`
	Observations          []string `json:"observations"`
	CandidateObservations []string `json:"candidate_observations"`
	BaselineObservations  []string `json:"baseline_observations"`
	BaselineBefore        []string `json:"baseline_before_observations"`
	BaselineAfter         []string `json:"baseline_after_observations"`
}

var _ = Describe("paired observations", func() {
	BeforeEach(saveGlobals)

	It("records a bracketed pair and analyzes drift diagnostics", func() {
		dir, baselineID, candidateExp, candidateRef := setupPairedObservationScenario()

		resp := runCLIJSON[observePairJSON](dir,
			"observe-pair", candidateExp,
			"--baseline", baselineID,
			"--instrument", "timing",
			"--candidate-ref", candidateRef,
			"--samples", "1",
			"--mode", "bracket",
		)
		Expect(resp.PairID).To(HavePrefix("P-"))
		Expect(resp.Mode).To(Equal("bracket"))
		Expect(resp.SamplesPerArm).To(Equal(1))
		Expect(resp.Observations).To(HaveLen(3))
		Expect(resp.CandidateObservations).To(HaveLen(1))
		Expect(resp.BaselineObservations).To(HaveLen(2))
		Expect(resp.BaselineBefore).To(HaveLen(1))
		Expect(resp.BaselineAfter).To(HaveLen(1))

		s, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		event := findLastEvent(s, "observation.pair_record")
		Expect(event).NotTo(BeNil())
		Expect(event.Subject).To(Equal(resp.PairID))
		payload := decodePayload(event)
		Expect(payload).To(HaveKeyWithValue("mode", "bracket"))
		Expect(payload).To(HaveKeyWithValue("instrument", "timing"))

		obs, err := s.ReadObservation(resp.CandidateObservations[0])
		Expect(err).NotTo(HaveOccurred())
		meta, ok := readmodel.ObservationPairMeta(obs)
		Expect(ok).To(BeTrue())
		Expect(meta.PairID).To(Equal(resp.PairID))
		Expect(meta.Arm).To(Equal(readmodel.PairArmCandidate))
		Expect(meta.CandidateRef).NotTo(BeEmpty())

		analysis := runCLIJSON[readmodel.PairedObservationAnalysis](dir, "analyze-pair", resp.PairID)
		Expect(analysis.PairID).To(Equal(resp.PairID))
		Expect(analysis.Candidate.Summary.N).To(Equal(1))
		Expect(analysis.Baseline.Summary.N).To(Equal(2))
		Expect(analysis.BaselineBefore).NotTo(BeNil())
		Expect(analysis.BaselineAfter).NotTo(BeNil())
		Expect(analysis.Drift.EffectSmallerThanDrift).To(BeTrue())
		Expect(analysis.Drift.DriftComparableToEffect).To(BeTrue())
		Expect(strings.Join(analysis.Warnings, "\n")).To(ContainSubstring("baseline drift"))

		text := runCLI(dir, "analyze-pair", resp.PairID)
		expectText(text, "delta_abs:", "baseline drift:", "warning: baseline drift")
	})

	It("records interleaved one-sample baseline and candidate runs", func() {
		dir, baselineID, candidateExp, candidateRef := setupPairedObservationScenario()

		resp := runCLIJSON[observePairJSON](dir,
			"observe-pair", candidateExp,
			"--baseline", baselineID,
			"--instrument", "timing",
			"--candidate-ref", candidateRef,
			"--samples", "2",
			"--mode", "interleave",
		)
		Expect(resp.Observations).To(HaveLen(4))
		Expect(resp.CandidateObservations).To(HaveLen(2))
		Expect(resp.BaselineObservations).To(HaveLen(2))
		Expect(resp.BaselineBefore).To(BeEmpty())
		Expect(resp.BaselineAfter).To(BeEmpty())

		analysis := runCLIJSON[readmodel.PairedObservationAnalysis](dir, "analyze-pair", resp.PairID)
		Expect(analysis.Candidate.Summary.N).To(Equal(2))
		Expect(analysis.Baseline.Summary.N).To(Equal(2))
		Expect(analysis.Rows).To(HaveLen(4))
		Expect([]string{analysis.Rows[0].Arm, analysis.Rows[1].Arm, analysis.Rows[2].Arm, analysis.Rows[3].Arm}).To(Equal([]string{
			readmodel.PairArmBaseline,
			readmodel.PairArmCandidate,
			readmodel.PairArmBaseline,
			readmodel.PairArmCandidate,
		}))
		Expect(analysis.Rows[0].Samples).To(HaveLen(1))
	})
})

func setupPairedObservationScenario() (dir, baselineID, candidateExpID, candidateRef string) {
	GinkgoHelper()

	dir = setupObserveScenarioStore()
	registerPairedDriftInstrument(dir)
	registerScenarioSupportInstruments(dir)
	runCLIJSON[cliIDResponse](dir,
		"goal", "set",
		"--objective-instrument", "timing",
		"--objective-target", "kernel",
		"--objective-direction", "decrease",
		"--constraint-max", "binary_size=1000",
	)
	baseline := runCLIJSON[cliIDResponse](dir, "experiment", "baseline")
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
	ref := commitScenarioMetricsCandidate(impl.Worktree, "candidate/paired-drift", "candidate", "90\n", "900\n")
	return dir, baseline.ID, exp.ID, ref
}

func registerPairedDriftInstrument(dir string) {
	GinkgoHelper()
	runCLI(dir,
		"instrument", "register", "timing",
		"--cmd", "sh",
		"--cmd", "-c",
		"--cmd", `state="$PROJECT/drift-counter.txt"; drift=100; if test -f "$state"; then drift=$(cat "$state"); fi; base=$(cat timing.txt); echo $((base + drift)); echo $((drift + 20)) > "$state"`,
		"--parser", "builtin:scalar",
		"--pattern", "([0-9]+)",
		"--unit", "ns",
	)
}

var _ = Describe("paired observation readmodel", func() {
	It("ignores non-paired aux data", func() {
		_, ok := readmodel.ObservationPairMeta(&entity.Observation{Aux: map[string]any{"other": "value"}})
		Expect(ok).To(BeFalse())
	})
})
