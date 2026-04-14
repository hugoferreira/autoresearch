package cli

import (
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

func scopedFixtureStore(t *testing.T) *store.Store {
	t.Helper()

	s, err := store.Create(t.TempDir(), store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	g1 := &entity.Goal{
		ID:        "G-0001",
		Status:    entity.GoalStatusConcluded,
		CreatedAt: &now,
		Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
		Constraints: []entity.Constraint{
			{Instrument: "host_test", Require: "pass"},
		},
	}
	g2 := &entity.Goal{
		ID:        "G-0002",
		Status:    entity.GoalStatusActive,
		CreatedAt: &now,
		Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"},
		Constraints: []entity.Constraint{
			{Instrument: "size_flash", Max: ptrFloat(131072)},
		},
	}
	for _, g := range []*entity.Goal{g1, g2} {
		if err := s.WriteGoal(g); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UpdateState(func(st *store.State) error {
		st.CurrentGoalID = g2.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	h1 := &entity.Hypothesis{
		ID: "H-0001", GoalID: g1.ID, Claim: "goal 1 hypothesis", Status: entity.StatusOpen, Author: "human", CreatedAt: now,
		Predicts: entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease"},
	}
	h2 := &entity.Hypothesis{
		ID: "H-0002", GoalID: g2.ID, Claim: "goal 2 hypothesis", Status: entity.StatusOpen, Author: "human", CreatedAt: now,
		Predicts: entity.Predicts{Instrument: "qemu_cycles", Target: "fir", Direction: "decrease"},
	}
	for _, h := range []*entity.Hypothesis{h1, h2} {
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}
	}

	base := &entity.Experiment{
		ID: "E-0001", IsBaseline: true, Status: entity.ExpMeasured,
		Baseline:    entity.Baseline{Ref: "HEAD", SHA: "abc123"},
		Instruments: []string{"host_timing"},
		Author:      "system", CreatedAt: now,
	}
	e1 := &entity.Experiment{
		ID: "E-0002", Hypothesis: h1.ID, Status: entity.ExpMeasured,
		Baseline:    entity.Baseline{Ref: "HEAD", SHA: "abc123"},
		Instruments: []string{"host_timing"},
		Author:      "system", CreatedAt: now,
	}
	e2 := &entity.Experiment{
		ID: "E-0003", Hypothesis: h2.ID, Status: entity.ExpMeasured,
		Baseline:    entity.Baseline{Ref: "HEAD", SHA: "def456"},
		Instruments: []string{"qemu_cycles"},
		Author:      "system", CreatedAt: now,
	}
	for _, e := range []*entity.Experiment{base, e1, e2} {
		if err := s.WriteExperiment(e); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.WriteObservation(&entity.Observation{
		ID: "O-0001", Experiment: base.ID, Instrument: "host_timing", MeasuredAt: now, Value: 1.0, Unit: "s", Samples: 3, Author: "system",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteObservation(&entity.Observation{
		ID: "O-0002", Experiment: e2.ID, Instrument: "qemu_cycles", MeasuredAt: now, Value: 100, Unit: "cycles", Samples: 3, Author: "system",
	}); err != nil {
		t.Fatal(err)
	}

	c1 := &entity.Conclusion{
		ID: "C-0001", Hypothesis: h1.ID, Verdict: entity.VerdictSupported,
		CandidateExp: e1.ID, Effect: entity.Effect{Instrument: "host_timing", DeltaFrac: -0.1},
		StatTest: "welch", Author: "agent:analyst", CreatedAt: now,
	}
	c2 := &entity.Conclusion{
		ID: "C-0002", Hypothesis: h2.ID, Verdict: entity.VerdictSupported,
		CandidateExp: e2.ID, Effect: entity.Effect{Instrument: "qemu_cycles", DeltaFrac: -0.2},
		StatTest: "welch", Author: "agent:analyst", CreatedAt: now,
	}
	for _, c := range []*entity.Conclusion{c1, c2} {
		if err := s.WriteConclusion(c); err != nil {
			t.Fatal(err)
		}
	}

	l1 := &entity.Lesson{
		ID: "L-0001", Claim: "goal 1 lesson", Scope: entity.LessonScopeHypothesis, Subjects: []string{h1.ID},
		Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: now,
	}
	l2 := &entity.Lesson{
		ID: "L-0002", Claim: "system lesson", Scope: entity.LessonScopeSystem,
		Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: now,
	}
	l3 := &entity.Lesson{
		ID: "L-0003", Claim: "goal 2 lesson", Scope: entity.LessonScopeHypothesis, Subjects: []string{c2.ID},
		Status: entity.LessonStatusActive, Author: "agent:analyst", CreatedAt: now,
	}
	for _, l := range []*entity.Lesson{l1, l2, l3} {
		if err := s.WriteLesson(l); err != nil {
			t.Fatal(err)
		}
	}

	events := []store.Event{
		{Ts: now, Kind: "goal.new", Actor: "human", Subject: g1.ID},
		{Ts: now, Kind: "goal.new", Actor: "human", Subject: g2.ID},
		{Ts: now, Kind: "hypothesis.add", Actor: "human", Subject: h1.ID},
		{Ts: now, Kind: "hypothesis.add", Actor: "human", Subject: h2.ID},
		{Ts: now, Kind: "experiment.baseline", Actor: "system", Subject: base.ID, Data: jsonRaw(map[string]any{"goal": g1.ID})},
		{Ts: now, Kind: "observation.record", Actor: "system", Subject: "O-0001"},
		{Ts: now, Kind: "lesson.add", Actor: "agent:analyst", Subject: l1.ID},
		{Ts: now, Kind: "lesson.add", Actor: "agent:analyst", Subject: l2.ID},
		{Ts: now, Kind: "pause", Actor: "human"},
	}
	for _, ev := range events {
		if err := s.AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
	}

	return s
}

func ptrFloat(v float64) *float64 { return &v }

func TestResolveGoalScope_DefaultsAndAll(t *testing.T) {
	s, err := store.Create(t.TempDir(), store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}

	scope, err := resolveGoalScope(s, "")
	if err != nil {
		t.Fatal(err)
	}
	if !scope.All {
		t.Fatalf("empty scope with no active goal should default to all, got %+v", scope)
	}

	now := time.Now().UTC()
	goal := &entity.Goal{
		ID: "G-0001", Status: entity.GoalStatusActive, CreatedAt: &now,
		Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
	}
	if err := s.WriteGoal(goal); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateState(func(st *store.State) error {
		st.CurrentGoalID = goal.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	scope, err = resolveGoalScope(s, "")
	if err != nil {
		t.Fatal(err)
	}
	if scope.All || scope.GoalID != goal.ID {
		t.Fatalf("default scope = %+v, want %s", scope, goal.ID)
	}

	scope, err = resolveGoalScope(s, goalScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if !scope.All {
		t.Fatalf("--goal all should resolve to all, got %+v", scope)
	}

	if _, err := resolveGoalScope(s, "G-9999"); err == nil {
		t.Fatal("expected unknown explicit goal to fail")
	}
}

func TestGoalScopeResolver_FiltersBaselineLessonsAndEvents(t *testing.T) {
	s := scopedFixtureStore(t)
	r := newGoalScopeResolver(s, goalScope{GoalID: "G-0001"})

	exps, err := s.ListExperiments()
	if err != nil {
		t.Fatal(err)
	}
	exps, err = r.filterExperiments(exps)
	if err != nil {
		t.Fatal(err)
	}
	if len(exps) != 2 {
		t.Fatalf("scoped experiments = %d, want 2", len(exps))
	}
	if exps[0].ID != "E-0001" || exps[1].ID != "E-0002" {
		t.Fatalf("unexpected scoped experiments: %s, %s", exps[0].ID, exps[1].ID)
	}

	obs, err := s.ListObservations()
	if err != nil {
		t.Fatal(err)
	}
	obs, err = r.filterObservations(obs)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 1 || obs[0].ID != "O-0001" {
		t.Fatalf("scoped observations = %+v, want O-0001 only", obs)
	}

	lessons, err := s.ListLessons()
	if err != nil {
		t.Fatal(err)
	}
	lessons, err = r.filterLessons(lessons)
	if err != nil {
		t.Fatal(err)
	}
	if len(lessons) != 2 {
		t.Fatalf("scoped lessons = %d, want 2", len(lessons))
	}
	if lessons[0].ID != "L-0001" || lessons[1].ID != "L-0002" {
		t.Fatalf("unexpected lesson scope: %s, %s", lessons[0].ID, lessons[1].ID)
	}

	events, err := s.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	events, err = r.filterEvents(events)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"goal.new:G-0001":            true,
		"hypothesis.add:H-0001":      true,
		"experiment.baseline:E-0001": true,
		"observation.record:O-0001":  true,
		"lesson.add:L-0001":          true,
		"lesson.add:L-0002":          true,
	}
	if len(events) != len(want) {
		t.Fatalf("scoped events = %d, want %d", len(events), len(want))
	}
	for _, ev := range events {
		key := ev.Kind + ":" + ev.Subject
		if !want[key] {
			t.Fatalf("unexpected scoped event %s", key)
		}
	}
}

func TestCaptureDashboard_DefaultScopeTracksActiveGoal(t *testing.T) {
	s := scopedFixtureStore(t)

	snap, err := captureDashboard(s)
	if err != nil {
		t.Fatal(err)
	}
	if snap.ScopeAll || snap.ScopeGoalID != "G-0002" {
		t.Fatalf("dashboard scope = %+v, want goal G-0002", snap)
	}
	if got := snap.Counts["hypotheses"]; got != 1 {
		t.Fatalf("scoped hypothesis count = %d, want 1", got)
	}
	if len(snap.Tree) != 1 || snap.Tree[0].ID != "H-0002" {
		t.Fatalf("scoped tree = %+v, want H-0002 only", snap.Tree)
	}
	if got := len(snap.RecentLessons); got != 2 {
		t.Fatalf("scoped lessons = %d, want goal lesson + system lesson", got)
	}

	allSnap, err := captureDashboardScoped(s, goalScope{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if !allSnap.ScopeAll {
		t.Fatalf("all-goal dashboard should report scope_all=true")
	}
	if got := allSnap.Counts["hypotheses"]; got != 2 {
		t.Fatalf("all-goal hypothesis count = %d, want 2", got)
	}
	if got := len(allSnap.RecentLessons); got != 3 {
		t.Fatalf("all-goal lessons = %d, want 3", got)
	}
}
