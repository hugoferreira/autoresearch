package readmodel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
)

type baselineFixture struct {
	s   *store.Store
	now time.Time
}

func newBaselineFixture(t testkit.T) *baselineFixture {
	t.Helper()
	s, err := store.Create(t.TempDir(), store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &baselineFixture{
		s:   s,
		now: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
	}
}

func (f *baselineFixture) writeGoal(t testkit.T, id string) *entity.Goal {
	t.Helper()
	g := &entity.Goal{
		ID:        id,
		Status:    entity.GoalStatusActive,
		CreatedAt: &f.now,
		Objective: entity.Objective{Instrument: "timing", Direction: "decrease"},
	}
	if err := f.s.WriteGoal(g); err != nil {
		t.Fatal(err)
	}
	return g
}

func (f *baselineFixture) writeHypothesis(t testkit.T, id, goalID, parent string) *entity.Hypothesis {
	t.Helper()
	h := &entity.Hypothesis{
		ID:        id,
		GoalID:    goalID,
		Parent:    parent,
		Claim:     id,
		Status:    entity.StatusOpen,
		Author:    "agent:analyst",
		CreatedAt: f.now,
		Predicts: entity.Predicts{
			Instrument: "timing",
			Target:     "kernel",
			Direction:  "decrease",
			MinEffect:  0.05,
		},
	}
	if err := f.s.WriteHypothesis(h); err != nil {
		t.Fatal(err)
	}
	return h
}

func (f *baselineFixture) writeExperiment(t testkit.T, id, hypID, goalID, baselineExp string, isBaseline bool) *entity.Experiment {
	t.Helper()
	if goalID == "" && hypID != "" {
		h, err := f.s.ReadHypothesis(hypID)
		if err != nil {
			t.Fatal(err)
		}
		goalID = h.GoalID
	}
	e := &entity.Experiment{
		ID:         id,
		GoalID:     goalID,
		Hypothesis: hypID,
		IsBaseline: isBaseline,
		Status:     entity.ExpMeasured,
		Baseline: entity.Baseline{
			Ref:        "HEAD",
			SHA:        "abc123",
			Experiment: baselineExp,
		},
		Instruments: []string{"timing"},
		Author:      "agent:observer",
		CreatedAt:   f.now,
	}
	if err := f.s.WriteExperiment(e); err != nil {
		t.Fatal(err)
	}
	return e
}

func (f *baselineFixture) writeObservation(t testkit.T, id, expID, instrument string) {
	t.Helper()
	o := &entity.Observation{
		ID:         id,
		Experiment: expID,
		Instrument: instrument,
		MeasuredAt: f.now,
		Value:      100,
		Samples:    3,
		PerSample:  []float64{100, 101, 99},
		Unit:       "ns",
		Author:     "agent:observer",
	}
	if err := f.s.WriteObservation(o); err != nil {
		t.Fatal(err)
	}
}

func (f *baselineFixture) writeScopedObservation(t testkit.T, id, expID, instrument string, attempt int, ref, sha string) {
	t.Helper()
	o := &entity.Observation{
		ID:           id,
		Experiment:   expID,
		Instrument:   instrument,
		MeasuredAt:   f.now,
		Value:        100,
		Samples:      3,
		PerSample:    []float64{100, 101, 99},
		Unit:         "ns",
		Attempt:      attempt,
		CandidateRef: ref,
		CandidateSHA: sha,
		Author:       "agent:observer",
	}
	if err := f.s.WriteObservation(o); err != nil {
		t.Fatal(err)
	}
}

func (f *baselineFixture) writeConclusion(t testkit.T, id, hypID, candidateExp string, reviewed bool) {
	t.Helper()
	c := &entity.Conclusion{
		ID:           id,
		Hypothesis:   hypID,
		Verdict:      entity.VerdictSupported,
		Observations: []string{"O-" + id},
		CandidateExp: candidateExp,
		Effect:       entity.Effect{Instrument: "timing", DeltaFrac: -0.1},
		StatTest:     "mann_whitney_u",
		Author:       "agent:analyst",
		CreatedAt:    f.now,
	}
	if reviewed {
		c.ReviewedBy = "human:gate"
	}
	if err := f.s.WriteConclusion(c); err != nil {
		t.Fatal(err)
	}
}

func (f *baselineFixture) writeScopedConclusion(t testkit.T, id, hypID, obsID string, scope ObservationScope, reviewed bool) {
	t.Helper()
	c := &entity.Conclusion{
		ID:               id,
		Hypothesis:       hypID,
		Verdict:          entity.VerdictSupported,
		Observations:     []string{obsID},
		CandidateExp:     scope.Experiment,
		CandidateAttempt: scope.Attempt,
		CandidateRef:     scope.Ref,
		CandidateSHA:     scope.SHA,
		Effect:           entity.Effect{Instrument: "timing", DeltaFrac: -0.1},
		StatTest:         "mann_whitney_u",
		Author:           "agent:analyst",
		CreatedAt:        f.now,
	}
	if reviewed {
		c.ReviewedBy = "human:gate"
	}
	if err := f.s.WriteConclusion(c); err != nil {
		t.Fatal(err)
	}
}

func (f *baselineFixture) appendBaselineEvent(t testkit.T, expID, goalID string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"goal": goalID})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.AppendEvent(store.Event{
		Ts:      f.now,
		Kind:    "experiment.baseline",
		Actor:   "system",
		Subject: expID,
		Data:    data,
	}); err != nil {
		t.Fatal(err)
	}
}

var _ = testkit.Spec("TestResolveInferredBaseline_UsesCandidateRecordedWhenUsable", func(t testkit.T) {
	f := newBaselineFixture(t)
	f.writeGoal(t, "G-0001")
	parent := f.writeHypothesis(t, "H-0001", "G-0001", "")
	current := f.writeHypothesis(t, "H-0002", "G-0001", parent.ID)

	goalBaseline := f.writeExperiment(t, "E-0001", "", "G-0001", "", true)
	f.writeObservation(t, "O-0001", goalBaseline.ID, "timing")

	ancestorExp := f.writeExperiment(t, "E-0002", parent.ID, "", "", false)
	f.writeObservation(t, "O-0002", ancestorExp.ID, "timing")
	f.writeConclusion(t, "C-0001", parent.ID, ancestorExp.ID, true)

	recorded := f.writeExperiment(t, "E-0003", "", "G-0001", "", true)
	f.writeObservation(t, "O-0003", recorded.ID, "timing")

	candidate := f.writeExperiment(t, "E-0004", current.ID, "", recorded.ID, false)

	got, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("ResolveInferredBaseline returned nil result")
	}
	if got.ExperimentID != recorded.ID {
		t.Fatalf("experiment = %q, want %q", got.ExperimentID, recorded.ID)
	}
	if got.Source != BaselineSourceCandidateRecorded {
		t.Fatalf("source = %q, want %q", got.Source, BaselineSourceCandidateRecorded)
	}
})

var _ = testkit.Spec("TestResolveInferredBaseline_PrefersNearestAcceptedSupportedAncestor", func(t testkit.T) {
	f := newBaselineFixture(t)
	f.writeGoal(t, "G-0001")

	root := f.writeHypothesis(t, "H-0001", "G-0001", "")
	mid := f.writeHypothesis(t, "H-0002", "G-0001", root.ID)
	current := f.writeHypothesis(t, "H-0003", "G-0001", mid.ID)

	rootExp := f.writeExperiment(t, "E-0001", root.ID, "", "", false)
	midExp := f.writeExperiment(t, "E-0002", mid.ID, "", "", false)
	f.writeObservation(t, "O-0001", rootExp.ID, "timing")
	f.writeObservation(t, "O-0002", midExp.ID, "timing")
	f.writeConclusion(t, "C-0001", root.ID, rootExp.ID, true)
	f.writeConclusion(t, "C-0002", mid.ID, midExp.ID, true)

	candidate := f.writeExperiment(t, "E-0003", current.ID, "", "", false)

	got, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("ResolveInferredBaseline returned nil result")
	}
	if got.ExperimentID != midExp.ID {
		t.Fatalf("experiment = %q, want %q", got.ExperimentID, midExp.ID)
	}
	if got.Source != BaselineSourceAncestorSupported {
		t.Fatalf("source = %q, want %q", got.Source, BaselineSourceAncestorSupported)
	}
	if got.AncestorHypothesis != mid.ID {
		t.Fatalf("ancestor hypothesis = %q, want %q", got.AncestorHypothesis, mid.ID)
	}
	if got.AncestorConclusion != "C-0002" {
		t.Fatalf("ancestor conclusion = %q, want %q", got.AncestorConclusion, "C-0002")
	}
})

var _ = testkit.Spec("TestResolveInferredBaseline_DedupesSupportedConclusionsOnSameAncestorExperiment", func(t testkit.T) {
	f := newBaselineFixture(t)
	f.writeGoal(t, "G-0001")

	parent := f.writeHypothesis(t, "H-0001", "G-0001", "")
	current := f.writeHypothesis(t, "H-0002", "G-0001", parent.ID)

	ancestorExp := f.writeExperiment(t, "E-0001", parent.ID, "", "", false)
	f.writeObservation(t, "O-0001", ancestorExp.ID, "timing")
	f.writeConclusion(t, "C-0001", parent.ID, ancestorExp.ID, true)
	f.now = f.now.Add(time.Minute)
	f.writeConclusion(t, "C-0002", parent.ID, ancestorExp.ID, true)

	candidate := f.writeExperiment(t, "E-0002", current.ID, "", "", false)

	got, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("ResolveInferredBaseline returned nil result")
	}
	if got.ExperimentID != ancestorExp.ID {
		t.Fatalf("experiment = %q, want %q", got.ExperimentID, ancestorExp.ID)
	}
	if got.Source != BaselineSourceAncestorSupported {
		t.Fatalf("source = %q, want %q", got.Source, BaselineSourceAncestorSupported)
	}
	if got.AncestorConclusion != "C-0002" {
		t.Fatalf("ancestor conclusion = %q, want %q", got.AncestorConclusion, "C-0002")
	}
})

var _ = testkit.Spec("TestResolveInferredBaseline_UsesAcceptedAncestorScope", func(t testkit.T) {
	f := newBaselineFixture(t)
	f.writeGoal(t, "G-0001")

	parent := f.writeHypothesis(t, "H-0001", "G-0001", "")
	current := f.writeHypothesis(t, "H-0002", "G-0001", parent.ID)

	ancestorExp := f.writeExperiment(t, "E-0001", parent.ID, "", "", false)
	scopeA := ObservationScope{
		Experiment: ancestorExp.ID,
		Attempt:    1,
		Ref:        "refs/heads/candidate/E-0001-a1",
		SHA:        "1111111111111111111111111111111111111111",
	}
	scopeB := ObservationScope{
		Experiment: ancestorExp.ID,
		Attempt:    2,
		Ref:        "refs/heads/candidate/E-0001-a2",
		SHA:        "2222222222222222222222222222222222222222",
	}
	f.writeScopedObservation(t, "O-0001", ancestorExp.ID, "timing", scopeA.Attempt, scopeA.Ref, scopeA.SHA)
	f.writeScopedObservation(t, "O-0002", ancestorExp.ID, "timing", scopeB.Attempt, scopeB.Ref, scopeB.SHA)
	f.writeScopedConclusion(t, "C-0001", parent.ID, "O-0001", scopeA, true)

	candidate := f.writeExperiment(t, "E-0002", current.ID, "", "", false)

	got, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("ResolveInferredBaseline returned nil result")
	}
	if got.ExperimentID != ancestorExp.ID {
		t.Fatalf("experiment = %q, want %q", got.ExperimentID, ancestorExp.ID)
	}
	if got.Attempt != scopeA.Attempt || got.Ref != scopeA.Ref || got.SHA != scopeA.SHA {
		t.Fatalf("ancestor scope = %+v, want %+v", got.Scope(), scopeA)
	}
})

var _ = testkit.Spec("TestResolveInferredBaseline_UsesGoalScopedBaselineMapping", func(t testkit.T) {
	f := newBaselineFixture(t)
	f.writeGoal(t, "G-0001")
	f.writeGoal(t, "G-0002")

	otherBase := f.writeExperiment(t, "E-0001", "", "G-0001", "", true)
	wantBase := f.writeExperiment(t, "E-0002", "", "G-0002", "", true)
	f.writeObservation(t, "O-0001", otherBase.ID, "timing")
	f.writeObservation(t, "O-0002", wantBase.ID, "timing")

	current := f.writeHypothesis(t, "H-0001", "G-0002", "")
	candidate := f.writeExperiment(t, "E-0003", current.ID, "", "", false)

	got, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("ResolveInferredBaseline returned nil result")
	}
	if got.ExperimentID != wantBase.ID {
		t.Fatalf("experiment = %q, want %q", got.ExperimentID, wantBase.ID)
	}
	if got.Source != BaselineSourceGoalBaseline {
		t.Fatalf("source = %q, want %q", got.Source, BaselineSourceGoalBaseline)
	}
})

var _ = testkit.Spec("TestResolveInferredBaseline_ErrorsOnAmbiguousGoalBaseline", func(t testkit.T) {
	f := newBaselineFixture(t)
	f.writeGoal(t, "G-0001")

	baseA := f.writeExperiment(t, "E-0001", "", "G-0001", "", true)
	baseB := f.writeExperiment(t, "E-0002", "", "G-0001", "", true)
	f.writeObservation(t, "O-0001", baseA.ID, "timing")
	f.writeObservation(t, "O-0002", baseB.ID, "timing")

	current := f.writeHypothesis(t, "H-0001", "G-0001", "")
	candidate := f.writeExperiment(t, "E-0003", current.ID, "", "", false)

	_, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
	if err == nil {
		t.Fatal("ResolveInferredBaseline unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "multiple baseline scopes") {
		t.Fatalf("error = %q, want multiple baseline scopes", err)
	}
})

var _ = testkit.Spec("TestResolveInferredBaseline_ErrorsOnAmbiguousCandidateRecordedScope", func(t testkit.T) {
	f := newBaselineFixture(t)
	f.writeGoal(t, "G-0001")
	current := f.writeHypothesis(t, "H-0001", "G-0001", "")

	recorded := f.writeExperiment(t, "E-0001", "", "G-0001", "", true)
	f.writeScopedObservation(t, "O-0001", recorded.ID, "timing", 1, "refs/heads/baseline/E-0001-a1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	f.writeScopedObservation(t, "O-0002", recorded.ID, "timing", 2, "refs/heads/baseline/E-0001-a2", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	candidate := f.writeExperiment(t, "E-0002", current.ID, "", recorded.ID, false)

	_, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
	if err == nil {
		t.Fatal("ResolveInferredBaseline unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "multiple observation scopes") {
		t.Fatalf("error = %q, want multiple observation scopes", err)
	}
})

var _ = testkit.Spec("TestResolveInferredBaseline_PropagatesObservationReadErrors", func(t testkit.T) {
	f := newBaselineFixture(t)
	f.writeGoal(t, "G-0001")
	current := f.writeHypothesis(t, "H-0001", "G-0001", "")
	candidate := f.writeExperiment(t, "E-0001", current.ID, "", "", false)

	badPath := filepath.Join(f.s.ObservationsDir(), "O-9999.json")
	if err := os.WriteFile(badPath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
	if err == nil {
		t.Fatal("ResolveInferredBaseline unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "parse observation") {
		t.Fatalf("error = %q, want parse observation", err)
	}
})
