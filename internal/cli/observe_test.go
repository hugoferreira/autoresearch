package cli

import (
	"os"
	"path/filepath"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type observeRecordJSON struct {
	Action       string               `json:"action"`
	ID           string               `json:"id"`
	SamplesAdded int                  `json:"samples_added"`
	Observation  entity.Observation   `json:"observation"`
	Observations []entity.Observation `json:"observations"`
}

type observeCheckJSON struct {
	Check observeSampleCheck `json:"check"`
}

func timingSampleTotal(observations []*entity.Observation) int {
	return samplesForObservedInstrument(store.Instrument{Parser: "builtin:scalar"}, observations, "timing")
}

func setupObserveFixture() (string, *store.Store) {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	Expect(os.WriteFile(filepath.Join(dir, "timing.txt"), []byte("100\n"), 0o644)).To(Succeed())
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		gitRun(dir, args...)
	}
	gitRun(dir, "add", "timing.txt")
	gitRun(dir, "commit", "-m", "init")

	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
		Instruments: map[string]store.Instrument{
			"timing": {
				Cmd:        []string{"sh", "-c", "cat timing.txt"},
				Parser:     "builtin:scalar",
				Pattern:    "([0-9]+)",
				Unit:       "ns",
				MinSamples: 5,
			},
		},
	})
	Expect(err).NotTo(HaveOccurred())

	now := time.Now().UTC()
	exp := &entity.Experiment{
		ID:          "E-0001",
		GoalID:      "G-0001",
		Hypothesis:  "H-0001",
		Status:      entity.ExpImplemented,
		Baseline:    entity.Baseline{Ref: "HEAD"},
		Attempt:     1,
		Instruments: []string{"timing"},
		Worktree:    dir,
		Author:      "human:test",
		CreatedAt:   now,
	}
	Expect(s.WriteExperiment(exp)).To(Succeed())
	Expect(s.UpdateState(func(st *store.State) error {
		st.Counters["E"] = 1
		return nil
	})).To(Succeed())
	return dir, s
}

func observeFixtureObservations(s *store.Store, expID string) []*entity.Observation {
	GinkgoHelper()
	obs, err := s.ListObservationsForExperiment(expID)
	Expect(err).NotTo(HaveOccurred())
	return obs
}

var _ = Describe("observe command", func() {
	BeforeEach(func() {
		saveGlobals()
	})

	Describe("sample accounting", func() {
		var (
			dir          string
			s            *store.Store
			candidateRef string
		)

		BeforeEach(func() {
			dir, s = setupObserveFixture()
			candidateRef = gitCreateCandidateRef(dir, "candidate/e-0001-a1")
		})

		It("skips recording when existing samples already satisfy the target", func() {
			first := runCLIJSON[observeRecordJSON](dir, "observe", "E-0001", "--instrument", "timing", "--candidate-ref", candidateRef)
			Expect(first.Observation.Samples).To(Equal(5))

			out := runCLI(dir, "observe", "E-0001", "--instrument", "timing", "--candidate-ref", candidateRef)
			expectText(out, "observation already satisfied", "have 5 samples", "--append")

			obs := observeFixtureObservations(s, "E-0001")
			Expect(obs).To(HaveLen(1))
			Expect(timingSampleTotal(obs)).To(Equal(5))
		})

		It("tops up only the samples needed to reach an explicit target", func() {
			runCLIJSON[observeRecordJSON](dir, "observe", "E-0001", "--instrument", "timing", "--candidate-ref", candidateRef)
			resp := runCLIJSON[observeRecordJSON](dir, "observe", "E-0001", "--instrument", "timing", "--candidate-ref", candidateRef, "--samples", "7")

			Expect(resp.Action).To(Equal("recorded"))
			Expect(resp.SamplesAdded).To(Equal(2))
			Expect(resp.Observation.Samples).To(Equal(2))
			Expect(resp.Observations).To(HaveLen(1))

			obs := observeFixtureObservations(s, "E-0001")
			Expect(obs).To(HaveLen(2))
			Expect(timingSampleTotal(obs)).To(Equal(7))

			exp, err := s.ReadExperiment("E-0001")
			Expect(err).NotTo(HaveOccurred())
			Expect(exp.Status).To(Equal(entity.ExpMeasured))
		})

		It("appends a full additional run when requested", func() {
			runCLIJSON[observeRecordJSON](dir, "observe", "E-0001", "--instrument", "timing", "--candidate-ref", candidateRef)
			resp := runCLIJSON[observeRecordJSON](dir, "observe", "E-0001", "--instrument", "timing", "--candidate-ref", candidateRef, "--append")

			Expect(resp.SamplesAdded).To(Equal(5))
			Expect(resp.Observation.Samples).To(Equal(5))

			obs := observeFixtureObservations(s, "E-0001")
			Expect(obs).To(HaveLen(2))
			Expect(timingSampleTotal(obs)).To(Equal(10))
		})

		It("reports current, minimum, and requested sample counts from observe check", func() {
			runCLIJSON[observeRecordJSON](dir, "observe", "E-0001", "--instrument", "timing", "--candidate-ref", candidateRef)
			resp := runCLIJSON[observeCheckJSON](dir, "observe", "check", "E-0001", "--instrument", "timing", "--candidate-ref", candidateRef, "--samples", "7")

			Expect(resp.Check.CurrentSamples).To(Equal(5))
			Expect(resp.Check.MinSamples).To(Equal(5))
			Expect(resp.Check.MinSatisfied).To(BeTrue())
			Expect(resp.Check.TargetSamples).To(Equal(7))
			Expect(resp.Check.TargetSource).To(Equal("requested"))
			Expect(resp.Check.TargetSatisfied).To(BeFalse())
			Expect(resp.Check.AdditionalSamples).To(Equal(2))
		})
	})

	Describe("candidate identity", func() {
		It("requires candidate refs for non-baseline observations and checks", func() {
			dir, _ := setupObserveFixture()

			_, _, err := runCLIResult(dir, "observe", "E-0001", "--instrument", "timing")
			Expect(err).To(MatchError(ContainSubstring("requires --candidate-ref")))

			_, _, err = runCLIResult(dir, "observe", "check", "E-0001", "--instrument", "timing")
			Expect(err).To(MatchError(ContainSubstring("requires --candidate-ref")))
		})

		Describe("recorded candidate scopes", func() {
			var (
				dir      string
				scenario observeScenarioExperiment
			)

			BeforeEach(func() {
				dir, scenario = setupTimingObserveScenario()
			})

			It("ignores observations from abandoned reset attempts", func() {
				candidateRef1 := gitCreateCandidateRef(scenario.Worktree, "candidate/reset-a1")
				first := runCLIJSON[observeRecordJSON](dir,
					"observe", scenario.ExpID,
					"--instrument", "timing",
					"--candidate-ref", candidateRef1,
					"--allow-unchanged",
				)
				Expect(first.ID).NotTo(BeEmpty())

				runCLI(dir, "experiment", "reset", scenario.ExpID, "--reason", "retry measurement")
				impl2 := runCLIJSON[cliImplementResponse](dir, "experiment", "implement", scenario.ExpID)
				candidateRef2 := gitCreateCandidateRef(impl2.Worktree, "candidate/reset-a2")

				check := runCLIJSON[observeCheckJSON](dir,
					"observe", "check", scenario.ExpID,
					"--instrument", "timing",
					"--candidate-ref", candidateRef2,
				)
				Expect(check.Check.CurrentSamples).To(Equal(0))
				Expect(check.Check.TargetSatisfied).To(BeFalse())

				second := runCLIJSON[observeRecordJSON](dir,
					"observe", scenario.ExpID,
					"--instrument", "timing",
					"--candidate-ref", candidateRef2,
					"--allow-unchanged",
				)
				Expect(second.ID).NotTo(Equal(first.ID))

				s, err := store.Open(dir)
				Expect(err).NotTo(HaveOccurred())
				expEntity, err := s.ReadExperiment(scenario.ExpID)
				Expect(err).NotTo(HaveOccurred())
				Expect(expEntity.Attempt).To(Equal(2))
			})

			It("ignores observations when the candidate commit changes", func() {
				candidateRefA := commitScenarioMetricsCandidate(scenario.Worktree, "candidate/commit-a", "candidate a", "90\n", "900\n")
				runCLIJSON[observeRecordJSON](dir, "observe", scenario.ExpID, "--instrument", "timing", "--candidate-ref", candidateRefA)

				candidateRefB := commitScenarioMetricsCandidate(scenario.Worktree, "candidate/commit-b", "candidate b", "85\n", "900\n")

				check := runCLIJSON[observeCheckJSON](dir,
					"observe", "check", scenario.ExpID,
					"--instrument", "timing",
					"--candidate-ref", candidateRefB,
				)
				Expect(check.Check.CurrentSamples).To(Equal(0))
				Expect(check.Check.TargetSatisfied).To(BeFalse())
			})

			It("does not reuse observations recorded under a different candidate ref, even for the same SHA", func() {
				candidateRefA := commitScenarioMetricsCandidate(scenario.Worktree, "candidate/ref-a", "candidate a", "90\n", "900\n")
				runCLIJSON[observeRecordJSON](dir, "observe", scenario.ExpID, "--instrument", "timing", "--candidate-ref", candidateRefA)
				candidateRefB := gitCreateCandidateRef(scenario.Worktree, "candidate/ref-b")

				check := runCLIJSON[observeCheckJSON](dir,
					"observe", "check", scenario.ExpID,
					"--instrument", "timing",
					"--candidate-ref", candidateRefB,
				)
				Expect(check.Check.CurrentSamples).To(Equal(0))
				Expect(check.Check.TargetSatisfied).To(BeFalse())
			})

			It("refuses measurement when HEAD no longer matches the supplied candidate ref", func() {
				candidateRef := commitScenarioMetricsCandidate(scenario.Worktree, "candidate/mismatch-a", "candidate a", "90\n", "900\n")
				commitScenarioMetricsCandidate(scenario.Worktree, "candidate/mismatch-b", "candidate b", "85\n", "900\n")

				_, _, err := runCLIResult(dir,
					"observe", scenario.ExpID,
					"--instrument", "timing",
					"--candidate-ref", candidateRef,
				)
				Expect(err).To(MatchError(ContainSubstring("does not match --candidate-ref")))
			})
		})
	})

	Describe("worktree safety", func() {
		It("refuses observe and observe check while the experiment worktree is dirty", func() {
			dir, scenario := setupTimingObserveScenario()
			candidateRef := commitScenarioMetricsCandidate(scenario.Worktree, "candidate/dirty-a", "candidate a", "90\n", "900\n")
			first := runCLIJSON[observeRecordJSON](dir, "observe", scenario.ExpID, "--instrument", "timing", "--candidate-ref", candidateRef)

			writeScenarioMetrics(scenario.Worktree, "80\n", "900\n")

			_, _, err := runCLIResult(dir,
				"observe", "check", scenario.ExpID,
				"--instrument", "timing",
				"--candidate-ref", candidateRef,
			)
			Expect(err).To(MatchError(ContainSubstring("has uncommitted changes")))

			_, _, err = runCLIResult(dir,
				"observe", scenario.ExpID,
				"--instrument", "timing",
				"--candidate-ref", candidateRef,
			)
			Expect(err).To(MatchError(ContainSubstring("has uncommitted changes")))

			s, err := store.Open(dir)
			Expect(err).NotTo(HaveOccurred())
			obs := observeFixtureObservations(s, scenario.ExpID)
			Expect(obs).To(HaveLen(1))
			Expect(obs[0].ID).To(Equal(first.ID))
		})
	})

	Describe("observe --all", func() {
		It("returns the current observation set when a rerun is fully idempotent", func() {
			dir := setupObserveScenarioStore()
			registerScenarioInstruments(dir)
			scenario := setupObserveScenarioExperiment(dir, "timing,binary_size,host_test",
				"--constraint-max", "binary_size=1000",
				"--constraint-require", "host_test=pass",
			)
			candidateRef := commitScenarioMetricsCandidate(scenario.Worktree, "candidate/all-a", "candidate", "80\n", "900\n")

			first := runCLIJSON[cliObserveAllResponse](dir, "observe", scenario.ExpID, "--all", "--candidate-ref", candidateRef)
			Expect(first.Observations).To(HaveLen(3))
			Expect(first.NewObservations).To(HaveLen(3))
			Expect(first.ReusedObservations).To(BeEmpty())

			second := runCLIJSON[cliObserveAllResponse](dir, "observe", scenario.ExpID, "--all", "--candidate-ref", candidateRef)
			Expect(second.Action).To(Equal(observeActionSkipped))
			Expect(second.Observations).To(HaveLen(3))
			Expect(second.NewObservations).To(BeEmpty())
			Expect(second.ReusedObservations).To(HaveLen(3))
		})

		It("reruns failed prerequisites instead of skipping only by sample count", func() {
			dir := setupObserveScenarioStore()
			runCLI(dir,
				"instrument", "register", "timing",
				"--cmd", "sh",
				"--cmd", "-c",
				"--cmd", "cat timing.txt",
				"--parser", "builtin:scalar",
				"--pattern", "([0-9]+)",
				"--unit", "ns",
				"--requires", "host_test=pass",
			)
			runCLI(dir,
				"instrument", "register", "host_test",
				"--cmd", "sh",
				"--cmd", "-c",
				"--cmd", "test -f PASS",
				"--parser", "builtin:passfail",
				"--unit", "bool",
			)
			scenario := setupObserveScenarioExperiment(dir, "timing,host_test", "--constraint-require", "host_test=pass")
			writeScenarioMetrics(scenario.Worktree, "80\n", "900\n")
			Expect(os.Remove(filepath.Join(scenario.Worktree, "PASS"))).To(Succeed())
			gitRun(scenario.Worktree, "add", "timing.txt", "PASS")
			gitRun(scenario.Worktree, "commit", "-m", "candidate without pass marker")
			failingRef := gitCreateCandidateRef(scenario.Worktree, "candidate/prereq-fail")

			_, _, err := runCLIResult(dir, "observe", scenario.ExpID, "--all", "--candidate-ref", failingRef)
			Expect(err).To(MatchError(ContainSubstring("stuck: instruments [timing] have unsatisfied dependencies")))

			Expect(os.WriteFile(filepath.Join(scenario.Worktree, "PASS"), []byte("ok\n"), 0o644)).To(Succeed())
			gitRun(scenario.Worktree, "add", "PASS")
			gitRun(scenario.Worktree, "commit", "-m", "restore pass marker")
			recoveryRef := gitCreateCandidateRef(scenario.Worktree, "candidate/prereq-pass")
			resp := runCLIJSON[cliObserveAllResponse](dir, "observe", scenario.ExpID, "--all", "--candidate-ref", recoveryRef)

			recorded := map[string]bool{}
			for _, result := range resp.Results {
				recorded[result.Inst] = result.Action == observeActionRecorded
			}
			Expect(recorded).To(HaveKeyWithValue("host_test", true))
			Expect(recorded).To(HaveKeyWithValue("timing", true))
		})
	})
})
