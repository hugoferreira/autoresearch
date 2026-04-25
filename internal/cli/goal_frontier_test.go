package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func observationIndexForFrontierTest(m map[string][]*entity.Observation) *readmodel.ObservationIndex {
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
	return readmodel.NewObservationIndex(all)
}

var _ = Describe("goal frontier rules", func() {
	Describe("building goals from flags", func() {
		It("defaults threshold completion to ask_human", func() {
			g, err := buildGoalFromFlags(
				"host_timing", "dsp_fir", "decrease",
				0.2, "",
				[]string{"size_flash=131072"}, nil, nil, nil,
				0, "",
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(g.Completion).NotTo(BeNil())
			Expect(g.Completion.OnThreshold).To(Equal(entity.GoalOnThresholdAskHuman))
		})

		DescribeTable("rejects invalid completion and rescuer flag combinations",
			func(threshold float64, onThreshold string, rescuers []string, neutralBand float64) {
				_, err := buildGoalFromFlags(
					"host_timing", "dsp_fir", "decrease",
					threshold, onThreshold,
					[]string{"size_flash=131072"}, nil, nil, rescuers,
					neutralBand, "",
				)
				Expect(err).To(HaveOccurred())
			},
			Entry("on-success without threshold", 0.0, entity.GoalOnThresholdStop, nil, 0.0),
			Entry("negative threshold", -0.1, "", nil, 0.0),
			Entry("rescuer without neutral band", 0.0, "", []string{"sim_total_bytes:decrease:0.02"}, 0.0),
			Entry("neutral band without rescuer", 0.0, "", nil, 0.02),
		)

		It("accepts a rescuer when a neutral band is configured", func() {
			g, err := buildGoalFromFlags(
				"host_timing", "dsp_fir", "decrease",
				0, "",
				[]string{"size_flash=131072"}, nil, nil,
				[]string{"sim_total_bytes:decrease:0.02"},
				0.02, "",
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(g.Rescuers).To(HaveLen(1))
			Expect(g.Rescuers[0].Instrument).To(Equal("sim_total_bytes"))
			Expect(g.Rescuers[0].Direction).To(Equal("decrease"))
			Expect(g.Rescuers[0].MinEffect).To(Equal(0.02))
			Expect(g.NeutralBandFrac).To(Equal(0.02))
		})
	})

	It("uses rescuers as a tiebreak only inside the neutral band", func() {
		goal := &entity.Goal{
			Objective:       entity.Objective{Instrument: "ns_per_eval", Direction: "decrease"},
			NeutralBandFrac: 0.02,
			Rescuers: []entity.Rescuer{
				{Instrument: "sim_total_bytes", Direction: "decrease", MinEffect: 0.02},
			},
		}

		a := frontierRow{Value: 100.0, TiebreakValues: []float64{512}}
		b := frontierRow{Value: 100.5, TiebreakValues: []float64{600}}
		Expect(readmodel.FrontierRowBetter(goal, a, b)).To(BeTrue())
		Expect(readmodel.FrontierRowBetter(goal, b, a)).To(BeFalse())

		c := frontierRow{Value: 80.0, TiebreakValues: []float64{9999}}
		d := frontierRow{Value: 100.0, TiebreakValues: []float64{10}}
		Expect(readmodel.FrontierRowBetter(goal, c, d)).To(BeTrue())
	})

	Describe("goal completion assessment", func() {
		It("continues open-ended goals", func() {
			goal := &entity.Goal{Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"}}
			got := readmodel.AssessGoalCompletion(goal, nil, nil, nil)
			Expect(got.Mode).To(Equal("open_ended"))
			Expect(got.Met).To(BeFalse())
			Expect(got.RecommendedAction).To(Equal("continue"))
		})

		It("marks a thresholded decrease goal met by a reviewed supported candidate", func() {
			goal := &entity.Goal{
				Objective:  entity.Objective{Instrument: "host_timing", Direction: "decrease"},
				Completion: &entity.Completion{Threshold: 0.2, OnThreshold: entity.GoalOnThresholdAskHuman},
			}
			concls := []*entity.Conclusion{{
				ID:           "C-0001",
				Hypothesis:   "H-0001",
				Verdict:      entity.VerdictSupported,
				ReviewedBy:   "agent:gate",
				CandidateExp: "E-0001",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.25},
			}}
			got := readmodel.AssessGoalCompletion(goal, concls, observationIndexForFrontierTest(map[string][]*entity.Observation{
				"E-0001": {{Instrument: "host_timing", Value: 0.75}},
			}), nil)
			Expect(got.Met).To(BeTrue())
			Expect(got.MetByConclusion).To(Equal("C-0001"))
			Expect(got.RecommendedAction).To(Equal("ask_human"))
		})

		It("chooses the best reviewed candidate over a better unreviewed one", func() {
			goal := &entity.Goal{
				Objective:  entity.Objective{Instrument: "host_timing", Direction: "decrease"},
				Completion: &entity.Completion{Threshold: 0.15, OnThreshold: entity.GoalOnThresholdStop},
			}
			concls := []*entity.Conclusion{
				{ID: "C-0001", Hypothesis: "H-0001", Verdict: entity.VerdictSupported, CandidateExp: "E-0001", Effect: entity.Effect{Instrument: "host_timing", DeltaFrac: -0.30}},
				{ID: "C-0002", Hypothesis: "H-0002", Verdict: entity.VerdictSupported, ReviewedBy: "agent:gate", CandidateExp: "E-0002", Effect: entity.Effect{Instrument: "host_timing", DeltaFrac: -0.18}},
			}
			got := readmodel.AssessGoalCompletion(goal, concls, observationIndexForFrontierTest(map[string][]*entity.Observation{
				"E-0001": {{Instrument: "host_timing", Value: 0.70}},
				"E-0002": {{Instrument: "host_timing", Value: 0.82}},
			}), nil)
			Expect(got.Met).To(BeTrue())
			Expect(got.MetByConclusion).To(Equal("C-0002"))
			Expect(got.RecommendedAction).To(Equal("stop"))
		})

		It("requires the reviewed candidate to satisfy every constraint", func() {
			flashMax := 100.0
			qualityMin := 0.99
			pass := true
			goal := &entity.Goal{
				Objective:  entity.Objective{Instrument: "host_timing", Direction: "decrease"},
				Completion: &entity.Completion{Threshold: 0.2, OnThreshold: entity.GoalOnThresholdAskHuman},
				Constraints: []entity.Constraint{
					{Instrument: "size_flash", Max: &flashMax},
					{Instrument: "quality_score", Min: &qualityMin},
					{Instrument: "host_test", Require: "pass"},
				},
			}
			concls := []*entity.Conclusion{
				{ID: "C-0100", Hypothesis: "H-0100", Verdict: entity.VerdictSupported, ReviewedBy: "agent:gate", CandidateExp: "E-0100", Effect: entity.Effect{Instrument: "host_timing", DeltaFrac: -0.30}},
				{ID: "C-0101", Hypothesis: "H-0101", Verdict: entity.VerdictSupported, ReviewedBy: "agent:gate", CandidateExp: "E-0101", Effect: entity.Effect{Instrument: "host_timing", DeltaFrac: -0.25}},
				{ID: "C-0102", Hypothesis: "H-0102", Verdict: entity.VerdictSupported, ReviewedBy: "agent:gate", CandidateExp: "E-0102", Effect: entity.Effect{Instrument: "host_timing", DeltaFrac: -0.22}},
			}
			got := readmodel.AssessGoalCompletion(goal, concls, observationIndexForFrontierTest(map[string][]*entity.Observation{
				"E-0100": {
					{Instrument: "host_timing", Value: 0.70},
					{Instrument: "size_flash", Value: 120},
					{Instrument: "quality_score", Value: 0.995},
					{Instrument: "host_test", Pass: &pass},
				},
				"E-0101": {
					{Instrument: "host_timing", Value: 0.75},
					{Instrument: "size_flash", Value: 90},
					{Instrument: "quality_score", Value: 0.98},
					{Instrument: "host_test", Pass: &pass},
				},
				"E-0102": {
					{Instrument: "host_timing", Value: 0.78},
					{Instrument: "size_flash", Value: 95},
					{Instrument: "quality_score", Value: 0.995},
					{Instrument: "host_test", Pass: &pass},
				},
			}), nil)
			Expect(got.Met).To(BeTrue())
			Expect(got.MetByConclusion).To(Equal("C-0102"))
			Expect(got.RecommendedAction).To(Equal("ask_human"))
		})

		It("uses positive delta_frac thresholds for increase goals", func() {
			goal := &entity.Goal{
				Objective:  entity.Objective{Instrument: "throughput", Direction: "increase"},
				Completion: &entity.Completion{Threshold: 0.1, OnThreshold: entity.GoalOnThresholdContinueUntilStall},
			}
			concls := []*entity.Conclusion{{
				ID:           "C-1000",
				Hypothesis:   "H-1000",
				Verdict:      entity.VerdictSupported,
				ReviewedBy:   "agent:gate",
				CandidateExp: "E-1000",
				Effect:       entity.Effect{Instrument: "throughput", DeltaFrac: 0.12},
			}}
			got := readmodel.AssessGoalCompletion(goal, concls, observationIndexForFrontierTest(map[string][]*entity.Observation{
				"E-1000": {{Instrument: "throughput", Value: 112}},
			}), nil)
			Expect(got.Met).To(BeTrue())
			Expect(got.RecommendedAction).To(Equal("continue"))
		})

		It("ignores unreviewed and non-supported conclusions", func() {
			goal := &entity.Goal{
				Objective:  entity.Objective{Instrument: "host_timing", Direction: "decrease"},
				Completion: &entity.Completion{Threshold: 0.2, OnThreshold: entity.GoalOnThresholdAskHuman},
			}
			concls := []*entity.Conclusion{
				{ID: "C-2000", Hypothesis: "H-2000", Verdict: entity.VerdictInconclusive, ReviewedBy: "agent:gate", CandidateExp: "E-2000", Effect: entity.Effect{Instrument: "host_timing", DeltaFrac: -0.3}},
				{ID: "C-2001", Hypothesis: "H-2001", Verdict: entity.VerdictSupported, CandidateExp: "E-2001", Effect: entity.Effect{Instrument: "host_timing", DeltaFrac: -0.3}},
			}
			got := readmodel.AssessGoalCompletion(goal, concls, observationIndexForFrontierTest(map[string][]*entity.Observation{
				"E-2000": {{Instrument: "host_timing", Value: 0.70}},
				"E-2001": {{Instrument: "host_timing", Value: 0.70}},
			}), nil)
			Expect(got.Met).To(BeFalse())
			Expect(got.RecommendedAction).To(Equal("continue"))
		})

		It("continues to count accepted supported candidates after promotion", func() {
			goal := &entity.Goal{
				Objective:  entity.Objective{Instrument: "host_timing", Direction: "decrease"},
				Completion: &entity.Completion{Threshold: 0.2, OnThreshold: entity.GoalOnThresholdStop},
			}
			concls := []*entity.Conclusion{{
				ID:           "C-3000",
				Hypothesis:   "H-3000",
				Verdict:      entity.VerdictSupported,
				ReviewedBy:   "agent:gate",
				CandidateExp: "E-3000",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.3},
			}}
			got := readmodel.AssessGoalCompletion(goal, concls, observationIndexForFrontierTest(map[string][]*entity.Observation{
				"E-3000": {{Instrument: "host_timing", Value: 0.70}},
			}), map[string]experimentReadClass{
				"E-3000": {Classification: experimentClassificationDead, HypothesisStatus: entity.StatusSupported},
			})
			Expect(got.Met).To(BeTrue())
			Expect(got.MetByConclusion).To(Equal("C-3000"))
			Expect(got.RecommendedAction).To(Equal("stop"))
		})
	})

	Describe("frontier projection", func() {
		It("carries experiment classification metadata", func() {
			goal := &entity.Goal{Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"}}
			concls := []*entity.Conclusion{{
				ID:           "C-0001",
				Hypothesis:   "H-0001",
				Verdict:      entity.VerdictSupported,
				CandidateExp: "E-0001",
				Effect:       entity.Effect{Instrument: "host_timing", DeltaFrac: -0.25},
			}}
			rows, _ := readmodel.ComputeFrontierFromObservations(goal, concls, observationIndexForFrontierTest(map[string][]*entity.Observation{
				"E-0001": {{Instrument: "host_timing", Value: 0.75}},
			}), map[string]experimentReadClass{
				"E-0001": {Classification: experimentClassificationDead, HypothesisStatus: entity.StatusSupported},
			})
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].Classification).To(Equal(experimentClassificationDead))
			Expect(rows[0].HypothesisStatus).To(Equal(entity.StatusSupported))
		})

		It("counts later conclusions as stalled after an accepted win", func() {
			goal := &entity.Goal{Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"}}
			base := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
			concls := []*entity.Conclusion{
				{ID: "C-0001", Hypothesis: "H-0001", Verdict: entity.VerdictSupported, CandidateExp: "E-0001", Effect: entity.Effect{Instrument: "host_timing", DeltaFrac: -0.10}, CreatedAt: base},
				{ID: "C-0002", Hypothesis: "H-0002", Verdict: entity.VerdictInconclusive, CandidateExp: "E-0002", Effect: entity.Effect{Instrument: "host_timing", DeltaFrac: -0.09}, CreatedAt: base.Add(1 * time.Minute)},
				{ID: "C-0003", Hypothesis: "H-0003", Verdict: entity.VerdictRefuted, CandidateExp: "E-0003", Effect: entity.Effect{Instrument: "host_timing", DeltaFrac: -0.20}, CreatedAt: base.Add(2 * time.Minute)},
			}
			rows, stalled := readmodel.ComputeFrontierFromObservations(goal, concls, observationIndexForFrontierTest(map[string][]*entity.Observation{
				"E-0001": {{Instrument: "host_timing", Value: 100}},
				"E-0002": {{Instrument: "host_timing", Value: 101}},
				"E-0003": {{Instrument: "host_timing", Value: 90}},
			}), map[string]experimentReadClass{
				"E-0001": {Classification: experimentClassificationDead, HypothesisStatus: entity.StatusSupported},
			})
			Expect(stalled).To(Equal(2))
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].Candidate).To(Equal("E-0001"))
			Expect(rows[0].Classification).To(Equal(experimentClassificationDead))
		})
	})
})
