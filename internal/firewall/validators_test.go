package firewall_test

import (
	"fmt"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/stats"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func cfgWith(names ...string) *store.Config {
	cfg := &store.Config{Instruments: map[string]store.Instrument{}}
	for _, n := range names {
		cfg.Instruments[n] = store.Instrument{Unit: "x"}
	}
	return cfg
}

type fakeInspiredByReader struct {
	hypotheses  map[string]*entity.Hypothesis
	experiments map[string]*entity.Experiment
	conclusions map[string]*entity.Conclusion
}

func (f fakeInspiredByReader) ReadHypothesis(id string) (*entity.Hypothesis, error) {
	if h, ok := f.hypotheses[id]; ok {
		return h, nil
	}
	return nil, fmt.Errorf("hypothesis %s not found", id)
}

func (f fakeInspiredByReader) ReadExperiment(id string) (*entity.Experiment, error) {
	if e, ok := f.experiments[id]; ok {
		return e, nil
	}
	return nil, fmt.Errorf("experiment %s not found", id)
}

func (f fakeInspiredByReader) ReadConclusion(id string) (*entity.Conclusion, error) {
	if c, ok := f.conclusions[id]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("conclusion %s not found", id)
}

var _ = Describe("goal state gates", func() {
	It("requires an active goal for mutating research verbs", func() {
		Expect(firewall.RequireActiveGoal(nil)).To(HaveOccurred())
		Expect(firewall.RequireActiveGoal(&store.State{})).To(HaveOccurred())
		Expect(firewall.RequireActiveGoal(&store.State{CurrentGoalID: "G-0001"})).To(Succeed())
	})

	It("rejects a new goal when another active goal is present", func() {
		Expect(firewall.RequireNoActiveGoal(nil)).To(Succeed())
		Expect(firewall.RequireNoActiveGoal(&store.State{})).To(Succeed())
		Expect(firewall.RequireNoActiveGoal(&store.State{CurrentGoalID: "G-0007"})).To(MatchError(ContainSubstring("G-0007")))
	})
})

var _ = Describe("goal validation", func() {
	It("rejects feature-style goals without optimization objective fields", func() {
		err := firewall.ValidateGoal(&entity.Goal{}, cfgWith())
		Expect(err).To(MatchError(ContainSubstring("not build features")))
	})

	It("requires registered objective and constraint instruments", func() {
		g := &entity.Goal{Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"}}
		Expect(firewall.ValidateGoal(g, cfgWith())).To(MatchError(ContainSubstring("not registered")))
		Expect(firewall.ValidateGoal(g, cfgWith("qemu_cycles"))).To(MatchError(ContainSubstring("at least one constraint")))
	})

	It("accepts a well-formed goal with completion policy", func() {
		max := 65536.0
		g := &entity.Goal{
			Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"},
			Completion: &entity.Completion{
				Threshold:   0.2,
				OnThreshold: entity.GoalOnThresholdAskHuman,
			},
			Constraints: []entity.Constraint{{Instrument: "size_flash", Max: &max}},
		}
		Expect(firewall.ValidateGoal(g, cfgWith("qemu_cycles", "size_flash"))).To(Succeed())
	})

	DescribeTable("rejects invalid completion policies",
		func(completion entity.Completion, want string) {
			max := 65536.0
			g := &entity.Goal{
				Objective:   entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"},
				Constraints: []entity.Constraint{{Instrument: "size_flash", Max: &max}},
				Completion:  &completion,
			}
			Expect(firewall.ValidateGoal(g, cfgWith("qemu_cycles", "size_flash"))).To(MatchError(ContainSubstring(want)))
		},
		Entry("non-positive threshold", entity.Completion{Threshold: 0}, "threshold must be > 0"),
		Entry("unknown threshold action", entity.Completion{Threshold: 0.2, OnThreshold: "invent_it"}, "completion.on_threshold"),
	)

	Describe("rescuers", func() {
		cfg := &store.Config{Instruments: map[string]store.Instrument{
			"ns_per_eval":     {},
			"sim_total_bytes": {},
			"host_test":       {},
		}}
		baseGoal := func() *entity.Goal {
			return &entity.Goal{
				Objective:   entity.Objective{Instrument: "ns_per_eval", Direction: "decrease"},
				Constraints: []entity.Constraint{{Instrument: "host_test", Require: "pass"}},
			}
		}

		It("accepts goals without rescuers", func() {
			Expect(firewall.ValidateGoal(baseGoal(), cfg)).To(Succeed())
		})

		DescribeTable("rejects malformed rescuer clauses",
			func(mut func(*entity.Goal), want string) {
				g := baseGoal()
				mut(g)
				Expect(firewall.ValidateGoal(g, cfg)).To(MatchError(ContainSubstring(want)))
			},
			Entry("without neutral band",
				func(g *entity.Goal) {
					g.Rescuers = []entity.Rescuer{{Instrument: "sim_total_bytes", Direction: "decrease", MinEffect: 0.02}}
				},
				"neutral_band_frac",
			),
			Entry("pointing at the objective instrument",
				func(g *entity.Goal) {
					g.NeutralBandFrac = 0.02
					g.Rescuers = []entity.Rescuer{{Instrument: "ns_per_eval", Direction: "decrease", MinEffect: 0.02}}
				},
				"equals the goal objective",
			),
			Entry("with an unregistered instrument",
				func(g *entity.Goal) {
					g.NeutralBandFrac = 0.02
					g.Rescuers = []entity.Rescuer{{Instrument: "unregistered", Direction: "decrease", MinEffect: 0.02}}
				},
				"not registered",
			),
			Entry("with negative min effect",
				func(g *entity.Goal) {
					g.NeutralBandFrac = 0.02
					g.Rescuers = []entity.Rescuer{{Instrument: "sim_total_bytes", Direction: "decrease", MinEffect: -0.01}}
				},
				">= 0",
			),
		)

		DescribeTable("accepts well-formed rescuer clauses",
			func(minEffect float64) {
				g := baseGoal()
				g.NeutralBandFrac = 0.02
				g.Rescuers = []entity.Rescuer{{Instrument: "sim_total_bytes", Direction: "decrease", MinEffect: minEffect}}
				Expect(firewall.ValidateGoal(g, cfg)).To(Succeed())
			},
			Entry("with positive min effect", 0.02),
			Entry("as directional only", 0.0),
		)
	})
})

var _ = Describe("hypothesis and experiment validation", func() {
	It("rejects incomplete hypotheses", func() {
		Expect(firewall.ValidateHypothesis(&entity.Hypothesis{}, cfgWith("qemu_cycles"))).To(HaveOccurred())
		h := &entity.Hypothesis{
			Claim: "x",
			Predicts: entity.Predicts{
				Instrument: "qemu_cycles", Target: "y", Direction: "decrease", MinEffect: 0.1,
			},
		}
		Expect(firewall.ValidateHypothesis(h, cfgWith("qemu_cycles"))).To(MatchError(ContainSubstring("kill_if")))
	})

	It("accepts magnitude and directional hypotheses", func() {
		Expect(firewall.ValidateHypothesis(&entity.Hypothesis{
			Claim: "unroll dsp_fir 4x cuts cycles >10%",
			Predicts: entity.Predicts{
				Instrument: "qemu_cycles", Target: "dsp_fir_bench", Direction: "decrease", MinEffect: 0.10,
			},
			KillIf: []string{"flash delta > 1024 bytes"},
		}, cfgWith("qemu_cycles"))).To(Succeed())

		Expect(firewall.ValidateHypothesis(&entity.Hypothesis{
			Claim: "unrolling the hot loop will reduce cycles",
			Predicts: entity.Predicts{
				Instrument: "qemu_cycles", Target: "dsp_fir_bench", Direction: "decrease", MinEffect: 0,
			},
			KillIf: []string{"ci upper bound >= 0"},
		}, cfgWith("qemu_cycles"))).To(Succeed())
	})

	It("rejects negative hypothesis min_effect", func() {
		err := firewall.ValidateHypothesis(&entity.Hypothesis{
			Claim: "x",
			Predicts: entity.Predicts{
				Instrument: "qemu_cycles", Target: "y", Direction: "decrease", MinEffect: -0.01,
			},
			KillIf: []string{"..."},
		}, cfgWith("qemu_cycles"))
		Expect(err).To(MatchError(ContainSubstring(">= 0")))
	})

	It("accepts and rejects experiments based on registered instruments", func() {
		Expect(firewall.ValidateExperiment(&entity.Experiment{
			GoalID:      "G-0001",
			Hypothesis:  "H-0001",
			Instruments: []string{"qemu_cycles"},
			Baseline:    entity.Baseline{Ref: "HEAD"},
		}, cfgWith("qemu_cycles"))).To(Succeed())

		Expect(firewall.ValidateExperiment(&entity.Experiment{
			GoalID:      "G-0001",
			Hypothesis:  "H-0001",
			Instruments: []string{"ghost_instrument"},
			Baseline:    entity.Baseline{Ref: "HEAD"},
		}, cfgWith("qemu_cycles"))).To(HaveOccurred())
	})

	It("limits hypothesis predicted instruments to goal objective or constraints", func() {
		max := 65536.0
		goal := &entity.Goal{
			ID:        "G-0001",
			Objective: entity.Objective{Instrument: "timing", Direction: "decrease"},
			Constraints: []entity.Constraint{
				{Instrument: "binary_size", Max: &max},
				{Instrument: "test", Require: "pass"},
				{Instrument: "compile", Require: "pass"},
			},
		}
		newHypothesis := func(inst string) *entity.Hypothesis {
			return &entity.Hypothesis{
				Claim: "x",
				Predicts: entity.Predicts{
					Instrument: inst,
					Target:     "firmware",
					Direction:  "decrease",
					MinEffect:  0.1,
				},
				KillIf: []string{"fails"},
			}
		}

		Expect(firewall.CheckHypothesisInstrumentWithinGoal(goal, newHypothesis("timing"))).To(Succeed())
		Expect(firewall.CheckHypothesisInstrumentWithinGoal(goal, newHypothesis("binary_size"))).To(Succeed())
		err := firewall.CheckHypothesisInstrumentWithinGoal(goal, newHypothesis("qemu_cycles"))
		Expect(err).To(HaveOccurred())
		for _, want := range []string{
			"qemu_cycles",
			"timing, binary_size, test, compile",
			"supporting instruments may still be observed on experiments",
		} {
			Expect(err.Error()).To(ContainSubstring(want))
		}
	})
})

var _ = Describe("instrument dependency gates", func() {
	It("requires prerequisite observations to pass", func() {
		cfg := &store.Config{Instruments: map[string]store.Instrument{
			"host_test":   {Unit: "bool"},
			"qemu_cycles": {Unit: "cycles", Requires: []string{"host_test=pass"}},
		}}
		pass := true
		fail := false

		Expect(firewall.CheckInstrumentDependencies("qemu_cycles", cfg, nil)).To(HaveOccurred())
		Expect(firewall.CheckInstrumentDependencies("qemu_cycles", cfg, []*entity.Observation{
			{Instrument: "host_test", Pass: &pass},
		})).To(Succeed())
		Expect(firewall.CheckInstrumentDependencies("qemu_cycles", cfg, []*entity.Observation{
			{Instrument: "host_test", Pass: &fail},
		})).To(HaveOccurred())
	})
})

var _ = Describe("lesson validation and provenance", func() {
	DescribeTable("validates lesson shape",
		func(l *entity.Lesson, wantErr string) {
			err := firewall.ValidateLesson(l)
			if wantErr == "" {
				Expect(err).NotTo(HaveOccurred())
				return
			}
			Expect(err).To(MatchError(ContainSubstring(wantErr)))
		},
		Entry("happy hypothesis scope", &entity.Lesson{
			Claim:      "unroll past 8x is cache-bound",
			Scope:      entity.LessonScopeHypothesis,
			Subjects:   []string{"C-0003"},
			Status:     entity.LessonStatusProvisional,
			Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceUnreviewedDecisive},
		}, ""),
		Entry("happy system scope", &entity.Lesson{
			Claim: "fixture cache is sticky",
			Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive,
		}, ""),
		Entry("system scope with subjects", &entity.Lesson{
			Claim: "x", Scope: entity.LessonScopeSystem, Subjects: []string{"H-0001"},
		}, "cannot cite --from subjects"),
		Entry("empty claim", &entity.Lesson{Scope: entity.LessonScopeSystem}, "claim"),
		Entry("invalid scope", &entity.Lesson{Claim: "x", Scope: "galactic"}, "scope must be"),
		Entry("hypothesis scope without subjects", &entity.Lesson{Claim: "x", Scope: entity.LessonScopeHypothesis}, "at least one subject"),
		Entry("invalid subject prefix", &entity.Lesson{
			Claim: "x", Scope: entity.LessonScopeHypothesis, Subjects: []string{"X-0001"},
		}, "H-/E-/C-"),
		Entry("invalid supersedes id", &entity.Lesson{
			Claim: "x", Scope: entity.LessonScopeSystem, SupersedesID: "H-0001",
		}, "L- id"),
		Entry("invalid provenance source chain", &entity.Lesson{
			Claim:      "x",
			Scope:      entity.LessonScopeHypothesis,
			Subjects:   []string{"H-0001"},
			Provenance: &entity.LessonProvenance{SourceChain: "sideways"},
		}, "source_chain"),
	)

	DescribeTable("assesses source chains from lesson subjects",
		func(reader fakeInspiredByReader, lesson *entity.Lesson, want string) {
			got, err := firewall.AssessLessonSourceChain(reader, lesson)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry("system lessons resolve to system source",
			fakeInspiredByReader{},
			&entity.Lesson{ID: "L-0000", Scope: entity.LessonScopeSystem},
			entity.LessonSourceSystem,
		),
		Entry("malformed system lessons with subjects still resolve from subjects",
			fakeInspiredByReader{
				hypotheses: map[string]*entity.Hypothesis{
					"H-0003": {ID: "H-0003", Status: entity.StatusUnreviewed},
				},
				conclusions: map[string]*entity.Conclusion{
					"C-0003": {ID: "C-0003", Hypothesis: "H-0003", Verdict: entity.VerdictSupported},
				},
			},
			&entity.Lesson{ID: "L-0003", Scope: entity.LessonScopeSystem, Subjects: []string{"C-0003"}},
			entity.LessonSourceUnreviewedDecisive,
		),
		Entry("reviewed hypothesis resolves to reviewed decisive",
			fakeInspiredByReader{
				hypotheses: map[string]*entity.Hypothesis{
					"H-0001": {ID: "H-0001", Status: entity.StatusSupported},
				},
			},
			&entity.Lesson{ID: "L-0001", Scope: entity.LessonScopeHypothesis, Subjects: []string{"H-0001"}},
			entity.LessonSourceReviewedDecisive,
		),
		Entry("unreviewed decisive conclusion resolves to unreviewed decisive",
			fakeInspiredByReader{
				hypotheses: map[string]*entity.Hypothesis{
					"H-0003": {ID: "H-0003", Status: entity.StatusUnreviewed},
				},
				conclusions: map[string]*entity.Conclusion{
					"C-0003": {ID: "C-0003", Hypothesis: "H-0003", Verdict: entity.VerdictSupported},
				},
			},
			&entity.Lesson{ID: "L-0003", Scope: entity.LessonScopeHypothesis, Subjects: []string{"C-0003"}},
			entity.LessonSourceUnreviewedDecisive,
		),
		Entry("downgraded chain resolves to inconclusive",
			fakeInspiredByReader{
				hypotheses: map[string]*entity.Hypothesis{
					"H-0004": {ID: "H-0004", Status: entity.StatusInconclusive},
				},
				conclusions: map[string]*entity.Conclusion{
					"C-0004": {ID: "C-0004", Hypothesis: "H-0004", Verdict: entity.VerdictInconclusive},
				},
			},
			&entity.Lesson{ID: "L-0004", Scope: entity.LessonScopeHypothesis, Subjects: []string{"C-0004"}},
			entity.LessonSourceInconclusive,
		),
	)

	DescribeTable("requires inspired-by lessons to be active and reviewed",
		func(reader fakeInspiredByReader, lessons []*entity.Lesson, wantErr string) {
			err := firewall.CheckInspiredByLessonsReviewed(reader, lessons)
			if wantErr == "" {
				Expect(err).NotTo(HaveOccurred())
				return
			}
			Expect(err).To(MatchError(ContainSubstring(wantErr)))
		},
		Entry("active reviewed hypothesis chain",
			fakeInspiredByReader{
				hypotheses: map[string]*entity.Hypothesis{
					"H-0001": {ID: "H-0001", Status: entity.StatusSupported},
				},
			},
			[]*entity.Lesson{{ID: "L-0001", Scope: entity.LessonScopeHypothesis, Status: entity.LessonStatusActive, Subjects: []string{"H-0001"}}},
			"",
		),
		Entry("non-active lesson status",
			fakeInspiredByReader{},
			[]*entity.Lesson{{ID: "L-0002", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusProvisional}},
			"active",
		),
		Entry("unreviewed decisive chains",
			fakeInspiredByReader{
				hypotheses: map[string]*entity.Hypothesis{
					"H-0003": {ID: "H-0003", Status: entity.StatusUnreviewed},
				},
				conclusions: map[string]*entity.Conclusion{
					"C-0003": {ID: "C-0003", Hypothesis: "H-0003", Verdict: entity.VerdictSupported},
				},
			},
			[]*entity.Lesson{{ID: "L-0003", Scope: entity.LessonScopeHypothesis, Status: entity.LessonStatusActive, Subjects: []string{"C-0003"}}},
			"unreviewed decisive chain",
		),
		Entry("malformed system lesson on unreviewed decisive chain",
			fakeInspiredByReader{
				hypotheses: map[string]*entity.Hypothesis{
					"H-0005": {ID: "H-0005", Status: entity.StatusUnreviewed},
				},
				conclusions: map[string]*entity.Conclusion{
					"C-0005": {ID: "C-0005", Hypothesis: "H-0005", Verdict: entity.VerdictSupported},
				},
			},
			[]*entity.Lesson{{ID: "L-0005", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive, Subjects: []string{"C-0005"}}},
			"unreviewed decisive chain",
		),
		Entry("downgraded or inconclusive chains",
			fakeInspiredByReader{
				hypotheses: map[string]*entity.Hypothesis{
					"H-0004": {ID: "H-0004", Status: entity.StatusInconclusive},
				},
				conclusions: map[string]*entity.Conclusion{
					"C-0004": {ID: "C-0004", Hypothesis: "H-0004", Verdict: entity.VerdictInconclusive},
				},
			},
			[]*entity.Lesson{{ID: "L-0004", Scope: entity.LessonScopeHypothesis, Status: entity.LessonStatusActive, Subjects: []string{"C-0004"}}},
			"non-decisive chain",
		),
	)
})

var _ = Describe("budget gates", func() {
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)

	DescribeTable("allows experiments under configured budgets",
		func(st *store.State, cfg *store.Config) {
			Expect(firewall.CheckBudgetForNewExperiment(st, cfg, now).Ok()).To(BeTrue())
		},
		Entry("zero budgets", &store.State{Counters: map[string]int{"E": 42}}, &store.Config{}),
		Entry("under max_experiments", &store.State{Counters: map[string]int{"E": 4}}, &store.Config{Budgets: store.Budgets{MaxExperiments: 5}}),
		Entry("nil start time under wall clock budget", &store.State{Counters: map[string]int{"E": 0}}, &store.Config{Budgets: store.Budgets{MaxWallTimeH: 24}}),
		Entry("under max_wall_time_h", func() *store.State {
			start := now.Add(-6 * time.Hour)
			return &store.State{Counters: map[string]int{"E": 0}, ResearchStartedAt: &start}
		}(), &store.Config{Budgets: store.Budgets{MaxWallTimeH: 24}}),
	)

	It("breaches max_experiments at the limit", func() {
		breach := firewall.CheckBudgetForNewExperiment(
			&store.State{Counters: map[string]int{"E": 5}},
			&store.Config{Budgets: store.Budgets{MaxExperiments: 5}},
			now,
		)
		Expect(breach.Ok()).To(BeFalse())
		Expect(breach.Rule).To(Equal("max_experiments"))
		Expect(breach.Message).To(ContainSubstring("max_experiments=5"))
	})

	It("breaches max_wall_time_h after the limit", func() {
		start := now.Add(-25 * time.Hour)
		breach := firewall.CheckBudgetForNewExperiment(
			&store.State{Counters: map[string]int{"E": 0}, ResearchStartedAt: &start},
			&store.Config{Budgets: store.Budgets{MaxWallTimeH: 24}},
			now,
		)
		Expect(breach.Ok()).To(BeFalse())
		Expect(breach.Rule).To(Equal("max_wall_time_h"))
		Expect(breach.Message).To(ContainSubstring("max_wall_time_h=24"))
	})

	It("checks max_experiments before wall time", func() {
		start := now.Add(-30 * time.Hour)
		breach := firewall.CheckBudgetForNewExperiment(
			&store.State{Counters: map[string]int{"E": 5}, ResearchStartedAt: &start},
			&store.Config{Budgets: store.Budgets{MaxExperiments: 5, MaxWallTimeH: 24}},
			now,
		)
		Expect(breach.Rule).To(Equal("max_experiments"))
	})
})

var _ = Describe("strict verdict firewall", func() {
	hyp := func(direction string, minEffect float64) *entity.Hypothesis {
		return &entity.Hypothesis{Predicts: entity.Predicts{Direction: direction, MinEffect: minEffect}}
	}
	cmp := func(n int, deltaFrac, ciLow, ciHigh float64) *stats.Comparison {
		return &stats.Comparison{
			NBaseline:  n,
			NCandidate: n,
			DeltaFrac:  deltaFrac,
			CILowFrac:  ciLow,
			CIHighFrac: ciHigh,
		}
	}

	It("lets directional hypotheses pass on clean CI without magnitude gate", func() {
		h := hyp("decrease", 0)
		d := firewall.CheckStrictVerdict(entity.VerdictSupported, h, cmp(10, -0.003, -0.005, -0.001))
		Expect(d.Passed).To(BeTrue())
		Expect(d.Downgraded).To(BeFalse())

		d = firewall.CheckStrictVerdict(entity.VerdictSupported, h, cmp(10, -0.003, -0.010, 0.005))
		Expect(d.Passed).To(BeFalse())
		Expect(d.Downgraded).To(BeTrue())
	})

	DescribeTable("downgrades unsupported supported verdicts",
		func(h *entity.Hypothesis, comparison *stats.Comparison, wantReason string) {
			d := firewall.CheckStrictVerdict(entity.VerdictSupported, h, comparison)
			Expect(d.Passed).To(BeFalse())
			Expect(d.Downgraded).To(BeTrue())
			Expect(d.FinalVerdict).To(Equal(entity.VerdictInconclusive))
			if wantReason != "" {
				Expect(strings.Join(d.Reasons, " ")).To(ContainSubstring(wantReason))
			}
		},
		Entry("insufficient samples", hyp("decrease", 0.05), cmp(1, -0.2, -0.3, -0.1), ""),
		Entry("nil comparison", hyp("decrease", 0.05), nil, ""),
		Entry("increase CI crossing zero", hyp("increase", 0.05), cmp(10, 0.15, -0.02, 0.30), "crosses zero"),
		Entry("decrease CI crossing zero", hyp("decrease", 0.05), cmp(10, -0.15, -0.30, 0.02), ""),
		Entry("effect below min_effect", hyp("decrease", 0.20), cmp(10, -0.05, -0.08, -0.02), "min_effect"),
	)

	It("allows supported verdicts with clean strict evidence", func() {
		d := firewall.CheckStrictVerdict(entity.VerdictSupported, hyp("decrease", 0.05), cmp(10, -0.20, -0.30, -0.10))
		Expect(d.Passed).To(BeTrue())
		Expect(d.Downgraded).To(BeFalse())
		Expect(d.FinalVerdict).To(Equal(entity.VerdictSupported))
	})

	It("accepts refuted and inconclusive verdicts while recording structural evidence when present", func() {
		d := firewall.CheckStrictVerdict(entity.VerdictRefuted, hyp("decrease", 0.05), cmp(10, 0.20, 0.10, 0.30))
		Expect(d.Passed).To(BeTrue())
		Expect(d.Reasons).NotTo(BeEmpty())

		d = firewall.CheckStrictVerdict(entity.VerdictRefuted, hyp("decrease", 0.05), cmp(10, -0.20, -0.30, -0.10))
		Expect(d.Passed).To(BeTrue())

		d = firewall.CheckStrictVerdict(entity.VerdictInconclusive, hyp("decrease", 0.05), nil)
		Expect(d.Passed).To(BeTrue())
		Expect(d.Downgraded).To(BeFalse())
	})

	It("rejects unknown verdicts", func() {
		d := firewall.CheckStrictVerdict("uncertain", hyp("decrease", 0.05), cmp(10, -0.20, -0.30, -0.10))
		Expect(d.Passed).To(BeFalse())
		Expect(strings.Join(d.Reasons, " ")).To(ContainSubstring("unknown verdict"))
	})

	Describe("rescuer clauses", func() {
		primaryHyp := &entity.Hypothesis{Predicts: entity.Predicts{
			Instrument: "ns_per_eval",
			Direction:  "decrease",
			MinEffect:  0.05,
		}}
		goal := &entity.Goal{
			Objective:       entity.Objective{Instrument: "ns_per_eval", Direction: "decrease"},
			NeutralBandFrac: 0.02,
			Rescuers: []entity.Rescuer{{
				Instrument: "sim_total_bytes",
				Direction:  "decrease",
				MinEffect:  0.02,
			}},
		}
		mkCmp := func(deltaFrac, ciLow, ciHigh float64) *stats.Comparison {
			return &stats.Comparison{
				NBaseline: 10, NCandidate: 10,
				DeltaFrac: deltaFrac, CILowFrac: ciLow, CIHighFrac: ciHigh,
			}
		}

		It("does not consult rescuers when the primary comparison passes", func() {
			ctx := firewall.StrictContext{
				Goal: goal,
				RescuerComparison: func(instrument string) (*stats.Comparison, string) {
					Fail("rescuer should not be consulted when primary passes")
					return nil, ""
				},
			}
			d := firewall.CheckStrictVerdictWithContext(entity.VerdictSupported, primaryHyp, mkCmp(-0.20, -0.30, -0.10), ctx)
			Expect(d.Passed).To(BeTrue())
			Expect(d.Downgraded).To(BeFalse())
			Expect(d.RescuedBy).To(BeEmpty())
		})

		It("rescues neutral primary results when a rescuer passes strict checks", func() {
			resc := mkCmp(-0.10, -0.15, -0.05)
			ctx := firewall.StrictContext{
				Goal: goal,
				RescuerComparison: func(instrument string) (*stats.Comparison, string) {
					Expect(instrument).To(Equal("sim_total_bytes"))
					return resc, ""
				},
			}
			d := firewall.CheckStrictVerdictWithContext(entity.VerdictSupported, primaryHyp, mkCmp(0.005, -0.01, 0.02), ctx)
			Expect(d.Passed).To(BeTrue())
			Expect(d.Downgraded).To(BeFalse())
			Expect(d.RescuedBy).To(Equal("sim_total_bytes"))
			Expect(d.FinalVerdict).To(Equal(entity.VerdictSupported))
			Expect(d.ClauseChecks).To(HaveLen(1))
			Expect(d.ClauseChecks[0].Passed).To(BeTrue())
		})

		It("downgrades neutral primaries when no rescuer passes", func() {
			ctx := firewall.StrictContext{
				Goal: goal,
				RescuerComparison: func(instrument string) (*stats.Comparison, string) {
					return mkCmp(-0.10, -0.20, 0.01), ""
				},
			}
			d := firewall.CheckStrictVerdictWithContext(entity.VerdictSupported, primaryHyp, mkCmp(0.005, -0.01, 0.02), ctx)
			Expect(d.Passed).To(BeFalse())
			Expect(d.Downgraded).To(BeTrue())
			Expect(d.RescuedBy).To(BeEmpty())
		})

		It("does not rescue structural primary regressions", func() {
			ctx := firewall.StrictContext{
				Goal: goal,
				RescuerComparison: func(instrument string) (*stats.Comparison, string) {
					return mkCmp(-0.10, -0.15, -0.05), ""
				},
			}
			d := firewall.CheckStrictVerdictWithContext(entity.VerdictSupported, primaryHyp, mkCmp(0.05, 0.03, 0.08), ctx)
			Expect(d.Passed).To(BeFalse())
			Expect(d.Downgraded).To(BeTrue())
			Expect(d.RescuedBy).To(BeEmpty())
		})

		It("records no-data clause checks for missing rescuer observations", func() {
			ctx := firewall.StrictContext{
				Goal: goal,
				RescuerComparison: func(instrument string) (*stats.Comparison, string) {
					return nil, "no observations on \"sim_total_bytes\" for candidate E-0001"
				},
			}
			d := firewall.CheckStrictVerdictWithContext(entity.VerdictSupported, primaryHyp, mkCmp(0.005, -0.01, 0.02), ctx)
			Expect(d.Passed).To(BeFalse())
			Expect(d.Downgraded).To(BeTrue())
			Expect(d.RescuedBy).To(BeEmpty())
		})

		It("does not rescue through the plain strict verdict path", func() {
			d := firewall.CheckStrictVerdict(entity.VerdictSupported, primaryHyp, mkCmp(0.005, -0.01, 0.02))
			Expect(d.Passed).To(BeFalse())
			Expect(d.Downgraded).To(BeTrue())
		})
	})
})

var _ = Describe("instrument deletion safety", func() {
	goal := func(id string, status string, obj string, constraints ...string) *entity.Goal {
		g := &entity.Goal{ID: id, Status: status, Objective: entity.Objective{Instrument: obj, Direction: "decrease"}}
		for _, c := range constraints {
			g.Constraints = append(g.Constraints, entity.Constraint{Instrument: c})
		}
		return g
	}
	hyp := func(id, inst string) *entity.Hypothesis {
		return &entity.Hypothesis{ID: id, Predicts: entity.Predicts{Instrument: inst, Direction: "decrease"}}
	}
	obs := func(id, inst string) *entity.Observation {
		return &entity.Observation{ID: id, Instrument: inst}
	}

	It("allows unreferenced instruments", func() {
		u := firewall.CheckInstrumentSafeToDelete("binary_size",
			[]*entity.Goal{goal("G-0001", entity.GoalStatusActive, "timing")},
			[]*entity.Hypothesis{hyp("H-0001", "timing")},
			[]*entity.Observation{obs("O-0001", "timing")},
		)
		Expect(u.Ok()).To(BeTrue())
	})

	It("blocks active goal objective references even with force", func() {
		u := firewall.CheckInstrumentSafeToDelete("timing", []*entity.Goal{goal("G-0001", entity.GoalStatusActive, "timing")}, nil, nil)
		Expect(u.Ok()).To(BeFalse())
		Expect(u.BlocksEvenWithForce()).To(BeTrue())
		Expect(u.GoalObjectives).To(Equal([]string{"G-0001"}))
	})

	It("treats constraints, hypotheses, and observations as force-overridable references", func() {
		u := firewall.CheckInstrumentSafeToDelete("binary_size", []*entity.Goal{goal("G-0001", entity.GoalStatusActive, "timing", "binary_size")}, nil, nil)
		Expect(u.Ok()).To(BeFalse())
		Expect(u.BlocksEvenWithForce()).To(BeFalse())

		u = firewall.CheckInstrumentSafeToDelete("binary_size", nil, []*entity.Hypothesis{hyp("H-0001", "binary_size"), hyp("H-0002", "timing")}, nil)
		Expect(u.Ok()).To(BeFalse())
		Expect(u.BlocksEvenWithForce()).To(BeFalse())
		Expect(u.Hypotheses).To(Equal([]string{"H-0001"}))

		u = firewall.CheckInstrumentSafeToDelete("binary_size", nil, nil, []*entity.Observation{obs("O-0001", "binary_size")})
		Expect(u.Ok()).To(BeFalse())
		Expect(u.BlocksEvenWithForce()).To(BeFalse())
		Expect(u.Observations).To(HaveLen(1))
	})

	It("ignores inactive goal references", func() {
		u := firewall.CheckInstrumentSafeToDelete("timing", []*entity.Goal{goal("G-0001", entity.GoalStatusConcluded, "timing", "binary_size")}, nil, nil)
		Expect(u.Ok()).To(BeTrue())
	})

	It("summarizes references and leaves safe usages blank", func() {
		u := firewall.CheckInstrumentSafeToDelete("binary_size",
			[]*entity.Goal{goal("G-0001", entity.GoalStatusActive, "timing", "binary_size")},
			[]*entity.Hypothesis{hyp("H-0001", "binary_size")},
			[]*entity.Observation{obs("O-0001", "binary_size")},
		)
		summary := u.Summary()
		Expect(summary).To(ContainSubstring("G-0001"))
		Expect(summary).To(ContainSubstring("H-0001"))
		Expect(summary).To(ContainSubstring("O-0001"))
		Expect(firewall.CheckInstrumentSafeToDelete("x", nil, nil, nil).Summary()).To(BeEmpty())
	})
})
