package readmodel

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func observationIndexForTest(m map[string][]*entity.Observation) *ObservationIndex {
	var all []*entity.Observation
	for expID, obs := range m {
		for _, o := range obs {
			if o == nil {
				continue
			}
			if o.Experiment == expID {
				all = append(all, o)
				continue
			}
			copyObs := *o
			copyObs.Experiment = expID
			all = append(all, &copyObs)
		}
	}
	return NewObservationIndex(all)
}

var _ = Describe("frontier read models", func() {
	It("counts reviewed supported candidates after they have been accepted", func() {
		goal := &entity.Goal{
			Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
			Completion: &entity.Completion{
				Threshold:   0.2,
				OnThreshold: entity.GoalOnThresholdStop,
			},
		}
		concls := []*entity.Conclusion{
			{
				ID:           "C-3000",
				Hypothesis:   "H-3000",
				Verdict:      entity.VerdictSupported,
				ReviewedBy:   "agent:gate",
				CandidateExp: "E-3000",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.3},
			},
		}
		obsByExp := map[string][]*entity.Observation{
			"E-3000": {{Instrument: "host_timing", Value: 0.70}},
		}
		expClassByID := map[string]ExperimentReadClass{
			"E-3000": {
				Classification:   ExperimentClassificationDead,
				HypothesisStatus: entity.StatusSupported,
			},
		}

		got := AssessGoalCompletion(goal, concls, observationIndexForTest(obsByExp), expClassByID)
		Expect(got.Met).To(BeTrue())
		Expect(got.MetByConclusion).To(Equal("C-3000"))
		Expect(got.RecommendedAction).To(Equal("stop"))
	})

	It("increments stalled-for using later conclusions after the accepted win", func() {
		goal := &entity.Goal{
			Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
		}
		base := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
		concls := []*entity.Conclusion{
			{
				ID:           "C-0001",
				Hypothesis:   "H-0001",
				Verdict:      entity.VerdictSupported,
				CandidateExp: "E-0001",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.10},
				CreatedAt:    base,
			},
			{
				ID:           "C-0002",
				Hypothesis:   "H-0002",
				Verdict:      entity.VerdictInconclusive,
				CandidateExp: "E-0002",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.09},
				CreatedAt:    base.Add(1 * time.Minute),
			},
			{
				ID:           "C-0003",
				Hypothesis:   "H-0003",
				Verdict:      entity.VerdictRefuted,
				CandidateExp: "E-0003",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.20},
				CreatedAt:    base.Add(2 * time.Minute),
			},
		}
		obsByExp := map[string][]*entity.Observation{
			"E-0001": {{Instrument: "host_timing", Value: 100}},
			"E-0002": {{Instrument: "host_timing", Value: 101}},
			"E-0003": {{Instrument: "host_timing", Value: 90}},
		}

		rows, stalled := ComputeFrontierFromObservations(goal, concls, observationIndexForTest(obsByExp), map[string]ExperimentReadClass{
			"E-0001": {
				Classification:   ExperimentClassificationDead,
				HypothesisStatus: entity.StatusSupported,
			},
		})
		Expect(stalled).To(Equal(2))
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].Candidate).To(Equal("E-0001"))
		Expect(rows[0].Classification).To(Equal(ExperimentClassificationDead))
	})

	It("composes rows, goal assessment, and stalled count in one snapshot", func() {
		goal := &entity.Goal{
			Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
			Completion: &entity.Completion{
				Threshold:   0.1,
				OnThreshold: entity.GoalOnThresholdStop,
			},
		}
		concls := []*entity.Conclusion{
			{
				ID:           "C-0001",
				Hypothesis:   "H-0001",
				Verdict:      entity.VerdictSupported,
				ReviewedBy:   "agent:gate",
				CandidateExp: "E-0001",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.15},
			},
		}
		obs := []*entity.Observation{
			{Experiment: "E-0001", Instrument: "host_timing", Value: 85},
		}
		expClassByID := map[string]ExperimentReadClass{
			"E-0001": {
				Classification:   ExperimentClassificationDead,
				HypothesisStatus: entity.StatusSupported,
			},
		}

		got := BuildFrontierSnapshot(goal, concls, NewObservationIndex(obs), expClassByID)
		Expect(got.StalledFor).To(Equal(0))
		Expect(got.Rows).To(HaveLen(1))
		Expect(got.Rows[0].Candidate).To(Equal("E-0001"))
		Expect(got.Rows[0].Classification).To(Equal(ExperimentClassificationDead))
		Expect(got.Assessment.Met).To(BeTrue())
		Expect(got.Assessment.MetByConclusion).To(Equal("C-0001"))
	})

	It("uses each conclusion's candidate scope when ranking repeated attempts", func() {
		goal := &entity.Goal{
			Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
		}
		concls := []*entity.Conclusion{
			{
				ID:               "C-0001",
				Hypothesis:       "H-0001",
				Verdict:          entity.VerdictSupported,
				Observations:     []string{"O-a1"},
				CandidateExp:     "E-0001",
				CandidateAttempt: 1,
				CandidateRef:     "refs/heads/candidate/E-0001-a1",
				CandidateSHA:     "1111111111111111111111111111111111111111",
				Effect:           entity.Effect{Instrument: "host_timing", DeltaFrac: -0.10},
			},
			{
				ID:               "C-0002",
				Hypothesis:       "H-0002",
				Verdict:          entity.VerdictSupported,
				Observations:     []string{"O-a2"},
				CandidateExp:     "E-0001",
				CandidateAttempt: 2,
				CandidateRef:     "refs/heads/candidate/E-0001-a2",
				CandidateSHA:     "2222222222222222222222222222222222222222",
				Effect:           entity.Effect{Instrument: "host_timing", DeltaFrac: -0.20},
			},
		}
		obs := NewObservationIndex([]*entity.Observation{
			{
				ID:           "O-a2",
				Experiment:   "E-0001",
				Instrument:   "host_timing",
				Value:        80,
				Attempt:      2,
				CandidateRef: "refs/heads/candidate/E-0001-a2",
				CandidateSHA: "2222222222222222222222222222222222222222",
			},
			{
				ID:           "O-a1",
				Experiment:   "E-0001",
				Instrument:   "host_timing",
				Value:        100,
				Attempt:      1,
				CandidateRef: "refs/heads/candidate/E-0001-a1",
				CandidateSHA: "1111111111111111111111111111111111111111",
			},
		})

		rows, _ := ComputeFrontierFromObservations(goal, concls, obs, nil)
		Expect(rows).To(HaveLen(2))
		Expect(rows[0].Conclusion).To(Equal("C-0002"))
		Expect(rows[0].Value).To(Equal(80.0))
		Expect(rows[1].Value).To(Equal(100.0))
	})

	It("uses the reviewed conclusion scope when deciding whether a goal is met", func() {
		goal := &entity.Goal{
			Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
			Completion: &entity.Completion{
				Threshold:   0.15,
				OnThreshold: entity.GoalOnThresholdStop,
			},
		}
		concls := []*entity.Conclusion{
			{
				ID:               "C-0001",
				Hypothesis:       "H-0001",
				Verdict:          entity.VerdictSupported,
				ReviewedBy:       "agent:gate",
				Observations:     []string{"O-a1"},
				CandidateExp:     "E-0001",
				CandidateAttempt: 1,
				CandidateRef:     "refs/heads/candidate/E-0001-a1",
				CandidateSHA:     "1111111111111111111111111111111111111111",
				Effect:           entity.Effect{Instrument: "host_timing", DeltaFrac: -0.10},
			},
			{
				ID:               "C-0002",
				Hypothesis:       "H-0002",
				Verdict:          entity.VerdictSupported,
				ReviewedBy:       "agent:gate",
				Observations:     []string{"O-a2"},
				CandidateExp:     "E-0001",
				CandidateAttempt: 2,
				CandidateRef:     "refs/heads/candidate/E-0001-a2",
				CandidateSHA:     "2222222222222222222222222222222222222222",
				Effect:           entity.Effect{Instrument: "host_timing", DeltaFrac: -0.20},
			},
		}
		obs := NewObservationIndex([]*entity.Observation{
			{
				ID:           "O-a1",
				Experiment:   "E-0001",
				Instrument:   "host_timing",
				Value:        100,
				Attempt:      1,
				CandidateRef: "refs/heads/candidate/E-0001-a1",
				CandidateSHA: "1111111111111111111111111111111111111111",
			},
			{
				ID:           "O-a2",
				Experiment:   "E-0001",
				Instrument:   "host_timing",
				Value:        80,
				Attempt:      2,
				CandidateRef: "refs/heads/candidate/E-0001-a2",
				CandidateSHA: "2222222222222222222222222222222222222222",
			},
		})

		got := AssessGoalCompletion(goal, concls, obs, nil)
		Expect(got.Met).To(BeTrue())
		Expect(got.MetByConclusion).To(Equal("C-0002"))
		Expect(got.RecommendedAction).To(Equal("stop"))
	})
})
