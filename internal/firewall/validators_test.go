package firewall_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/stats"
	"github.com/bytter/autoresearch/internal/store"
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

func TestRequireActiveGoal(t *testing.T) {
	if err := firewall.RequireActiveGoal(nil); err == nil {
		t.Error("nil state should be rejected")
	}
	if err := firewall.RequireActiveGoal(&store.State{}); err == nil {
		t.Error("empty current_goal_id should be rejected")
	}
	if err := firewall.RequireActiveGoal(&store.State{CurrentGoalID: "G-0001"}); err != nil {
		t.Errorf("active goal should pass, got %v", err)
	}
}

func TestRequireNoActiveGoal(t *testing.T) {
	if err := firewall.RequireNoActiveGoal(nil); err != nil {
		t.Errorf("nil state counts as no active goal, got %v", err)
	}
	if err := firewall.RequireNoActiveGoal(&store.State{}); err != nil {
		t.Errorf("empty current_goal_id counts as no active goal, got %v", err)
	}
	err := firewall.RequireNoActiveGoal(&store.State{CurrentGoalID: "G-0007"})
	if err == nil || !strings.Contains(err.Error(), "G-0007") {
		t.Errorf("existing active goal should be rejected with id, got %v", err)
	}
}

func TestValidateGoal_FeatureStyle(t *testing.T) {
	g := &entity.Goal{}
	err := firewall.ValidateGoal(g, cfgWith())
	if err == nil || !strings.Contains(err.Error(), "not build features") {
		t.Errorf("feature-style goal should name the scope boundary, got %v", err)
	}
}

func TestValidateGoal_UnregisteredInstrument(t *testing.T) {
	g := &entity.Goal{Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"}}
	err := firewall.ValidateGoal(g, cfgWith())
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Errorf("unregistered instrument should be rejected, got %v", err)
	}
}

func TestValidateGoal_NoConstraints(t *testing.T) {
	g := &entity.Goal{Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"}}
	err := firewall.ValidateGoal(g, cfgWith("qemu_cycles"))
	if err == nil || !strings.Contains(err.Error(), "at least one constraint") {
		t.Errorf("missing constraints should be rejected, got %v", err)
	}
}

func TestValidateGoal_Happy(t *testing.T) {
	max := 65536.0
	g := &entity.Goal{
		Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"},
		Completion: &entity.Completion{
			Threshold:   0.2,
			OnThreshold: entity.GoalOnThresholdAskHuman,
		},
		Constraints: []entity.Constraint{
			{Instrument: "size_flash", Max: &max},
		},
	}
	if err := firewall.ValidateGoal(g, cfgWith("qemu_cycles", "size_flash")); err != nil {
		t.Errorf("valid goal rejected: %v", err)
	}
}

func TestValidateGoal_InvalidCompletion(t *testing.T) {
	max := 65536.0
	base := &entity.Goal{
		Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"},
		Constraints: []entity.Constraint{
			{Instrument: "size_flash", Max: &max},
		},
	}

	t.Run("threshold must be positive", func(t *testing.T) {
		g := *base
		g.Completion = &entity.Completion{Threshold: 0}
		err := firewall.ValidateGoal(&g, cfgWith("qemu_cycles", "size_flash"))
		if err == nil || !strings.Contains(err.Error(), "threshold must be > 0") {
			t.Fatalf("expected bad threshold to fail, got %v", err)
		}
	})

	t.Run("on_threshold must be known", func(t *testing.T) {
		g := *base
		g.Completion = &entity.Completion{Threshold: 0.2, OnThreshold: "invent_it"}
		err := firewall.ValidateGoal(&g, cfgWith("qemu_cycles", "size_flash"))
		if err == nil || !strings.Contains(err.Error(), "completion.on_threshold") {
			t.Fatalf("expected bad on_threshold to fail, got %v", err)
		}
	})
}

func TestValidateHypothesis_MissingFields(t *testing.T) {
	h := &entity.Hypothesis{}
	if err := firewall.ValidateHypothesis(h, cfgWith("qemu_cycles")); err == nil {
		t.Error("empty hypothesis should be rejected")
	}
}

func TestValidateHypothesis_NoKillIf(t *testing.T) {
	h := &entity.Hypothesis{
		Claim: "x",
		Predicts: entity.Predicts{
			Instrument: "qemu_cycles", Target: "y", Direction: "decrease", MinEffect: 0.1,
		},
	}
	if err := firewall.ValidateHypothesis(h, cfgWith("qemu_cycles")); err == nil || !strings.Contains(err.Error(), "kill_if") {
		t.Errorf("hypothesis without kill_if should be rejected, got %v", err)
	}
}

func TestValidateExperiment_Happy(t *testing.T) {
	e := &entity.Experiment{
		Hypothesis:  "H-0001",
		Instruments: []string{"qemu_cycles"},
		Baseline:    entity.Baseline{Ref: "HEAD"},
	}
	if err := firewall.ValidateExperiment(e, cfgWith("qemu_cycles")); err != nil {
		t.Errorf("valid experiment rejected: %v", err)
	}
}

func TestValidateExperiment_UnregisteredInstrument(t *testing.T) {
	e := &entity.Experiment{
		Hypothesis:  "H-0001",
		Instruments: []string{"ghost_instrument"},
		Baseline:    entity.Baseline{Ref: "HEAD"},
	}
	if err := firewall.ValidateExperiment(e, cfgWith("qemu_cycles")); err == nil {
		t.Error("unregistered instrument should be rejected")
	}
}

func TestCheckInstrumentDependencies(t *testing.T) {
	cfg := &store.Config{Instruments: map[string]store.Instrument{
		"host_test":   {Unit: "bool"},
		"qemu_cycles": {Unit: "cycles", Requires: []string{"host_test=pass"}},
	}}

	t.Run("no observations returns error", func(t *testing.T) {
		err := firewall.CheckInstrumentDependencies("qemu_cycles", cfg, nil)
		if err == nil {
			t.Error("expected error when no observations satisfy the prerequisite")
		}
	})

	t.Run("passing host_test observation returns nil", func(t *testing.T) {
		pass := true
		obs := []*entity.Observation{
			{Instrument: "host_test", Pass: &pass},
		}
		if err := firewall.CheckInstrumentDependencies("qemu_cycles", cfg, obs); err != nil {
			t.Errorf("expected nil with passing prerequisite, got %v", err)
		}
	})

	t.Run("non-passing observation still returns error", func(t *testing.T) {
		fail := false
		obs := []*entity.Observation{
			{Instrument: "host_test", Pass: &fail},
		}
		err := firewall.CheckInstrumentDependencies("qemu_cycles", cfg, obs)
		if err == nil {
			t.Error("expected error when prerequisite observation is not passing")
		}
	})
}

func TestValidateHypothesis_Happy(t *testing.T) {
	h := &entity.Hypothesis{
		Claim: "unroll dsp_fir 4x cuts cycles >10%",
		Predicts: entity.Predicts{
			Instrument: "qemu_cycles", Target: "dsp_fir_bench", Direction: "decrease", MinEffect: 0.10,
		},
		KillIf: []string{"flash delta > 1024 bytes"},
	}
	if err := firewall.ValidateHypothesis(h, cfgWith("qemu_cycles")); err != nil {
		t.Errorf("valid hypothesis rejected: %v", err)
	}
}

func TestCheckHypothesisInstrumentWithinGoal(t *testing.T) {
	max := 65536.0
	goal := &entity.Goal{
		ID: "G-0001",
		Objective: entity.Objective{
			Instrument: "timing",
			Direction:  "decrease",
		},
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

	t.Run("objective instrument allowed", func(t *testing.T) {
		if err := firewall.CheckHypothesisInstrumentWithinGoal(goal, newHypothesis("timing")); err != nil {
			t.Fatalf("objective instrument should pass, got %v", err)
		}
	})

	t.Run("constraint instrument allowed", func(t *testing.T) {
		if err := firewall.CheckHypothesisInstrumentWithinGoal(goal, newHypothesis("binary_size")); err != nil {
			t.Fatalf("constraint instrument should pass, got %v", err)
		}
	})

	t.Run("non-goal instrument rejected", func(t *testing.T) {
		err := firewall.CheckHypothesisInstrumentWithinGoal(goal, newHypothesis("qemu_cycles"))
		if err == nil {
			t.Fatal("expected non-goal instrument to be rejected")
		}
		for _, want := range []string{
			"qemu_cycles",
			"timing, binary_size, test, compile",
			"supporting instruments may still be observed on experiments",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q missing %q", err, want)
			}
		}
	})
}

func TestValidateLesson(t *testing.T) {
	t.Run("happy hypothesis-scope", func(t *testing.T) {
		l := &entity.Lesson{
			Claim:      "unroll past 8× is cache-bound",
			Scope:      entity.LessonScopeHypothesis,
			Subjects:   []string{"C-0003"},
			Status:     entity.LessonStatusProvisional,
			Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceUnreviewedDecisive},
		}
		if err := firewall.ValidateLesson(l); err != nil {
			t.Errorf("valid lesson rejected: %v", err)
		}
	})
	t.Run("happy system-scope", func(t *testing.T) {
		l := &entity.Lesson{
			Claim: "fixture cache is sticky",
			Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive,
		}
		if err := firewall.ValidateLesson(l); err != nil {
			t.Errorf("valid system lesson rejected: %v", err)
		}
	})
	t.Run("system scope rejects subjects", func(t *testing.T) {
		l := &entity.Lesson{
			Claim: "x", Scope: entity.LessonScopeSystem, Subjects: []string{"H-0001"},
		}
		if err := firewall.ValidateLesson(l); err == nil ||
			!strings.Contains(err.Error(), "cannot cite --from subjects") {
			t.Errorf("system scope with subjects should fail, got %v", err)
		}
	})
	t.Run("empty claim", func(t *testing.T) {
		l := &entity.Lesson{Scope: entity.LessonScopeSystem}
		if err := firewall.ValidateLesson(l); err == nil {
			t.Error("empty claim should fail")
		}
	})
	t.Run("invalid scope", func(t *testing.T) {
		l := &entity.Lesson{Claim: "x", Scope: "galactic"}
		if err := firewall.ValidateLesson(l); err == nil ||
			!strings.Contains(err.Error(), "scope must be") {
			t.Errorf("bad scope should fail with scope hint, got %v", err)
		}
	})
	t.Run("hypothesis scope requires subjects", func(t *testing.T) {
		l := &entity.Lesson{Claim: "x", Scope: entity.LessonScopeHypothesis}
		if err := firewall.ValidateLesson(l); err == nil ||
			!strings.Contains(err.Error(), "at least one subject") {
			t.Errorf("hypothesis scope without subjects should fail, got %v", err)
		}
	})
	t.Run("invalid subject prefix", func(t *testing.T) {
		l := &entity.Lesson{
			Claim: "x", Scope: entity.LessonScopeHypothesis, Subjects: []string{"X-0001"},
		}
		if err := firewall.ValidateLesson(l); err == nil ||
			!strings.Contains(err.Error(), "H-/E-/C-") {
			t.Errorf("bad subject prefix should fail, got %v", err)
		}
	})
	t.Run("supersedes must be L-", func(t *testing.T) {
		l := &entity.Lesson{
			Claim: "x", Scope: entity.LessonScopeSystem, SupersedesID: "H-0001",
		}
		if err := firewall.ValidateLesson(l); err == nil ||
			!strings.Contains(err.Error(), "L- id") {
			t.Errorf("non-L supersedes target should fail, got %v", err)
		}
	})
	t.Run("invalid provenance state", func(t *testing.T) {
		l := &entity.Lesson{
			Claim:      "x",
			Scope:      entity.LessonScopeHypothesis,
			Subjects:   []string{"H-0001"},
			Provenance: &entity.LessonProvenance{SourceChain: "sideways"},
		}
		if err := firewall.ValidateLesson(l); err == nil || !strings.Contains(err.Error(), "source_chain") {
			t.Errorf("bad provenance should fail, got %v", err)
		}
	})
}

func TestAssessLessonSourceChain(t *testing.T) {
	t.Run("system lessons resolve to system source", func(t *testing.T) {
		lesson := &entity.Lesson{ID: "L-0000", Scope: entity.LessonScopeSystem}
		got, err := firewall.AssessLessonSourceChain(fakeInspiredByReader{}, lesson)
		if err != nil {
			t.Fatalf("AssessLessonSourceChain failed: %v", err)
		}
		if got != entity.LessonSourceSystem {
			t.Fatalf("source chain = %q, want %q", got, entity.LessonSourceSystem)
		}
	})

	t.Run("malformed system lessons with subjects still resolve from subjects", func(t *testing.T) {
		reader := fakeInspiredByReader{
			hypotheses: map[string]*entity.Hypothesis{
				"H-0003": {ID: "H-0003", Status: entity.StatusUnreviewed},
			},
			conclusions: map[string]*entity.Conclusion{
				"C-0003": {ID: "C-0003", Hypothesis: "H-0003", Verdict: entity.VerdictSupported},
			},
		}
		lesson := &entity.Lesson{ID: "L-0003", Scope: entity.LessonScopeSystem, Subjects: []string{"C-0003"}}
		got, err := firewall.AssessLessonSourceChain(reader, lesson)
		if err != nil {
			t.Fatalf("AssessLessonSourceChain failed: %v", err)
		}
		if got != entity.LessonSourceUnreviewedDecisive {
			t.Fatalf("source chain = %q, want %q", got, entity.LessonSourceUnreviewedDecisive)
		}
	})

	t.Run("reviewed hypothesis resolves to reviewed decisive", func(t *testing.T) {
		reader := fakeInspiredByReader{
			hypotheses: map[string]*entity.Hypothesis{
				"H-0001": {ID: "H-0001", Status: entity.StatusSupported},
			},
		}
		lesson := &entity.Lesson{ID: "L-0001", Scope: entity.LessonScopeHypothesis, Subjects: []string{"H-0001"}}
		got, err := firewall.AssessLessonSourceChain(reader, lesson)
		if err != nil {
			t.Fatalf("AssessLessonSourceChain failed: %v", err)
		}
		if got != entity.LessonSourceReviewedDecisive {
			t.Fatalf("source chain = %q, want %q", got, entity.LessonSourceReviewedDecisive)
		}
	})

	t.Run("unreviewed decisive conclusion resolves to unreviewed decisive", func(t *testing.T) {
		reader := fakeInspiredByReader{
			hypotheses: map[string]*entity.Hypothesis{
				"H-0003": {ID: "H-0003", Status: entity.StatusUnreviewed},
			},
			conclusions: map[string]*entity.Conclusion{
				"C-0003": {ID: "C-0003", Hypothesis: "H-0003", Verdict: entity.VerdictSupported},
			},
		}
		lesson := &entity.Lesson{ID: "L-0003", Scope: entity.LessonScopeHypothesis, Subjects: []string{"C-0003"}}
		got, err := firewall.AssessLessonSourceChain(reader, lesson)
		if err != nil {
			t.Fatalf("AssessLessonSourceChain failed: %v", err)
		}
		if got != entity.LessonSourceUnreviewedDecisive {
			t.Fatalf("source chain = %q, want %q", got, entity.LessonSourceUnreviewedDecisive)
		}
	})

	t.Run("downgraded chain resolves to inconclusive", func(t *testing.T) {
		reader := fakeInspiredByReader{
			hypotheses: map[string]*entity.Hypothesis{
				"H-0004": {ID: "H-0004", Status: entity.StatusInconclusive},
			},
			conclusions: map[string]*entity.Conclusion{
				"C-0004": {ID: "C-0004", Hypothesis: "H-0004", Verdict: entity.VerdictInconclusive},
			},
		}
		lesson := &entity.Lesson{ID: "L-0004", Scope: entity.LessonScopeHypothesis, Subjects: []string{"C-0004"}}
		got, err := firewall.AssessLessonSourceChain(reader, lesson)
		if err != nil {
			t.Fatalf("AssessLessonSourceChain failed: %v", err)
		}
		if got != entity.LessonSourceInconclusive {
			t.Fatalf("source chain = %q, want %q", got, entity.LessonSourceInconclusive)
		}
	})
}

func TestCheckBudgetForNewExperiment(t *testing.T) {
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)

	t.Run("zero budgets pass", func(t *testing.T) {
		st := &store.State{Counters: map[string]int{"E": 42}}
		cfg := &store.Config{}
		if breach := firewall.CheckBudgetForNewExperiment(st, cfg, now); !breach.Ok() {
			t.Fatalf("zero budgets should be ok, got %+v", breach)
		}
	})

	t.Run("max_experiments under limit passes", func(t *testing.T) {
		st := &store.State{Counters: map[string]int{"E": 4}}
		cfg := &store.Config{Budgets: store.Budgets{MaxExperiments: 5}}
		if breach := firewall.CheckBudgetForNewExperiment(st, cfg, now); !breach.Ok() {
			t.Fatalf("under-limit should be ok, got %+v", breach)
		}
	})

	t.Run("max_experiments at limit breaches", func(t *testing.T) {
		st := &store.State{Counters: map[string]int{"E": 5}}
		cfg := &store.Config{Budgets: store.Budgets{MaxExperiments: 5}}
		breach := firewall.CheckBudgetForNewExperiment(st, cfg, now)
		if breach.Ok() {
			t.Fatal("at-limit should breach")
		}
		if breach.Rule != "max_experiments" {
			t.Errorf("rule = %q, want max_experiments", breach.Rule)
		}
		if !strings.Contains(breach.Message, "max_experiments=5") {
			t.Errorf("message missing limit detail: %q", breach.Message)
		}
	})

	t.Run("max_wall_time_h with nil ResearchStartedAt passes", func(t *testing.T) {
		st := &store.State{Counters: map[string]int{"E": 0}}
		cfg := &store.Config{Budgets: store.Budgets{MaxWallTimeH: 24}}
		if breach := firewall.CheckBudgetForNewExperiment(st, cfg, now); !breach.Ok() {
			t.Fatalf("nil start time should be ok, got %+v", breach)
		}
	})

	t.Run("max_wall_time_h under limit passes", func(t *testing.T) {
		start := now.Add(-6 * time.Hour)
		st := &store.State{Counters: map[string]int{"E": 0}, ResearchStartedAt: &start}
		cfg := &store.Config{Budgets: store.Budgets{MaxWallTimeH: 24}}
		if breach := firewall.CheckBudgetForNewExperiment(st, cfg, now); !breach.Ok() {
			t.Fatalf("6h of 24h should be ok, got %+v", breach)
		}
	})

	t.Run("max_wall_time_h past limit breaches", func(t *testing.T) {
		start := now.Add(-25 * time.Hour)
		st := &store.State{Counters: map[string]int{"E": 0}, ResearchStartedAt: &start}
		cfg := &store.Config{Budgets: store.Budgets{MaxWallTimeH: 24}}
		breach := firewall.CheckBudgetForNewExperiment(st, cfg, now)
		if breach.Ok() {
			t.Fatal("past limit should breach")
		}
		if breach.Rule != "max_wall_time_h" {
			t.Errorf("rule = %q, want max_wall_time_h", breach.Rule)
		}
		if !strings.Contains(breach.Message, "max_wall_time_h=24") {
			t.Errorf("message missing limit detail: %q", breach.Message)
		}
	})

	t.Run("max_experiments triggers before max_wall_time_h", func(t *testing.T) {
		start := now.Add(-30 * time.Hour) // also over MaxWallTimeH=24
		st := &store.State{Counters: map[string]int{"E": 5}, ResearchStartedAt: &start}
		cfg := &store.Config{Budgets: store.Budgets{MaxExperiments: 5, MaxWallTimeH: 24}}
		breach := firewall.CheckBudgetForNewExperiment(st, cfg, now)
		if breach.Rule != "max_experiments" {
			t.Errorf("experiments should check first, got %q", breach.Rule)
		}
	})
}

func TestCheckStrictVerdict(t *testing.T) {
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

	t.Run("supported with insufficient samples downgrades", func(t *testing.T) {
		d := firewall.CheckStrictVerdict(entity.VerdictSupported, hyp("decrease", 0.05), cmp(1, -0.2, -0.3, -0.1))
		if d.Passed || !d.Downgraded {
			t.Fatalf("expected downgrade, got %+v", d)
		}
		if d.FinalVerdict != entity.VerdictInconclusive {
			t.Errorf("verdict = %q, want inconclusive", d.FinalVerdict)
		}
	})

	t.Run("supported with nil comparison downgrades", func(t *testing.T) {
		d := firewall.CheckStrictVerdict(entity.VerdictSupported, hyp("decrease", 0.05), nil)
		if d.Passed || !d.Downgraded {
			t.Fatalf("expected downgrade, got %+v", d)
		}
	})

	t.Run("supported increase with CI crossing zero downgrades", func(t *testing.T) {
		// direction=increase, predicts positive delta; CILowFrac <= 0 should flag
		d := firewall.CheckStrictVerdict(entity.VerdictSupported, hyp("increase", 0.05), cmp(10, 0.15, -0.02, 0.30))
		if d.Passed || !d.Downgraded {
			t.Fatalf("expected downgrade when CI crosses zero, got %+v", d)
		}
		joined := strings.Join(d.Reasons, " ")
		if !strings.Contains(joined, "crosses zero") {
			t.Errorf("reasons missing CI-crosses-zero message: %v", d.Reasons)
		}
	})

	t.Run("supported decrease with CI crossing zero downgrades", func(t *testing.T) {
		// direction=decrease, expects negative delta; CIHighFrac >= 0 should flag
		d := firewall.CheckStrictVerdict(entity.VerdictSupported, hyp("decrease", 0.05), cmp(10, -0.15, -0.30, 0.02))
		if d.Passed || !d.Downgraded {
			t.Fatalf("expected downgrade when CI crosses zero, got %+v", d)
		}
	})

	t.Run("supported with effect below min_effect downgrades", func(t *testing.T) {
		d := firewall.CheckStrictVerdict(entity.VerdictSupported, hyp("decrease", 0.20), cmp(10, -0.05, -0.08, -0.02))
		if d.Passed || !d.Downgraded {
			t.Fatalf("expected downgrade for too-small effect, got %+v", d)
		}
		joined := strings.Join(d.Reasons, " ")
		if !strings.Contains(joined, "min_effect") {
			t.Errorf("reasons missing min_effect message: %v", d.Reasons)
		}
	})

	t.Run("supported passing cleanly", func(t *testing.T) {
		d := firewall.CheckStrictVerdict(entity.VerdictSupported, hyp("decrease", 0.05), cmp(10, -0.20, -0.30, -0.10))
		if !d.Passed || d.Downgraded {
			t.Fatalf("expected clean pass, got %+v", d)
		}
		if d.FinalVerdict != entity.VerdictSupported {
			t.Errorf("verdict = %q, want supported", d.FinalVerdict)
		}
	})

	t.Run("refuted with structural evidence notes reason", func(t *testing.T) {
		// predicts decrease but CI lies entirely on the increase side
		d := firewall.CheckStrictVerdict(entity.VerdictRefuted, hyp("decrease", 0.05), cmp(10, 0.20, 0.10, 0.30))
		if !d.Passed {
			t.Fatalf("refuted should still pass, got %+v", d)
		}
		if len(d.Reasons) == 0 {
			t.Errorf("expected structural-refutation reason, got none")
		}
	})

	t.Run("refuted passing cleanly has no reasons", func(t *testing.T) {
		d := firewall.CheckStrictVerdict(entity.VerdictRefuted, hyp("decrease", 0.05), cmp(10, -0.20, -0.30, -0.10))
		if !d.Passed {
			t.Fatalf("refuted should pass, got %+v", d)
		}
	})

	t.Run("inconclusive always passes", func(t *testing.T) {
		d := firewall.CheckStrictVerdict(entity.VerdictInconclusive, hyp("decrease", 0.05), nil)
		if !d.Passed || d.Downgraded {
			t.Fatalf("inconclusive should pass cleanly, got %+v", d)
		}
	})

	t.Run("unknown verdict fails", func(t *testing.T) {
		d := firewall.CheckStrictVerdict("uncertain", hyp("decrease", 0.05), cmp(10, -0.20, -0.30, -0.10))
		if d.Passed {
			t.Fatal("unknown verdict should not pass")
		}
		joined := strings.Join(d.Reasons, " ")
		if !strings.Contains(joined, "unknown verdict") {
			t.Errorf("expected unknown-verdict reason, got: %v", d.Reasons)
		}
	})
}

func TestCheckInspiredByLessonsReviewed(t *testing.T) {
	t.Run("allows active reviewed hypothesis chain", func(t *testing.T) {
		reader := fakeInspiredByReader{
			hypotheses: map[string]*entity.Hypothesis{
				"H-0001": {ID: "H-0001", Status: entity.StatusSupported},
			},
		}
		lessons := []*entity.Lesson{{ID: "L-0001", Scope: entity.LessonScopeHypothesis, Status: entity.LessonStatusActive, Subjects: []string{"H-0001"}}}
		if err := firewall.CheckInspiredByLessonsReviewed(reader, lessons); err != nil {
			t.Fatalf("reviewed hypothesis lesson should pass, got %v", err)
		}
	})

	t.Run("rejects non-active lesson status", func(t *testing.T) {
		reader := fakeInspiredByReader{}
		lessons := []*entity.Lesson{{ID: "L-0002", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusProvisional}}
		err := firewall.CheckInspiredByLessonsReviewed(reader, lessons)
		if err == nil || !strings.Contains(err.Error(), "L-0002") || !strings.Contains(err.Error(), "active") {
			t.Fatalf("expected provisional lesson to be rejected, got %v", err)
		}
	})

	t.Run("rejects unreviewed decisive chains", func(t *testing.T) {
		reader := fakeInspiredByReader{
			hypotheses: map[string]*entity.Hypothesis{
				"H-0003": {ID: "H-0003", Status: entity.StatusUnreviewed},
			},
			conclusions: map[string]*entity.Conclusion{
				"C-0003": {ID: "C-0003", Hypothesis: "H-0003", Verdict: entity.VerdictSupported},
			},
		}
		lessons := []*entity.Lesson{{ID: "L-0003", Scope: entity.LessonScopeHypothesis, Status: entity.LessonStatusActive, Subjects: []string{"C-0003"}}}
		err := firewall.CheckInspiredByLessonsReviewed(reader, lessons)
		if err == nil || !strings.Contains(err.Error(), "unreviewed decisive chain") {
			t.Fatalf("expected unreviewed decisive lesson to be rejected, got %v", err)
		}
	})

	t.Run("rejects malformed system lessons on unreviewed decisive chains", func(t *testing.T) {
		reader := fakeInspiredByReader{
			hypotheses: map[string]*entity.Hypothesis{
				"H-0005": {ID: "H-0005", Status: entity.StatusUnreviewed},
			},
			conclusions: map[string]*entity.Conclusion{
				"C-0005": {ID: "C-0005", Hypothesis: "H-0005", Verdict: entity.VerdictSupported},
			},
		}
		lessons := []*entity.Lesson{{ID: "L-0005", Scope: entity.LessonScopeSystem, Status: entity.LessonStatusActive, Subjects: []string{"C-0005"}}}
		err := firewall.CheckInspiredByLessonsReviewed(reader, lessons)
		if err == nil || !strings.Contains(err.Error(), "unreviewed decisive chain") {
			t.Fatalf("expected malformed system lesson to be rejected, got %v", err)
		}
	})

	t.Run("rejects downgraded or inconclusive chains", func(t *testing.T) {
		reader := fakeInspiredByReader{
			hypotheses: map[string]*entity.Hypothesis{
				"H-0004": {ID: "H-0004", Status: entity.StatusInconclusive},
			},
			conclusions: map[string]*entity.Conclusion{
				"C-0004": {ID: "C-0004", Hypothesis: "H-0004", Verdict: entity.VerdictInconclusive},
			},
		}
		lessons := []*entity.Lesson{{ID: "L-0004", Scope: entity.LessonScopeHypothesis, Status: entity.LessonStatusActive, Subjects: []string{"C-0004"}}}
		err := firewall.CheckInspiredByLessonsReviewed(reader, lessons)
		if err == nil || !strings.Contains(err.Error(), "non-decisive chain") {
			t.Fatalf("expected inconclusive lesson to be rejected, got %v", err)
		}
	})
}

func TestCheckInstrumentSafeToDelete(t *testing.T) {
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

	t.Run("unreferenced instrument is safe", func(t *testing.T) {
		goals := []*entity.Goal{goal("G-0001", entity.GoalStatusActive, "timing")}
		hyps := []*entity.Hypothesis{hyp("H-0001", "timing")}
		os := []*entity.Observation{obs("O-0001", "timing")}
		u := firewall.CheckInstrumentSafeToDelete("binary_size", goals, hyps, os)
		if !u.Ok() {
			t.Errorf("unreferenced should be Ok, got %+v", u)
		}
	})

	t.Run("goal objective reference blocks even with --force", func(t *testing.T) {
		goals := []*entity.Goal{goal("G-0001", entity.GoalStatusActive, "timing")}
		u := firewall.CheckInstrumentSafeToDelete("timing", goals, nil, nil)
		if u.Ok() {
			t.Fatal("goal-objective reference should block")
		}
		if !u.BlocksEvenWithForce() {
			t.Errorf("goal-objective reference should block --force, got %+v", u)
		}
		if len(u.GoalObjectives) != 1 || u.GoalObjectives[0] != "G-0001" {
			t.Errorf("expected [G-0001] objective ref, got %v", u.GoalObjectives)
		}
	})

	t.Run("goal constraint reference blocks by default", func(t *testing.T) {
		goals := []*entity.Goal{goal("G-0001", entity.GoalStatusActive, "timing", "binary_size")}
		u := firewall.CheckInstrumentSafeToDelete("binary_size", goals, nil, nil)
		if u.Ok() {
			t.Fatal("goal-constraint reference should block by default")
		}
		if u.BlocksEvenWithForce() {
			t.Errorf("goal-constraint reference should be force-overridable, got %+v", u)
		}
	})

	t.Run("inactive goal references do not block", func(t *testing.T) {
		goals := []*entity.Goal{goal("G-0001", entity.GoalStatusConcluded, "timing", "binary_size")}
		u := firewall.CheckInstrumentSafeToDelete("timing", goals, nil, nil)
		if !u.Ok() {
			t.Errorf("concluded-goal references should not block, got %+v", u)
		}
	})

	t.Run("hypothesis prediction blocks by default", func(t *testing.T) {
		hyps := []*entity.Hypothesis{hyp("H-0001", "binary_size"), hyp("H-0002", "timing")}
		u := firewall.CheckInstrumentSafeToDelete("binary_size", nil, hyps, nil)
		if u.Ok() || u.BlocksEvenWithForce() {
			t.Fatalf("hypothesis ref should be blocking-but-forceable, got %+v", u)
		}
		if len(u.Hypotheses) != 1 || u.Hypotheses[0] != "H-0001" {
			t.Errorf("expected [H-0001], got %v", u.Hypotheses)
		}
	})

	t.Run("observation blocks by default", func(t *testing.T) {
		os := []*entity.Observation{obs("O-0001", "binary_size")}
		u := firewall.CheckInstrumentSafeToDelete("binary_size", nil, nil, os)
		if u.Ok() || u.BlocksEvenWithForce() {
			t.Fatalf("observation ref should be blocking-but-forceable, got %+v", u)
		}
		if len(u.Observations) != 1 {
			t.Errorf("expected 1 observation ref, got %v", u.Observations)
		}
	})

	t.Run("summary lists every reference class", func(t *testing.T) {
		goals := []*entity.Goal{goal("G-0001", entity.GoalStatusActive, "timing", "binary_size")}
		hyps := []*entity.Hypothesis{hyp("H-0001", "binary_size")}
		os := []*entity.Observation{obs("O-0001", "binary_size")}
		u := firewall.CheckInstrumentSafeToDelete("binary_size", goals, hyps, os)
		s := u.Summary()
		for _, want := range []string{"G-0001", "H-0001", "O-0001"} {
			if !strings.Contains(s, want) {
				t.Errorf("summary missing %q: %s", want, s)
			}
		}
	})

	t.Run("empty summary when safe", func(t *testing.T) {
		u := firewall.CheckInstrumentSafeToDelete("x", nil, nil, nil)
		if u.Summary() != "" {
			t.Errorf("safe usage should have empty summary, got %q", u.Summary())
		}
	})
}
