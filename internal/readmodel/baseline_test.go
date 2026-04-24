package readmodel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type baselineFixture struct {
	s   *store.Store
	now time.Time
}

func newBaselineFixture() *baselineFixture {
	GinkgoHelper()
	s, err := store.Create(GinkgoT().TempDir(), store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	Expect(err).NotTo(HaveOccurred())
	return &baselineFixture{
		s:   s,
		now: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
	}
}

func (f *baselineFixture) writeGoal(id string) *entity.Goal {
	GinkgoHelper()
	g := &entity.Goal{
		ID:        id,
		Status:    entity.GoalStatusActive,
		CreatedAt: &f.now,
		Objective: entity.Objective{Instrument: "timing", Direction: "decrease"},
	}
	Expect(f.s.WriteGoal(g)).To(Succeed())
	return g
}

func (f *baselineFixture) writeHypothesis(id, goalID, parent string) *entity.Hypothesis {
	GinkgoHelper()
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
	Expect(f.s.WriteHypothesis(h)).To(Succeed())
	return h
}

func (f *baselineFixture) writeExperiment(id, hypID, goalID, baselineExp string, isBaseline bool) *entity.Experiment {
	GinkgoHelper()
	if goalID == "" && hypID != "" {
		h, err := f.s.ReadHypothesis(hypID)
		Expect(err).NotTo(HaveOccurred())
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
	Expect(f.s.WriteExperiment(e)).To(Succeed())
	return e
}

func (f *baselineFixture) writeObservation(id, expID, instrument string) {
	GinkgoHelper()
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
	Expect(f.s.WriteObservation(o)).To(Succeed())
}

func (f *baselineFixture) writeScopedObservation(id, expID, instrument string, attempt int, ref, sha string) {
	GinkgoHelper()
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
	Expect(f.s.WriteObservation(o)).To(Succeed())
}

func (f *baselineFixture) writeConclusion(id, hypID, candidateExp string, reviewed bool) {
	GinkgoHelper()
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
	Expect(f.s.WriteConclusion(c)).To(Succeed())
}

func (f *baselineFixture) writeScopedConclusion(id, hypID, obsID string, scope ObservationScope, reviewed bool) {
	GinkgoHelper()
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
	Expect(f.s.WriteConclusion(c)).To(Succeed())
}

func (f *baselineFixture) appendBaselineEvent(expID, goalID string) {
	GinkgoHelper()
	data, err := json.Marshal(map[string]any{"goal": goalID})
	Expect(err).NotTo(HaveOccurred())
	Expect(f.s.AppendEvent(store.Event{
		Ts:      f.now,
		Kind:    "experiment.baseline",
		Actor:   "system",
		Subject: expID,
		Data:    data,
	})).To(Succeed())
}

var _ = Describe("inferred baseline resolution", func() {
	It("prefers a candidate's recorded baseline when that scope is usable", func() {
		f := newBaselineFixture()
		f.writeGoal("G-0001")
		parent := f.writeHypothesis("H-0001", "G-0001", "")
		current := f.writeHypothesis("H-0002", "G-0001", parent.ID)

		goalBaseline := f.writeExperiment("E-0001", "", "G-0001", "", true)
		f.writeObservation("O-0001", goalBaseline.ID, "timing")

		ancestorExp := f.writeExperiment("E-0002", parent.ID, "", "", false)
		f.writeObservation("O-0002", ancestorExp.ID, "timing")
		f.writeConclusion("C-0001", parent.ID, ancestorExp.ID, true)

		recorded := f.writeExperiment("E-0003", "", "G-0001", "", true)
		f.writeObservation("O-0003", recorded.ID, "timing")

		candidate := f.writeExperiment("E-0004", current.ID, "", recorded.ID, false)

		got, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		Expect(got.ExperimentID).To(Equal(recorded.ID))
		Expect(got.Source).To(Equal(BaselineSourceCandidateRecorded))
	})

	It("uses the nearest reviewed supported ancestor", func() {
		f := newBaselineFixture()
		f.writeGoal("G-0001")

		root := f.writeHypothesis("H-0001", "G-0001", "")
		mid := f.writeHypothesis("H-0002", "G-0001", root.ID)
		current := f.writeHypothesis("H-0003", "G-0001", mid.ID)

		rootExp := f.writeExperiment("E-0001", root.ID, "", "", false)
		midExp := f.writeExperiment("E-0002", mid.ID, "", "", false)
		f.writeObservation("O-0001", rootExp.ID, "timing")
		f.writeObservation("O-0002", midExp.ID, "timing")
		f.writeConclusion("C-0001", root.ID, rootExp.ID, true)
		f.writeConclusion("C-0002", mid.ID, midExp.ID, true)

		candidate := f.writeExperiment("E-0003", current.ID, "", "", false)

		got, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		Expect(got.ExperimentID).To(Equal(midExp.ID))
		Expect(got.Source).To(Equal(BaselineSourceAncestorSupported))
		Expect(got.AncestorHypothesis).To(Equal(mid.ID))
		Expect(got.AncestorConclusion).To(Equal("C-0002"))
	})

	It("deduplicates multiple supported conclusions for one ancestor experiment", func() {
		f := newBaselineFixture()
		f.writeGoal("G-0001")

		parent := f.writeHypothesis("H-0001", "G-0001", "")
		current := f.writeHypothesis("H-0002", "G-0001", parent.ID)

		ancestorExp := f.writeExperiment("E-0001", parent.ID, "", "", false)
		f.writeObservation("O-0001", ancestorExp.ID, "timing")
		f.writeConclusion("C-0001", parent.ID, ancestorExp.ID, true)
		f.now = f.now.Add(time.Minute)
		f.writeConclusion("C-0002", parent.ID, ancestorExp.ID, true)

		candidate := f.writeExperiment("E-0002", current.ID, "", "", false)

		got, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		Expect(got.ExperimentID).To(Equal(ancestorExp.ID))
		Expect(got.Source).To(Equal(BaselineSourceAncestorSupported))
		Expect(got.AncestorConclusion).To(Equal("C-0002"))
	})

	It("uses the attempt/ref/SHA scope accepted by the ancestor conclusion", func() {
		f := newBaselineFixture()
		f.writeGoal("G-0001")

		parent := f.writeHypothesis("H-0001", "G-0001", "")
		current := f.writeHypothesis("H-0002", "G-0001", parent.ID)

		ancestorExp := f.writeExperiment("E-0001", parent.ID, "", "", false)
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
		f.writeScopedObservation("O-0001", ancestorExp.ID, "timing", scopeA.Attempt, scopeA.Ref, scopeA.SHA)
		f.writeScopedObservation("O-0002", ancestorExp.ID, "timing", scopeB.Attempt, scopeB.Ref, scopeB.SHA)
		f.writeScopedConclusion("C-0001", parent.ID, "O-0001", scopeA, true)

		candidate := f.writeExperiment("E-0002", current.ID, "", "", false)

		got, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		Expect(got.ExperimentID).To(Equal(ancestorExp.ID))
		Expect(got.Scope()).To(Equal(scopeA))
	})

	It("uses a goal-scoped baseline mapping when no candidate or ancestor baseline is available", func() {
		f := newBaselineFixture()
		f.writeGoal("G-0001")
		f.writeGoal("G-0002")

		otherBase := f.writeExperiment("E-0001", "", "G-0001", "", true)
		wantBase := f.writeExperiment("E-0002", "", "G-0002", "", true)
		f.writeObservation("O-0001", otherBase.ID, "timing")
		f.writeObservation("O-0002", wantBase.ID, "timing")

		current := f.writeHypothesis("H-0001", "G-0002", "")
		candidate := f.writeExperiment("E-0003", current.ID, "", "", false)

		got, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		Expect(got.ExperimentID).To(Equal(wantBase.ID))
		Expect(got.Source).To(Equal(BaselineSourceGoalBaseline))
	})

	It("rejects ambiguous goal-scoped baseline scopes", func() {
		f := newBaselineFixture()
		f.writeGoal("G-0001")

		baseA := f.writeExperiment("E-0001", "", "G-0001", "", true)
		baseB := f.writeExperiment("E-0002", "", "G-0001", "", true)
		f.writeObservation("O-0001", baseA.ID, "timing")
		f.writeObservation("O-0002", baseB.ID, "timing")

		current := f.writeHypothesis("H-0001", "G-0001", "")
		candidate := f.writeExperiment("E-0003", current.ID, "", "", false)

		_, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
		Expect(err).To(MatchError(ContainSubstring("multiple baseline scopes")))
	})

	It("rejects ambiguous candidate-recorded observation scopes", func() {
		f := newBaselineFixture()
		f.writeGoal("G-0001")
		current := f.writeHypothesis("H-0001", "G-0001", "")

		recorded := f.writeExperiment("E-0001", "", "G-0001", "", true)
		f.writeScopedObservation("O-0001", recorded.ID, "timing", 1, "refs/heads/baseline/E-0001-a1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		f.writeScopedObservation("O-0002", recorded.ID, "timing", 2, "refs/heads/baseline/E-0001-a2", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

		candidate := f.writeExperiment("E-0002", current.ID, "", recorded.ID, false)

		_, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
		Expect(err).To(MatchError(ContainSubstring("multiple observation scopes")))
	})

	It("propagates observation read errors", func() {
		f := newBaselineFixture()
		f.writeGoal("G-0001")
		current := f.writeHypothesis("H-0001", "G-0001", "")
		candidate := f.writeExperiment("E-0001", current.ID, "", "", false)

		badPath := filepath.Join(f.s.ObservationsDir(), "O-9999.json")
		Expect(os.WriteFile(badPath, []byte("{not json"), 0o644)).To(Succeed())

		_, err := ResolveInferredBaseline(f.s, current, candidate, "timing")
		Expect(err).To(MatchError(ContainSubstring("parse observation")))
	})
})
