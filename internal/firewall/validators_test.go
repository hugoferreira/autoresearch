package firewall_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
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
