package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func scopedFixtureStore() *store.Store {
	GinkgoHelper()

	s, err := store.Create(GinkgoT().TempDir(), store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	Expect(err).NotTo(HaveOccurred())

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
		Expect(s.WriteGoal(g)).To(Succeed())
	}
	Expect(s.UpdateState(func(st *store.State) error {
		st.CurrentGoalID = g2.ID
		return nil
	})).To(Succeed())

	h1 := &entity.Hypothesis{
		ID: "H-0001", GoalID: g1.ID, Claim: "goal 1 hypothesis", Status: entity.StatusOpen, Author: "human", CreatedAt: now,
		Predicts: entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease"},
	}
	h2 := &entity.Hypothesis{
		ID: "H-0002", GoalID: g2.ID, Claim: "goal 2 hypothesis", Status: entity.StatusOpen, Author: "human", CreatedAt: now,
		Predicts: entity.Predicts{Instrument: "qemu_cycles", Target: "fir", Direction: "decrease"},
	}
	for _, h := range []*entity.Hypothesis{h1, h2} {
		Expect(s.WriteHypothesis(h)).To(Succeed())
	}

	base := &entity.Experiment{
		ID: "E-0001", GoalID: g1.ID, IsBaseline: true, Status: entity.ExpMeasured,
		Baseline:    entity.Baseline{Ref: "HEAD", SHA: "abc123"},
		Instruments: []string{"host_timing"},
		Author:      "system", CreatedAt: now,
	}
	e1 := &entity.Experiment{
		ID: "E-0002", GoalID: g1.ID, Hypothesis: h1.ID, Status: entity.ExpMeasured,
		Baseline:    entity.Baseline{Ref: "HEAD", SHA: "abc123"},
		Instruments: []string{"host_timing"},
		Author:      "system", CreatedAt: now,
	}
	e2 := &entity.Experiment{
		ID: "E-0003", GoalID: g2.ID, Hypothesis: h2.ID, Status: entity.ExpMeasured,
		Baseline:    entity.Baseline{Ref: "HEAD", SHA: "def456"},
		Instruments: []string{"qemu_cycles"},
		Author:      "system", CreatedAt: now,
	}
	for _, e := range []*entity.Experiment{base, e1, e2} {
		Expect(s.WriteExperiment(e)).To(Succeed())
	}

	Expect(s.WriteObservation(&entity.Observation{
		ID: "O-0001", Experiment: base.ID, Instrument: "host_timing", MeasuredAt: now, Value: 1.0, Unit: "s", Samples: 3, Author: "system",
	})).To(Succeed())
	Expect(s.WriteObservation(&entity.Observation{
		ID: "O-0002", Experiment: e2.ID, Instrument: "qemu_cycles", MeasuredAt: now, Value: 100, Unit: "cycles", Samples: 3, Author: "system",
	})).To(Succeed())

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
		Expect(s.WriteConclusion(c)).To(Succeed())
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
		Expect(s.WriteLesson(l)).To(Succeed())
	}

	events := []store.Event{
		{Ts: now, Kind: "goal.new", Actor: "human", Subject: g1.ID},
		{Ts: now, Kind: "goal.new", Actor: "human", Subject: g2.ID},
		{Ts: now, Kind: "hypothesis.add", Actor: "human", Subject: h1.ID},
		{Ts: now, Kind: "hypothesis.add", Actor: "human", Subject: h2.ID},
		{Ts: now, Kind: "experiment.baseline", Actor: "system", Subject: base.ID, Data: jsonRaw(map[string]any{"note": "legacy payload ignored once experiment.goal_id is present"})},
		{Ts: now, Kind: "observation.record", Actor: "system", Subject: "O-0001"},
		{Ts: now, Kind: "lesson.add", Actor: "agent:analyst", Subject: l1.ID},
		{Ts: now, Kind: "lesson.add", Actor: "agent:analyst", Subject: l2.ID},
		{Ts: now, Kind: "pause", Actor: "human"},
	}
	for _, ev := range events {
		Expect(s.AppendEvent(ev)).To(Succeed())
	}

	return s
}

func ptrFloat(v float64) *float64 { return &v }

func scopedSystemLessonAccuracyFixtureStore() (*store.Store, goalScope) {
	GinkgoHelper()

	s, err := store.Create(GinkgoT().TempDir(), store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	Expect(err).NotTo(HaveOccurred())

	base := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	g1 := &entity.Goal{
		ID:        "G-0001",
		Status:    entity.GoalStatusActive,
		CreatedAt: &base,
		Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
	}
	g2 := &entity.Goal{
		ID:        "G-0002",
		Status:    entity.GoalStatusConcluded,
		CreatedAt: &base,
		Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
	}
	for _, g := range []*entity.Goal{g1, g2} {
		Expect(s.WriteGoal(g)).To(Succeed())
	}

	// Legacy malformed system lesson: read surfaces should still avoid
	// coarse fallback if linked hypotheses exist outside the scoped goal.
	lesson := &entity.Lesson{
		ID:    "L-0007",
		Claim: "legacy system steering lesson",
		Scope: entity.LessonScopeSystem,
		PredictedEffect: &entity.PredictedEffect{
			Instrument: "host_timing",
			Direction:  "decrease",
			MinEffect:  0.10,
			MaxEffect:  0.20,
		},
		Status:     entity.LessonStatusActive,
		Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceSystem},
		Author:     "agent:analyst",
		CreatedAt:  base.Add(time.Minute),
	}
	Expect(s.WriteLesson(lesson)).To(Succeed())

	inScope := &entity.Hypothesis{
		ID: "H-0103", GoalID: g1.ID, Claim: "in-scope unrelated hypothesis",
		Status: entity.StatusSupported, Author: "agent:analyst", CreatedAt: base.Add(2 * time.Minute),
		Predicts: entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
	}
	outOfScopeLinked := &entity.Hypothesis{
		ID: "H-0201", GoalID: g2.ID, Claim: "out-of-scope linked hypothesis",
		InspiredBy: []string{lesson.ID},
		Status:     entity.StatusOpen, Author: "agent:analyst", CreatedAt: base.Add(2 * time.Minute),
		Predicts: entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
	}
	for _, h := range []*entity.Hypothesis{inScope, outOfScopeLinked} {
		Expect(s.WriteHypothesis(h)).To(Succeed())
	}

	unrelatedConclusion := &entity.Conclusion{
		ID:         "C-0103",
		Hypothesis: inScope.ID,
		Verdict:    entity.VerdictSupported,
		Effect:     entity.Effect{Instrument: "host_timing", DeltaFrac: -0.03},
		Author:     "agent:analyst",
		CreatedAt:  base.Add(3 * time.Minute),
	}
	Expect(s.WriteConclusion(unrelatedConclusion)).To(Succeed())

	return s, goalScope{GoalID: g1.ID}
}

var _ = Describe("goal scoping", func() {
	It("defaults to all without an active goal and supports explicit all", func() {
		s, err := store.Create(GinkgoT().TempDir(), store.Config{
			Build: store.CommandSpec{Command: "true"},
			Test:  store.CommandSpec{Command: "true"},
		})
		Expect(err).NotTo(HaveOccurred())

		scope, err := resolveGoalScope(s, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(scope.All).To(BeTrue())

		now := time.Now().UTC()
		goal := &entity.Goal{
			ID: "G-0001", Status: entity.GoalStatusActive, CreatedAt: &now,
			Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"},
		}
		Expect(s.WriteGoal(goal)).To(Succeed())
		Expect(s.UpdateState(func(st *store.State) error {
			st.CurrentGoalID = goal.ID
			return nil
		})).To(Succeed())

		scope, err = resolveGoalScope(s, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(scope.All).To(BeFalse())
		Expect(scope.GoalID).To(Equal(goal.ID))

		scope, err = resolveGoalScope(s, goalScopeAll)
		Expect(err).NotTo(HaveOccurred())
		Expect(scope.All).To(BeTrue())

		_, err = resolveGoalScope(s, "G-9999")
		Expect(err).To(HaveOccurred())
	})

	It("filters baselines, lessons, and events to the requested goal", func() {
		s := scopedFixtureStore()
		r := newGoalScopeResolver(s, goalScope{GoalID: "G-0001"})

		exps, err := s.ListExperiments()
		Expect(err).NotTo(HaveOccurred())
		exps, err = r.filterExperiments(exps)
		Expect(err).NotTo(HaveOccurred())
		Expect([]string{exps[0].ID, exps[1].ID}).To(Equal([]string{"E-0001", "E-0002"}))

		obs, err := s.ListObservations()
		Expect(err).NotTo(HaveOccurred())
		obs, err = r.filterObservations(obs)
		Expect(err).NotTo(HaveOccurred())
		Expect(obs).To(HaveLen(1))
		Expect(obs[0].ID).To(Equal("O-0001"))

		lessons, err := s.ListLessons()
		Expect(err).NotTo(HaveOccurred())
		lessons, err = r.filterLessons(lessons)
		Expect(err).NotTo(HaveOccurred())
		Expect([]string{lessons[0].ID, lessons[1].ID}).To(Equal([]string{"L-0001", "L-0002"}))

		events, err := s.Events(0)
		Expect(err).NotTo(HaveOccurred())
		events, err = r.filterEvents(events)
		Expect(err).NotTo(HaveOccurred())
		var keys []string
		for _, ev := range events {
			keys = append(keys, ev.Kind+":"+ev.Subject)
		}
		Expect(keys).To(ConsistOf(
			"goal.new:G-0001",
			"hypothesis.add:H-0001",
			"experiment.baseline:E-0001",
			"observation.record:O-0001",
			"lesson.add:L-0001",
			"lesson.add:L-0002",
		))
	})

	It("uses the active goal as the default dashboard scope", func() {
		s := scopedFixtureStore()

		snap, err := captureDashboard(s)
		Expect(err).NotTo(HaveOccurred())
		Expect(snap.ScopeAll).To(BeFalse())
		Expect(snap.ScopeGoalID).To(Equal("G-0002"))
		Expect(snap.Counts).To(HaveKeyWithValue("hypotheses", 1))
		Expect(snap.Tree).To(HaveLen(1))
		Expect(snap.Tree[0].ID).To(Equal("H-0002"))
		Expect(snap.RecentLessons).To(HaveLen(2))

		allSnap, err := captureDashboardScoped(s, goalScope{All: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(allSnap.ScopeAll).To(BeTrue())
		Expect(allSnap.Counts).To(HaveKeyWithValue("hypotheses", 2))
		Expect(allSnap.RecentLessons).To(HaveLen(3))
	})

	It("uses global hypothesis links for scoped system lesson accuracy in dashboard data", func() {
		s, scope := scopedSystemLessonAccuracyFixtureStore()

		snap, err := captureDashboardScoped(s, scope)
		Expect(err).NotTo(HaveOccurred())
		Expect(snap.RecentLessons).To(HaveLen(1))
		Expect(snap.RecentLessons[0].ID).To(Equal("L-0007"))
		Expect(snap.recentLessonAccuracy).NotTo(HaveKey("L-0007"))
	})

	It("uses global hypothesis links for scoped system lesson accuracy in the TUI lesson list", func() {
		s, scope := scopedSystemLessonAccuracyFixtureStore()

		msg := newLessonListView(scope).init(s)().(lessonListLoadedMsg)
		Expect(msg.err).NotTo(HaveOccurred())
		Expect(msg.list).To(HaveLen(1))
		Expect(msg.list[0].ID).To(Equal("L-0007"))
		Expect(msg.accuracy).NotTo(HaveKey("L-0007"))
	})
})
