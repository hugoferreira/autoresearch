package cli

import (
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type concludeResolutionJSON struct {
	RequestedObservations []string `json:"requested_observations"`
	UsedObservations      []string `json:"used_observations"`
	IgnoredObservations   []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	} `json:"ignored_observations"`
	CandidateExperiment string `json:"candidate_experiment"`
	CandidateAttempt    int    `json:"candidate_attempt,omitempty"`
	CandidateRef        string `json:"candidate_ref,omitempty"`
	CandidateSHA        string `json:"candidate_sha,omitempty"`
	CandidateSource     string `json:"candidate_source"`
	BaselineExperiment  string `json:"baseline_experiment,omitempty"`
	BaselineAttempt     int    `json:"baseline_attempt,omitempty"`
	BaselineRef         string `json:"baseline_ref,omitempty"`
	BaselineSHA         string `json:"baseline_sha,omitempty"`
	BaselineSource      string `json:"baseline_source"`
	BaselineNote        string `json:"baseline_note,omitempty"`
	AncestorHypothesis  string `json:"ancestor_hypothesis,omitempty"`
	AncestorConclusion  string `json:"ancestor_conclusion,omitempty"`
}

type concludeJSONResponse struct {
	ID         string                 `json:"id"`
	Conclusion entity.Conclusion      `json:"conclusion"`
	Resolution concludeResolutionJSON `json:"resolution"`
}

type concludeFixture struct {
	dir                 string
	goalID              string
	goalBaseline        string
	ancestorHypothesis  string
	ancestorExperiment  string
	ancestorConclusion  string
	currentHypothesis   string
	candidateExperiment string
	candidateRef        string
	candidateSHA        string
	timingObservation   string
	sizeObservation     string
}

func setupConcludeFallbackFixture() concludeFixture {
	GinkgoHelper()

	dir := GinkgoT().TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	Expect(err).NotTo(HaveOccurred())

	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	goal := &entity.Goal{
		ID:        "G-0001",
		Status:    entity.GoalStatusActive,
		CreatedAt: &now,
		Objective: entity.Objective{Instrument: "timing", Direction: "decrease"},
	}
	otherGoal := &entity.Goal{
		ID:        "G-0002",
		Status:    entity.GoalStatusActive,
		CreatedAt: &now,
		Objective: entity.Objective{Instrument: "timing", Direction: "decrease"},
	}
	for _, g := range []*entity.Goal{goal, otherGoal} {
		Expect(s.WriteGoal(g)).To(Succeed())
	}

	goalBaseline := &entity.Experiment{
		ID:          "E-0001",
		GoalID:      goal.ID,
		IsBaseline:  true,
		Status:      entity.ExpMeasured,
		Baseline:    entity.Baseline{Ref: "HEAD", SHA: "abc123"},
		Instruments: []string{"binary_size"},
		Author:      "system",
		CreatedAt:   now,
	}
	ancestorHyp := &entity.Hypothesis{
		ID:        "H-0001",
		GoalID:    goal.ID,
		Claim:     "ancestor",
		Status:    entity.StatusSupported,
		Author:    "agent:analyst",
		CreatedAt: now,
		Predicts: entity.Predicts{
			Instrument: "timing",
			Target:     "kernel",
			Direction:  "decrease",
			MinEffect:  0.05,
		},
	}
	currentHyp := &entity.Hypothesis{
		ID:        "H-0002",
		GoalID:    goal.ID,
		Parent:    ancestorHyp.ID,
		Claim:     "descendant",
		Status:    entity.StatusOpen,
		Author:    "agent:analyst",
		CreatedAt: now,
		Predicts: entity.Predicts{
			Instrument: "timing",
			Target:     "kernel",
			Direction:  "decrease",
			MinEffect:  0.05,
		},
	}
	ancestorExp := &entity.Experiment{
		ID:          "E-0002",
		GoalID:      goal.ID,
		Hypothesis:  ancestorHyp.ID,
		Status:      entity.ExpMeasured,
		Baseline:    entity.Baseline{Ref: "HEAD", SHA: "abc123"},
		Instruments: []string{"timing"},
		Author:      "agent:observer",
		CreatedAt:   now,
	}
	candidateExp := &entity.Experiment{
		ID:         "E-0003",
		GoalID:     goal.ID,
		Hypothesis: currentHyp.ID,
		Status:     entity.ExpMeasured,
		Baseline: entity.Baseline{
			Ref:        "HEAD",
			SHA:        "def456",
			Experiment: goalBaseline.ID,
		},
		Instruments: []string{"timing", "binary_size"},
		Author:      "agent:observer",
		CreatedAt:   now,
	}
	for _, h := range []*entity.Hypothesis{ancestorHyp, currentHyp} {
		Expect(s.WriteHypothesis(h)).To(Succeed())
	}
	for _, e := range []*entity.Experiment{goalBaseline, ancestorExp, candidateExp} {
		Expect(s.WriteExperiment(e)).To(Succeed())
	}

	writeObservation := func(id, expID, instrument string, value float64, perSample []float64, candidateRef, candidateSHA string) {
		GinkgoHelper()
		o := &entity.Observation{
			ID:           id,
			Experiment:   expID,
			Instrument:   instrument,
			MeasuredAt:   now,
			Value:        value,
			Samples:      len(perSample),
			PerSample:    perSample,
			Unit:         "ns",
			CandidateRef: candidateRef,
			CandidateSHA: candidateSHA,
			Author:       "agent:observer",
		}
		if candidateRef != "" || candidateSHA != "" {
			o.Attempt = 1
		}
		if instrument == "binary_size" {
			o.Unit = "bytes"
		}
		Expect(s.WriteObservation(o)).To(Succeed())
	}

	ancestorRef := "refs/heads/candidate/E-0002-a1"
	ancestorSHA := "1111111111111111111111111111111111111111"
	candidateRef := "refs/heads/candidate/E-0003-a1"
	candidateSHA := "2222222222222222222222222222222222222222"
	writeObservation("O-0001", goalBaseline.ID, "binary_size", 900, []float64{900, 900, 900, 900, 900}, "", "")
	writeObservation("O-0002", ancestorExp.ID, "timing", 100.4, []float64{100, 101, 99, 100, 102}, ancestorRef, ancestorSHA)
	writeObservation("O-0003", candidateExp.ID, "timing", 70.4, []float64{70, 71, 69, 72, 70}, candidateRef, candidateSHA)
	writeObservation("O-0004", candidateExp.ID, "binary_size", 860, []float64{860, 860, 860, 860, 860}, candidateRef, candidateSHA)

	ancestorConcl := &entity.Conclusion{
		ID:               "C-0001",
		Hypothesis:       ancestorHyp.ID,
		Verdict:          entity.VerdictSupported,
		Observations:     []string{"O-0002"},
		CandidateExp:     ancestorExp.ID,
		CandidateAttempt: 1,
		CandidateRef:     ancestorRef,
		CandidateSHA:     ancestorSHA,
		BaselineExp:      goalBaseline.ID,
		Effect: entity.Effect{
			Instrument: "timing",
			DeltaFrac:  -0.15,
			NCandidate: 5,
			NBaseline:  5,
		},
		StatTest:   "mann_whitney_u",
		Strict:     entity.Strict{Passed: true},
		Author:     "agent:analyst",
		ReviewedBy: "human:gate",
		CreatedAt:  now,
	}
	Expect(s.WriteConclusion(ancestorConcl)).To(Succeed())

	Expect(s.AppendEvent(store.Event{
		Ts:      now,
		Kind:    "experiment.baseline",
		Actor:   "system",
		Subject: goalBaseline.ID,
		Data:    jsonRaw(map[string]any{"goal": goal.ID}),
	})).To(Succeed())

	Expect(s.UpdateState(func(st *store.State) error {
		st.CurrentGoalID = otherGoal.ID
		st.Counters["G"] = 2
		st.Counters["H"] = 2
		st.Counters["E"] = 3
		st.Counters["O"] = 4
		st.Counters["C"] = 1
		return nil
	})).To(Succeed())

	return concludeFixture{
		dir:                 dir,
		goalID:              goal.ID,
		goalBaseline:        goalBaseline.ID,
		ancestorHypothesis:  ancestorHyp.ID,
		ancestorExperiment:  ancestorExp.ID,
		ancestorConclusion:  ancestorConcl.ID,
		currentHypothesis:   currentHyp.ID,
		candidateExperiment: candidateExp.ID,
		candidateRef:        candidateRef,
		candidateSHA:        candidateSHA,
		timingObservation:   "O-0003",
		sizeObservation:     "O-0004",
	}
}

var _ = Describe("conclude command", func() {
	BeforeEach(func() {
		saveGlobals()
	})

	Describe("resolution audit", func() {
		It("surfaces fallback resolution details in JSON and the event payload", func() {
			fx := setupConcludeFallbackFixture()

			resp := runCLIJSON[concludeJSONResponse](fx.dir,
				"conclude", fx.currentHypothesis,
				"--verdict", "supported",
				"--observations", strings.Join([]string{fx.timingObservation, fx.sizeObservation}, ","),
			)

			Expect(resp.Conclusion.CandidateExp).To(Equal(fx.candidateExperiment))
			Expect(resp.Conclusion.CandidateRef).To(Equal(fx.candidateRef))
			Expect(resp.Conclusion.CandidateSHA).To(Equal(fx.candidateSHA))
			Expect(resp.Conclusion.CandidateAttempt).To(Equal(1))
			Expect(resp.Conclusion.Observations).To(Equal([]string{fx.timingObservation}))
			Expect(resp.Resolution.CandidateRef).To(Equal(fx.candidateRef))
			Expect(resp.Resolution.CandidateSHA).To(Equal(fx.candidateSHA))
			Expect(resp.Resolution.CandidateAttempt).To(Equal(1))
			Expect(resp.Resolution.CandidateSource).To(Equal(concludeCandidateSourceObservations))
			Expect(resp.Resolution.UsedObservations).To(Equal([]string{fx.timingObservation}))
			Expect(resp.Resolution.RequestedObservations).To(Equal([]string{fx.timingObservation, fx.sizeObservation}))
			Expect(resp.Resolution.IgnoredObservations).To(ConsistOf(HaveField("ID", fx.sizeObservation)))
			Expect(resp.Resolution.BaselineExperiment).To(Equal(fx.ancestorExperiment))
			Expect(resp.Resolution.BaselineAttempt).To(Equal(1))
			Expect(resp.Resolution.BaselineSource).To(Equal(readmodel.BaselineSourceAncestorSupported))
			Expect(resp.Resolution.AncestorHypothesis).To(Equal(fx.ancestorHypothesis))
			Expect(resp.Resolution.AncestorConclusion).To(Equal(fx.ancestorConclusion))
			Expect(resp.Resolution.BaselineNote).To(ContainSubstring(fx.goalBaseline))

			s, err := store.Open(fx.dir)
			Expect(err).NotTo(HaveOccurred())
			written, err := s.ReadConclusion(resp.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(written.Observations).To(Equal([]string{fx.timingObservation}))

			e := findLastEvent(s, "conclusion.write")
			Expect(e).NotTo(BeNil())
			payload := decodePayload(e)
			Expect(payload).To(HaveKeyWithValue("candidate_source", concludeCandidateSourceObservations))
			Expect(payload).To(HaveKeyWithValue("candidate_ref", fx.candidateRef))
			Expect(payload).To(HaveKeyWithValue("candidate_sha", fx.candidateSHA))
			Expect(payload).To(HaveKeyWithValue("candidate_attempt", float64(1)))
			Expect(payload).To(HaveKeyWithValue("baseline_source", readmodel.BaselineSourceAncestorSupported))
			Expect(payload).To(HaveKeyWithValue("baseline_attempt", float64(1)))
			Expect(payload).To(HaveKeyWithValue("ancestor_hypothesis", fx.ancestorHypothesis))
			Expect(payload).To(HaveKeyWithValue("ancestor_conclusion", fx.ancestorConclusion))
			Expect(payload["observations"]).To(Equal([]any{fx.timingObservation}))
			Expect(payload["requested_observations"]).To(Equal([]any{fx.timingObservation, fx.sizeObservation}))
			Expect(payload["ignored_observations"]).To(ConsistOf(HaveKeyWithValue("id", fx.sizeObservation)))
		})

		It("surfaces fallback resolution details in text output", func() {
			fx := setupConcludeFallbackFixture()

			out := runCLI(fx.dir,
				"conclude", fx.currentHypothesis,
				"--verdict", "supported",
				"--observations", strings.Join([]string{fx.timingObservation, fx.sizeObservation}, ","),
			)

			expectText(out,
				"candidate:   "+fx.candidateExperiment+"  (source=observations",
				"candidate ref: "+fx.candidateRef,
				"candidate sha: "+fx.candidateSHA,
				"ignored:     "+fx.sizeObservation+" (instrument \"binary_size\" does not match predicted instrument \"timing\")",
				"baseline:    "+fx.ancestorExperiment+"  (n=5, source=ancestor_supported via "+fx.ancestorHypothesis+"/"+fx.ancestorConclusion+")",
				"baseline note: candidate recorded baseline "+fx.goalBaseline+" has no observations on instrument \"timing\"",
			)
		})

		It("keeps explicit baselines strict instead of falling back", func() {
			fx := setupConcludeFallbackFixture()

			_, _, err := runCLIResult(fx.dir,
				"conclude", fx.currentHypothesis,
				"--verdict", "supported",
				"--baseline-experiment", fx.goalBaseline,
				"--observations", strings.Join([]string{fx.timingObservation, fx.sizeObservation}, ","),
			)
			Expect(err).To(MatchError(ContainSubstring("baseline experiment " + fx.goalBaseline + " has no observations on instrument \"timing\"")))
		})
	})

	Describe("candidate observation scope", func() {
		It("refuses observations with mixed candidate provenance", func() {
			fx := setupConcludeFallbackFixture()

			s, err := store.Open(fx.dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(s.WriteObservation(&entity.Observation{
				ID:           "O-0005",
				Experiment:   fx.candidateExperiment,
				Instrument:   "timing",
				MeasuredAt:   time.Date(2026, 4, 19, 12, 1, 0, 0, time.UTC),
				Value:        69.8,
				Samples:      5,
				PerSample:    []float64{70, 70, 69, 70, 70},
				Unit:         "ns",
				CandidateRef: "refs/heads/candidate/E-0003-a2",
				CandidateSHA: "3333333333333333333333333333333333333333",
				Author:       "agent:observer",
			})).To(Succeed())

			_, _, err = runCLIResult(fx.dir,
				"conclude", fx.currentHypothesis,
				"--verdict", "supported",
				"--observations", strings.Join([]string{fx.timingObservation, "O-0005"}, ","),
			)
			Expect(err).To(MatchError(ContainSubstring("mix candidate scope")))
		})

		It("refuses observations from different attempts even when ref and SHA match", func() {
			fx := setupConcludeFallbackFixture()

			s, err := store.Open(fx.dir)
			Expect(err).NotTo(HaveOccurred())
			Expect(s.WriteObservation(&entity.Observation{
				ID:           "O-0006",
				Experiment:   fx.candidateExperiment,
				Instrument:   "timing",
				MeasuredAt:   time.Date(2026, 4, 19, 12, 2, 0, 0, time.UTC),
				Value:        69.9,
				Samples:      5,
				PerSample:    []float64{70, 70, 69, 70, 70},
				Unit:         "ns",
				Attempt:      2,
				CandidateRef: fx.candidateRef,
				CandidateSHA: fx.candidateSHA,
				Author:       "agent:observer",
			})).To(Succeed())

			_, _, err = runCLIResult(fx.dir,
				"conclude", fx.currentHypothesis,
				"--verdict", "supported",
				"--observations", strings.Join([]string{fx.timingObservation, "O-0006"}, ","),
			)
			Expect(err).To(MatchError(ContainSubstring("mix candidate scope")))
		})
	})

	Describe("rescuer comparisons", func() {
		It("uses the ancestor conclusion's observation scope for rescuer baselines", func() {
			dir := GinkgoT().TempDir()
			s, err := store.Create(dir, store.Config{
				Build: store.CommandSpec{Command: "true"},
				Test:  store.CommandSpec{Command: "true"},
			})
			Expect(err).NotTo(HaveOccurred())

			now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
			goal := &entity.Goal{
				ID:              "G-0001",
				Status:          entity.GoalStatusActive,
				CreatedAt:       &now,
				Objective:       entity.Objective{Instrument: "timing", Direction: "decrease"},
				NeutralBandFrac: 0.05,
				Rescuers: []entity.Rescuer{
					{Instrument: "binary_size", Direction: "decrease", MinEffect: 0.05},
				},
			}
			parent := &entity.Hypothesis{
				ID:        "H-0001",
				GoalID:    goal.ID,
				Claim:     "ancestor",
				Status:    entity.StatusSupported,
				Author:    "agent:analyst",
				CreatedAt: now,
				Predicts: entity.Predicts{
					Instrument: "timing",
					Target:     "kernel",
					Direction:  "decrease",
					MinEffect:  0.05,
				},
			}
			current := &entity.Hypothesis{
				ID:        "H-0002",
				GoalID:    goal.ID,
				Parent:    parent.ID,
				Claim:     "candidate",
				Status:    entity.StatusOpen,
				Author:    "agent:analyst",
				CreatedAt: now,
				Predicts: entity.Predicts{
					Instrument: "timing",
					Target:     "kernel",
					Direction:  "decrease",
					MinEffect:  0.05,
				},
			}
			ancestorExp := &entity.Experiment{
				ID:          "E-0001",
				GoalID:      goal.ID,
				Hypothesis:  parent.ID,
				Status:      entity.ExpMeasured,
				Attempt:     2,
				Baseline:    entity.Baseline{Ref: "HEAD", SHA: "abc123"},
				Instruments: []string{"timing", "binary_size"},
				Author:      "agent:observer",
				CreatedAt:   now,
			}
			candidateExp := &entity.Experiment{
				ID:          "E-0002",
				GoalID:      goal.ID,
				Hypothesis:  current.ID,
				Status:      entity.ExpMeasured,
				Attempt:     2,
				Baseline:    entity.Baseline{Ref: "HEAD", SHA: "abc123"},
				Instruments: []string{"timing", "binary_size"},
				Author:      "agent:observer",
				CreatedAt:   now,
			}
			Expect(s.WriteGoal(goal)).To(Succeed())
			for _, h := range []*entity.Hypothesis{parent, current} {
				Expect(s.WriteHypothesis(h)).To(Succeed())
			}
			for _, e := range []*entity.Experiment{ancestorExp, candidateExp} {
				Expect(s.WriteExperiment(e)).To(Succeed())
			}

			writeObs := func(id, expID, instrument string, value float64, attempt int, ref, sha string) {
				GinkgoHelper()
				perSample := []float64{value, value, value, value, value}
				unit := "ns"
				if instrument == "binary_size" {
					unit = "bytes"
				}
				Expect(s.WriteObservation(&entity.Observation{
					ID:           id,
					Experiment:   expID,
					Instrument:   instrument,
					MeasuredAt:   now,
					Value:        value,
					Samples:      len(perSample),
					PerSample:    perSample,
					Unit:         unit,
					Attempt:      attempt,
					CandidateRef: ref,
					CandidateSHA: sha,
					Author:       "agent:observer",
				})).To(Succeed())
			}

			ancestorScopeA := readmodel.ObservationScope{
				Experiment: ancestorExp.ID,
				Attempt:    1,
				Ref:        "refs/heads/candidate/E-0001-a1",
				SHA:        "1111111111111111111111111111111111111111",
			}
			ancestorScopeB := readmodel.ObservationScope{
				Experiment: ancestorExp.ID,
				Attempt:    2,
				Ref:        "refs/heads/candidate/E-0001-a2",
				SHA:        "2222222222222222222222222222222222222222",
			}
			currentScopeA := readmodel.ObservationScope{
				Experiment: candidateExp.ID,
				Attempt:    1,
				Ref:        "refs/heads/candidate/E-0002-a1",
				SHA:        "3333333333333333333333333333333333333333",
			}
			currentScopeB := readmodel.ObservationScope{
				Experiment: candidateExp.ID,
				Attempt:    2,
				Ref:        "refs/heads/candidate/E-0002-a2",
				SHA:        "4444444444444444444444444444444444444444",
			}

			writeObs("O-0001", ancestorExp.ID, "timing", 100, ancestorScopeA.Attempt, ancestorScopeA.Ref, ancestorScopeA.SHA)
			writeObs("O-0002", ancestorExp.ID, "binary_size", 1000, ancestorScopeA.Attempt, ancestorScopeA.Ref, ancestorScopeA.SHA)
			writeObs("O-0003", ancestorExp.ID, "timing", 95, ancestorScopeB.Attempt, ancestorScopeB.Ref, ancestorScopeB.SHA)
			writeObs("O-0004", ancestorExp.ID, "binary_size", 800, ancestorScopeB.Attempt, ancestorScopeB.Ref, ancestorScopeB.SHA)
			writeObs("O-0005", candidateExp.ID, "timing", 100, currentScopeA.Attempt, currentScopeA.Ref, currentScopeA.SHA)
			writeObs("O-0006", candidateExp.ID, "binary_size", 900, currentScopeA.Attempt, currentScopeA.Ref, currentScopeA.SHA)
			writeObs("O-0007", candidateExp.ID, "timing", 70, currentScopeB.Attempt, currentScopeB.Ref, currentScopeB.SHA)
			writeObs("O-0008", candidateExp.ID, "binary_size", 1200, currentScopeB.Attempt, currentScopeB.Ref, currentScopeB.SHA)

			Expect(s.WriteConclusion(&entity.Conclusion{
				ID:               "C-0001",
				Hypothesis:       parent.ID,
				Verdict:          entity.VerdictSupported,
				ReviewedBy:       "human:gate",
				Observations:     []string{"O-0001"},
				CandidateExp:     ancestorExp.ID,
				CandidateAttempt: ancestorScopeA.Attempt,
				CandidateRef:     ancestorScopeA.Ref,
				CandidateSHA:     ancestorScopeA.SHA,
				Effect: entity.Effect{
					Instrument: "timing",
					DeltaFrac:  -0.10,
					NCandidate: 5,
					NBaseline:  5,
				},
				StatTest:  "mann_whitney_u",
				Strict:    entity.Strict{Passed: true},
				Author:    "agent:analyst",
				CreatedAt: now,
			})).To(Succeed())

			resp := runCLIJSON[concludeJSONResponse](dir,
				"conclude", current.ID,
				"--verdict", "supported",
				"--observations", "O-0005",
			)
			Expect(resp.Conclusion.Verdict).To(Equal(entity.VerdictSupported))
			Expect(resp.Conclusion.Strict.RescuedBy).To(Equal("binary_size"))
			Expect(resp.Resolution.BaselineExperiment).To(Equal(ancestorExp.ID))
			Expect(resp.Resolution.BaselineAttempt).To(Equal(ancestorScopeA.Attempt))
			Expect(resp.Resolution.BaselineRef).To(Equal(ancestorScopeA.Ref))
			Expect(resp.Resolution.BaselineSHA).To(Equal(ancestorScopeA.SHA))
		})
	})
})
