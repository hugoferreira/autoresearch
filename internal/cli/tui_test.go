package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/stats"
	"github.com/bytter/autoresearch/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func lineContaining(s, needle string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func applyTUICommand(m tuiModel, cmd tea.Cmd) tuiModel {
	GinkgoHelper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	switch msg := msg.(type) {
	case nil:
		return m
	case tea.BatchMsg:
		for _, sub := range msg {
			m = applyTUICommand(m, sub)
		}
		return m
	default:
		updated, next := m.Update(msg)
		nm, ok := updated.(tuiModel)
		Expect(ok).To(BeTrue(), "expected tuiModel back, got %T", updated)
		return applyTUICommand(nm, next)
	}
}

func testExperimentView(e *entity.Experiment, classification, hypothesisStatus string) *experimentReadView {
	if classification == "" {
		classification = experimentClassificationLive
	}
	return &experimentReadView{
		Experiment:       e,
		Classification:   classification,
		HypothesisStatus: hypothesisStatus,
	}
}

func testObservationDetailEntity() *entity.Observation {
	return &entity.Observation{
		ID:         "O-0003",
		Experiment: "E-0007",
		Instrument: "host_timing",
		MeasuredAt: time.Date(2026, 4, 12, 11, 0, 0, 0, time.UTC),
		Value:      1.2,
		Unit:       "s",
		Samples:    1,
		Command:    "make test",
		ExitCode:   0,
		Author:     "agent:observer",
	}
}

func renderObservationDetailForTest(o *entity.Observation, height int) string {
	GinkgoHelper()
	v := newObservationDetailView(o.ID)
	nv, _ := v.update(obsDetailLoadedMsg{o: o}, nil)
	return stripANSI(nv.view(120, height))
}

var _ = Describe("TUI dashboard view", func() {
	It("renders the rich dashboard across primary panels", func() {
		v := newDashboardView(goalScope{All: true})
		nv, _ := v.update(dashLoadedMsg{snap: populatedDashboardSnapshot()}, nil)
		out := stripANSI(nv.view(140, 40))
		expectText(out,
			"Goal:", "decrease qemu_cycles", "size_flash", "5/20 exp",
			"Hypothesis tree", "H-0001", "H-0003",
			"Frontier", "C-0001",
			"In flight", "E-0007",
			"Recent events", "hypothesis.add",
		)
	})

	It("falls back to a single-column layout at narrow widths", func() {
		v := newDashboardView(goalScope{All: true})
		nv, _ := v.update(dashLoadedMsg{snap: populatedDashboardSnapshot()}, nil)
		expectText(stripANSI(nv.view(80, 40)), "Hypothesis tree", "Frontier", "H-0001")
	})

	It("advances elapsed fields on quiet ticks without resetting focus or cursors", func() {
		v := newDashboardView(goalScope{All: true})
		snap := populatedDashboardSnapshot()
		nv, _ := v.update(dashLoadedMsg{snap: snap}, nil)
		d := nv.(*dashboardView)
		d.focus = dashFocusInFlight
		d.cursors[dashFocusInFlight] = 0

		plain := stripANSI(d.view(140, 40))
		expectText(plain, "1.2h/8h", "02:00")

		later := snap.CapturedAt.Add(65 * time.Second)
		nv, _ = d.quietTick(later, nil)
		d = nv.(*dashboardView)
		plain = stripANSI(d.view(140, 40))
		expectText(plain, "1.2h/8h", "03:05")
		Expect(d.cursors[dashFocusInFlight]).To(Equal(0))
		Expect(d.focus).To(Equal(dashFocusInFlight))
	})

	It("uses lesson accuracy arrows only when comparison data exists", func() {
		snap := populatedDashboardSnapshot()
		snap.RecentLessons = []*entity.Lesson{
			{
				ID: "L-0001", Claim: "overshooting lesson", Scope: entity.LessonScopeHypothesis,
				Status: entity.LessonStatusActive, PredictedEffect: &entity.PredictedEffect{MinEffect: 0.10},
			},
			{
				ID: "L-0002", Claim: "undershooting lesson", Scope: entity.LessonScopeHypothesis,
				Status: entity.LessonStatusActive, PredictedEffect: &entity.PredictedEffect{MinEffect: 0.10},
			},
			{
				ID: "L-0003", Claim: "no data lesson", Scope: entity.LessonScopeHypothesis,
				Status: entity.LessonStatusActive, PredictedEffect: &entity.PredictedEffect{MinEffect: 0.10},
			},
		}
		snap.recentLessonAccuracy = map[string]lessonAccuracySummary{
			"L-0001": {Overshoot: 2, Undershoot: 1},
			"L-0002": {Overshoot: 1, Undershoot: 2},
		}

		v := newDashboardView(goalScope{All: true})
		nv, _ := v.update(dashLoadedMsg{snap: snap}, nil)
		out := stripANSI(nv.(*dashboardView).renderLessonsPanel(70, 8))

		Expect(lineContaining(out, "L-0001")).To(ContainSubstring("↓"))
		Expect(lineContaining(out, "L-0002")).To(ContainSubstring("↑"))
		noData := lineContaining(out, "L-0003")
		Expect(noData).NotTo(ContainSubstring("↓"))
		Expect(noData).NotTo(ContainSubstring("↑"))
	})

	It("shows dirty main checkout warnings consistently with the status view", func() {
		snap := populatedDashboardSnapshot()
		snap.MainCheckoutDirty = true
		snap.MainCheckoutDirtyPaths = []string{"bootstrap.sh"}

		dash := newDashboardView(goalScope{All: true})
		nv, _ := dash.update(dashLoadedMsg{snap: snap}, nil)
		expectText(stripANSI(nv.view(140, 40)), "Main checkout dirty:", "bootstrap.sh")

		status := newStatusView(goalScope{All: true})
		nv, _ = status.update(dashLoadedMsg{snap: snap}, nil)
		expectText(stripANSI(nv.view(100, 30)), "Main checkout:", "dirty outside autoresearch-managed files", "bootstrap.sh")
	})

	It("uses a titled border for the tree panel without consuming content height", func() {
		d := newDashboardView(goalScope{All: true})
		d.snap = populatedDashboardSnapshot()

		out := stripANSI(d.renderTreePanel(60, 8))
		lines := strings.Split(out, "\n")
		Expect(len(lines)).To(BeNumerically(">=", 2))
		Expect(lines[0]).To(ContainSubstring("┤ Hypothesis tree ├"))
		Expect(lines[1]).NotTo(ContainSubstring("Hypothesis tree"))
	})

	It("allocates remaining right-column height to recent events", func() {
		d := newDashboardView(goalScope{All: true})
		d.snap = populatedDashboardSnapshot()

		frontierH, inFlightH, eventsH := d.rightColumnHeights(30)
		Expect(eventsH).To(BeNumerically(">", frontierH))
		Expect(eventsH).To(BeNumerically(">", inFlightH))
	})

	It("loads additional recent events when the events cursor reaches the older edge", func() {
		s := newStoreWithBuiltins()
		base := time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC)
		for i := 0; i < 70; i++ {
			Expect(s.AppendEvent(store.Event{
				Ts:      base.Add(time.Duration(i) * time.Second),
				Kind:    "observation.record",
				Subject: fmt.Sprintf("O-%03d", i),
			})).To(Succeed())
		}

		d := newDashboardView(goalScope{All: true})
		nv, _ := d.update(dashLoadedMsg{snap: baseSnapshot()}, s)
		d = nv.(*dashboardView)
		d.focus = dashFocusEvents

		msg := loadDashboardEventsCmd(s, goalScope{All: true}, 0, dashboardRecentEventsPageSize, true)()
		nv, _ = d.update(msg, s)
		d = nv.(*dashboardView)
		Expect(d.events).To(HaveLen(dashboardRecentEventsPageSize))
		Expect(d.events[0].Subject).To(Equal("O-069"))
		Expect(d.events[len(d.events)-1].Subject).To(Equal("O-006"))

		d.cursors[dashFocusEvents] = len(d.events) - 1
		cmd := d.maybeLoadMoreEvents(s)
		Expect(cmd).NotTo(BeNil())
		nv, _ = d.update(cmd(), s)
		d = nv.(*dashboardView)
		Expect(d.events).To(HaveLen(70))
		Expect(d.events[len(d.events)-1].Subject).To(Equal("O-000"))
		Expect(d.eventsAllLoaded).To(BeTrue())
	})
})

var _ = Describe("TUI entity views", func() {
	It("renders hypothesis list and detail views", func() {
		list := newHypothesisListView(goalScope{All: true})
		hs := []*entity.Hypothesis{
			{ID: "H-0001", Claim: "one", Status: entity.StatusOpen, Author: "human"},
			{ID: "H-0002", Claim: "two", Status: entity.StatusSupported, Author: "agent"},
		}
		nv, _ := list.update(hypListLoadedMsg{list: hs}, nil)
		expectText(stripANSI(nv.view(100, 20)), "2 hypotheses", "H-0001", "H-0002", "supported")

		detail := newHypothesisDetailView("H-0007")
		h := &entity.Hypothesis{
			ID: "H-0007", Parent: "H-0001", Claim: "tiled kernel", Status: entity.StatusOpen,
			Author: "agent:gen",
			Predicts: entity.Predicts{
				Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease", MinEffect: 0.1,
			},
			KillIf: []string{"size_flash grows >10%"},
		}
		nv, _ = detail.update(hypDetailLoadedMsg{h: h}, nil)
		expectText(stripANSI(nv.view(100, 30)), "H-0007", "tiled kernel", "Predicts:", "decrease qemu_cycles", "Kill if:", "size_flash grows")
	})

	It("opens linked experiment, conclusion, and observation details from a hypothesis detail", func() {
		v := newHypothesisDetailView("H-0001")
		h := &entity.Hypothesis{
			ID: "H-0001", Claim: "unroll the FIR tap loop", Status: entity.StatusOpen,
			Predicts: entity.Predicts{Instrument: "host_timing", Target: "dsp_fir", Direction: "decrease"},
		}
		exps := []*entity.Experiment{{
			ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpImplemented, Instruments: []string{"host_timing"},
		}}
		concls := []*entity.Conclusion{{
			ID: "C-0001", Hypothesis: "H-0001", Verdict: entity.VerdictSupported,
			Effect: entity.Effect{DeltaFrac: -0.12, PValue: 0.01},
		}}
		obs := []*entity.Observation{{
			ID: "O-0001", Experiment: "E-0001", Instrument: "host_timing", Value: 1.2, Unit: "s", Samples: 5,
		}}
		nv, _ := v.update(hypDetailLoadedMsg{
			h:      h,
			exps:   []*experimentReadView{testExperimentView(exps[0], experimentClassificationLive, "")},
			concls: concls,
			obs:    obs,
		}, nil)
		hv := nv.(*hypothesisDetailView)

		_, cmd := hv.update(tea.KeyMsg{Type: tea.KeyEnter}, nil)
		Expect(cmd().(tuiPushMsg).v.kind()).To(Equal(kindExperimentDetail))

		nv, _ = hv.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, nil)
		hv = nv.(*hypothesisDetailView)
		_, cmd = hv.update(tea.KeyMsg{Type: tea.KeyEnter}, nil)
		Expect(cmd().(tuiPushMsg).v.kind()).To(Equal(kindConclusionDetail))

		nv, _ = hv.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, nil)
		hv = nv.(*hypothesisDetailView)
		_, cmd = hv.update(tea.KeyMsg{Type: tea.KeyEnter}, nil)
		Expect(cmd().(tuiPushMsg).v.kind()).To(Equal(kindObservationDetail))
	})

	It("renders experiment list and detail views with classification and stats", func() {
		list := newExperimentListView(goalScope{All: true})
		es := []*experimentReadView{
			testExperimentView(&entity.Experiment{ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpImplemented, Instruments: []string{"qemu_cycles"}}, experimentClassificationLive, ""),
		}
		nv, _ := list.update(expListLoadedMsg{list: es}, nil)
		expectText(stripANSI(nv.view(100, 20)), "1 experiments", "E-0001", "implemented", "qemu_cycles")

		deadList := newExperimentListView(goalScope{All: true})
		nv, _ = deadList.update(expListLoadedMsg{list: []*experimentReadView{
			testExperimentView(&entity.Experiment{ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpMeasured, Instruments: []string{"qemu_cycles"}}, experimentClassificationDead, entity.StatusSupported),
		}}, nil)
		expectText(stripANSI(nv.view(100, 20)), "E-0001", "measured", "[dead]")

		detail := newExperimentDetailView("E-0007")
		e := &entity.Experiment{
			ID: "E-0007", Hypothesis: "H-0002", Status: entity.ExpMeasured,
			Instruments: []string{"qemu_cycles"}, Author: "agent:impl",
			Baseline: entity.Baseline{Ref: "HEAD", SHA: "abc123def456789"},
		}
		pass := true
		ciLow, ciHigh := 730000.0, 770000.0
		obs := []*entity.Observation{
			{ID: "O-0001", Experiment: "E-0007", Instrument: "qemu_cycles", Value: 750000, Unit: "cycles", Samples: 10, CILow: &ciLow, CIHigh: &ciHigh, Pass: &pass},
		}
		nv, _ = detail.update(expDetailLoadedMsg{e: testExperimentView(e, experimentClassificationLive, ""), obs: obs}, nil)
		expectText(stripANSI(nv.view(120, 40)), "E-0007", "measured", "abc123def456", "O-0001", "qemu_cycles=750000", "pass")

		nv, _ = detail.update(expDetailLoadedMsg{
			e: testExperimentView(&entity.Experiment{
				ID: "E-0007", Hypothesis: "H-0002", Status: entity.ExpMeasured,
				Instruments: []string{"qemu_cycles"}, Author: "agent:impl",
			}, experimentClassificationDead, entity.StatusSupported),
		}, nil)
		expectText(stripANSI(nv.view(120, 20)), "classification", "dead (hypothesis=supported)", "[dead]")

		summ := map[string]stats.Summary{
			"qemu_cycles": {N: 5, Mean: 1000, StdDev: 7.07, Min: 990, Max: 1010, CILow: 993, CIHigh: 1007, CIMethod: "bca"},
		}
		nv, _ = detail.update(expDetailLoadedMsg{
			e:    testExperimentView(&entity.Experiment{ID: "E-0007", Hypothesis: "H-0002", Status: entity.ExpMeasured, Instruments: []string{"qemu_cycles"}, Author: "agent:impl"}, experimentClassificationLive, ""),
			obs:  []*entity.Observation{{ID: "O-0001", Experiment: "E-0007", Instrument: "qemu_cycles", Value: 1000, Unit: "cycles", Samples: 5, PerSample: []float64{1000, 1010, 990, 1005, 995}}},
			summ: summ,
		}, nil)
		expectText(stripANSI(nv.view(130, 40)), "E-0007", "Summary (per instrument):", "qemu_cycles", "1000", "bca")
	})

	It("renders observation detail metadata, artifacts, evidence failures, and aux data", func() {
		pass := true
		ciLow, ciHigh := 1.1, 1.3
		o := testObservationDetailEntity()
		o.Samples = 5
		o.PerSample = []float64{1.1, 1.2, 1.3}
		o.CILow = &ciLow
		o.CIHigh = &ciHigh
		o.CIMethod = "bca"
		o.Pass = &pass
		o.Artifacts = []entity.Artifact{{
			Name: "primary", SHA: "abcdef1234567890", Path: "artifacts/ab/abcdef1234567890", Bytes: 2048,
		}}
		o.EvidenceFailures = []entity.EvidenceFailure{{
			Name:     testEvidenceName,
			ExitCode: 7,
			Error:    "profile tool crashed",
		}}
		o.Worktree = "/tmp/wt"
		o.BaselineSHA = "fedcba9876543210"
		o.Aux = map[string]any{"stdev": 0.04, "warm": true}
		out := renderObservationDetailForTest(o, 40)
		expectText(out,
			"O-0003", "host_timing=1.2 s", "experiment=E-0007", "Artifacts (1):",
			"Evidence failures:", testEvidenceName+" (exit 7): profile tool crashed",
			"make test", "stdev", "warm",
		)

		o = testObservationDetailEntity()
		o.EvidenceFailures = []entity.EvidenceFailure{{Name: testEvidenceName, Error: testEvidenceSpawnTraceErr}}
		expectText(renderObservationDetailForTest(o, 20), "Evidence failures:", testEvidenceName+": "+testEvidenceSpawnTraceErr)
	})

	It("renders conclusion list and detail views", func() {
		list := newConclusionListView(goalScope{All: true})
		cs := []*entity.Conclusion{
			{ID: "C-0001", Hypothesis: "H-0001", Verdict: entity.VerdictSupported,
				Effect: entity.Effect{DeltaFrac: -0.25, PValue: 0.01}},
			{ID: "C-0002", Hypothesis: "H-0002", Verdict: entity.VerdictInconclusive,
				Strict: entity.Strict{RequestedFrom: "supported", Reasons: []string{"p>0.05"}}},
		}
		nv, _ := list.update(concListLoadedMsg{list: cs}, nil)
		expectText(stripANSI(nv.view(120, 20)), "2 conclusions", "C-0001", "supported", "C-0002", "↓from supported")

		detail := newConclusionDetailView("C-0042")
		c := &entity.Conclusion{
			ID: "C-0042", Hypothesis: "H-0007", Verdict: entity.VerdictInconclusive,
			Author: "agent:analyst", CandidateExp: "E-0007",
			Strict:   entity.Strict{RequestedFrom: "supported", Reasons: []string{"size_flash exceeded"}},
			Effect:   entity.Effect{Instrument: "qemu_cycles", DeltaFrac: -0.1, PValue: 0.02, NCandidate: 10, NBaseline: 10, CIMethod: "bca"},
			StatTest: "welch",
		}
		nv, _ = detail.update(concDetailLoadedMsg{c: c}, nil)
		expectText(stripANSI(nv.view(120, 30)), "C-0042", "inconclusive", "downgraded from supported", "size_flash exceeded", "Effect:", "welch", "delta_frac")
	})

	It("renders event list and pretty-printed event payload details", func() {
		v := newEventListView(goalScope{All: true})
		es := []store.Event{
			{Ts: time.Now().UTC(), Kind: "experiment.implement", Actor: "agent:impl", Subject: "E-0007",
				Data: []byte(`{"worktree":"/tmp/wt","branch":"autoresearch/E-0007","samples":5,"pass":true}`)},
		}
		nv, _ := v.update(eventListLoadedMsg{events: es, replace: true}, nil)
		expectText(stripANSI(nv.view(120, 20)), "1 events", "experiment.implement", "E-0007")

		d := newEventDetailView(es[0])
		dout := stripANSI(d.view(120, 25))
		expectText(dout, "experiment.implement", "Payload:", "worktree", "autoresearch/E-0007")
		payloadStart := strings.Index(dout, "Payload:")
		Expect(payloadStart).To(BeNumerically(">=", 0))
		payload := dout[payloadStart:]
		Expect(strings.Count(payload, "\n")).To(BeNumerically(">=", 3))
		Expect(payload).To(ContainSubstring(`"worktree": "/tmp/wt"`))
	})

	It("renders lesson detail markdown and lesson list accuracy arrows", func() {
		detail := newLessonDetailView("L-0007")
		l := &entity.Lesson{
			ID:        "L-0007",
			Claim:     "keep the 8x-unrolled FIR tap loop for this target",
			Scope:     entity.LessonScopeHypothesis,
			Status:    entity.LessonStatusActive,
			Subjects:  []string{"H-0002", "C-0001"},
			Tags:      []string{"fir", "unroll"},
			Author:    "agent:analyst",
			CreatedAt: time.Now().UTC(),
			Body:      "# Lesson\n\n**Why**: this keeps `host_timing` low.\n\n- cache stays warm\n- _single-accumulator_ variant regresses\n",
		}
		nv, _ := detail.update(lessonDetailLoadedMsg{l: l}, nil)
		out := stripANSI(nv.view(100, 24))
		expectText(out, "L-0007", "keep the 8x-unrolled FIR tap loop", "Why", "host_timing", "cache stays warm", "single-accumulator")
		expectNoText(out, "**Why**", "_single-accumulator_")

		list := newLessonListView(goalScope{All: true})
		lessons := []*entity.Lesson{
			{
				ID: "L-0001", Claim: "validated lesson without follow-up", Scope: entity.LessonScopeHypothesis,
				Status: entity.LessonStatusActive, PredictedEffect: &entity.PredictedEffect{MinEffect: 0.10},
				Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceReviewedDecisive},
			},
			{
				ID: "L-0002", Claim: "lesson that is undershooting", Scope: entity.LessonScopeHypothesis,
				Status: entity.LessonStatusActive, PredictedEffect: &entity.PredictedEffect{MinEffect: 0.10},
				Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceReviewedDecisive},
			},
		}
		nv, _ = list.update(lessonListLoadedMsg{
			list: lessons,
			accuracy: map[string]lessonAccuracySummary{
				"L-0002": {Overshoot: 1, Undershoot: 2},
			},
		}, nil)
		out = stripANSI(nv.view(120, 20))
		noData := lineContaining(out, "L-0001")
		Expect(noData).NotTo(ContainSubstring("↓"))
		Expect(noData).NotTo(ContainSubstring("↑"))
		Expect(lineContaining(out, "L-0002")).To(ContainSubstring("↑≥10%"))
	})
})

var _ = Describe("TUI read-only aggregate views", func() {
	It("pretty-prints JSON payloads with stable indentation and fallbacks", func() {
		raw := []byte(`{"name":"fir","count":3,"nested":{"k":true,"v":null,"n":-2.5e3},"arr":[1,"two",false]}`)
		plain := stripANSI(prettyJSON(raw, "  "))
		expectText(plain,
			"  {",
			`    "name": "fir"`,
			`    "count": 3`,
			`    "nested": {`,
			`      "k": true`,
			`      "v": null`,
			`    "arr": [`,
			"  }",
		)
		Expect(prettyJSON([]byte("not json"), "  ")).To(Equal("  not json"))
		Expect(stripANSI(prettyJSON(nil, "  "))).To(ContainSubstring("(empty)"))
	})

	It("renders tree and frontier views, using hypothesis status for frontier markers", func() {
		tree := newTreeView(goalScope{All: true})
		nodes := []*treeNode{
			{ID: "H-0001", Claim: "root hyp", Status: entity.StatusOpen},
			{ID: "H-0002", Claim: "another", Status: entity.StatusSupported},
		}
		nv, _ := tree.update(treeLoadedMsg{roots: nodes}, nil)
		expectText(stripANSI(nv.view(100, 20)), "H-0001", "root hyp", "H-0002", "another")

		frontier := newFrontierView(goalScope{GoalID: "G-0001"})
		g := &entity.Goal{Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"}}
		nv, _ = frontier.update(frontierLoadedMsg{
			goal:       g,
			rows:       []frontierRow{{Conclusion: "C-0001", Hypothesis: "H-0001", Value: 750067, DeltaFrac: -0.25}},
			assessment: frontierGoalAssessment{Mode: "open_ended", RecommendedAction: "continue"},
			stalled:    2,
		}, nil)
		expectText(stripANSI(nv.view(120, 20)), "decrease qemu_cycles", "stalled_for=2", "open-ended -> continue_until_stall", "C-0001", "H-0001", "750067")

		nv, _ = frontier.update(frontierLoadedMsg{
			goal: g,
			rows: []frontierRow{{
				Conclusion: "C-0001", Hypothesis: "H-0001", Value: 750067, DeltaFrac: -0.25,
				Classification: experimentClassificationDead, HypothesisStatus: entity.StatusSupported,
			}},
			assessment: frontierGoalAssessment{Mode: "open_ended", RecommendedAction: "continue"},
		}, nil)
		out := stripANSI(nv.view(120, 20))
		expectText(out, "[supported]")
		expectNoText(out, "[dead]")
	})

	It("renders goal list/detail and status views", func() {
		detail := newGoalDetailView("G-0001")
		flash := 65536.0
		g := &entity.Goal{
			ID:          "G-0001",
			Status:      entity.GoalStatusActive,
			Objective:   entity.Objective{Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease"},
			Completion:  &entity.Completion{Threshold: 0.2, OnThreshold: entity.GoalOnThresholdAskHuman},
			Constraints: []entity.Constraint{{Instrument: "size_flash", Max: &flash}, {Instrument: "host_test", Require: "pass"}},
		}
		nv, _ := detail.update(goalDetailLoadedMsg{g: g}, nil)
		expectText(stripANSI(nv.view(100, 20)), "G-0001", "Objective:", "decrease qemu_cycles", "Completion:", "threshold=0.2 -> ask_human", "size_flash", "host_test", "require=pass")

		list := newGoalListView()
		goals := []*entity.Goal{
			{ID: "G-0001", Status: entity.GoalStatusConcluded, Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"}},
			{ID: "G-0002", Status: entity.GoalStatusActive, Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"}},
		}
		nv, _ = list.update(goalListLoadedMsg{all: goals, current: "G-0002"}, nil)
		expectText(stripANSI(nv.view(100, 20)), "2 goals", "G-0001", "G-0002", "qemu_cycles")

		snap := populatedDashboardSnapshot()
		snap.ScopeAll = true
		status := newStatusView(goalScope{All: true})
		nv, _ = status.update(dashLoadedMsg{snap: snap}, nil)
		out := stripANSI(nv.view(100, 30))
		expectText(out, "Scope:", "all", "State:", "active", "Mode:", "strict", "Main checkout:", "clean", "Budget:", "5/20 experiments", "Counts:")
		expectNoText(out, "stalled")

		snap = populatedDashboardSnapshot()
		snap.ScopeGoalID = "G-0002"
		status = newStatusView(goalScope{GoalID: "G-0002"})
		nv, _ = status.update(dashLoadedMsg{snap: snap}, nil)
		expectText(stripANSI(nv.view(100, 30)), "stalled 2/5")
	})

	It("advances status elapsed budget on quiet ticks", func() {
		snap := populatedDashboardSnapshot()
		v := newStatusView(goalScope{All: true})
		nv, _ := v.update(dashLoadedMsg{snap: snap}, nil)
		status := nv.(*statusView)
		expectText(stripANSI(status.view(100, 30)), "1.2h/8h elapsed")

		later := snap.CapturedAt.Add(30 * time.Minute)
		nv, _ = status.quietTick(later, nil)
		status = nv.(*statusView)
		expectText(stripANSI(status.view(100, 30)), "1.7h/8h elapsed")
	})

	It("renders artifacts, instruments, and simple report views", func() {
		artifacts := newArtifactListView(goalScope{All: true})
		rows := []artifactRow{
			{Observation: "O-0001", Instrument: "qemu_cycles", Name: "primary", SHA: "abc123def45678", Bytes: 2048, Path: "artifacts/ab/abc123def45678"},
			{Observation: "O-0002", Instrument: "host_test", Name: "primary", SHA: "fed987abc12345", Bytes: 512000, Path: "artifacts/fe/fed987abc12345"},
		}
		nv, _ := artifacts.update(artifactListLoadedMsg{rows: rows}, nil)
		expectText(stripANSI(nv.view(120, 20)), "2 artifacts", "O-0001", "qemu_cycles", "abc123def456", "500.0K", "host_test")

		row := artifactRow{Observation: "O-0001", Instrument: "qemu_cycles", Name: "primary", SHA: "abcdef1234567890", Bytes: 4096, Path: "artifacts/ab/abcdef1234567890"}
		artifact := newArtifactView(row)
		nv, _ = artifact.update(artifactLoadedMsg{
			sha:   "abcdef1234567890",
			abs:   "/fake/abs",
			rel:   "artifacts/ab/abcdef1234567890",
			lines: []string{"line 1", "line 2", "line 3"},
			total: 3,
			bytes: 4096,
		}, nil)
		expectText(stripANSI(nv.view(100, 20)), "abcdef1234567890", "artifacts/ab/abcdef", "lines=3", "mode=head")

		diff := newArtifactDiffView("abcdef1234567890", "a/path", "1234567890abcdef", "b/path", []string{"@@ -1,3 +1,3 @@", "-old", "+new", " same"})
		expectText(stripANSI(diff.view(100, 20)), "a/path", "b/path", "-old", "+new")

		instruments := newInstrumentListView()
		by := map[string]store.Instrument{
			"qemu_cycles": {Cmd: []string{"bash", "-c", "./run"}, Parser: "builtin:scalar", Pattern: "cycles:(\\d+)", Unit: "cycles", MinSamples: 5},
			"host_test":   {Parser: "builtin:passfail"},
		}
		nv, _ = instruments.update(instrumentListLoadedMsg{by: by}, nil)
		expectText(stripANSI(nv.view(120, 20)), "2 instruments", "qemu_cycles", "host_test", "builtin:scalar", "pattern=/cycles", "min_samples=5")

		report := newReportView("H-0007")
		nv, _ = report.update(reportLoadedMsg{md: "# H-0007 — my claim\n\n**Status**: open\n\n## Prediction\n\n- stuff"}, nil)
		expectText(stripANSI(nv.view(100, 20)), "H-0007", "my claim", "Prediction")
	})
})

var _ = Describe("TUI model navigation", func() {
	It("renders model chrome around the active view", func() {
		m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
		m.width, m.height = 120, 30
		top := m.top()
		nv, _ := top.update(dashLoadedMsg{snap: populatedDashboardSnapshot()}, nil)
		m.setTop(nv)
		expectText(stripANSI(m.View()), "Dashboard", "help", "quit")
	})

	It("canonicalizes top-level jumps without duplicating existing views", func() {
		m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
		Expect(m.stack).To(HaveLen(1))

		m.jumpTo(newHypothesisListView(goalScope{All: true}), nil)
		Expect(m.stack).To(HaveLen(2))
		Expect(m.top().kind()).To(Equal(kindHypothesisList))

		m.jumpTo(newHypothesisListView(goalScope{All: true}), nil)
		Expect(m.stack).To(HaveLen(2))

		m.push(newHypothesisDetailView("H-0001"))
		Expect(m.stack).To(HaveLen(3))

		m.jumpTo(newHypothesisListView(goalScope{All: true}), nil)
		Expect(m.stack).To(HaveLen(2))
		Expect(m.top().kind()).To(Equal(kindHypothesisList))

		m.jumpTo(newExperimentListView(goalScope{All: true}), nil)
		Expect(m.stack).To(HaveLen(2))
		Expect(m.top().kind()).To(Equal(kindExperimentList))

		m.jumpTo(newHypothesisListViewForReport(goalScope{All: true}), nil)
		Expect(m.stack).To(HaveLen(2))
		Expect(m.top().kind()).To(Equal(kindHypothesisReport))
	})

	It("preserves a loaded dashboard instance across top-level jumps and pops", func() {
		m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
		dash := newDashboardView(goalScope{All: true})
		nv, _ := dash.update(dashLoadedMsg{snap: populatedDashboardSnapshot()}, nil)
		dash = nv.(*dashboardView)
		dash.focus = dashFocusEvents
		dash.cursors[dashFocusEvents] = 1
		m.stack = []tuiView{dash}

		m.jumpTo(newHypothesisListView(goalScope{All: true}), nil)
		got, ok := m.stack[0].(*dashboardView)
		Expect(ok).To(BeTrue())
		Expect(got).To(BeIdenticalTo(dash))

		m.pop()
		out := stripANSI(m.top().view(140, 40))
		expectNoText(out, "loading…")
		Expect(got.cursors[dashFocusEvents]).To(Equal(1))
	})

	It("reloads the hidden dashboard when popping back to it", func() {
		s := newStoreWithBuiltins()
		m := newTuiModel(s, goalScope{All: true}, 2*time.Second)
		m.stack = []tuiView{
			newDashboardView(goalScope{All: true}),
			newHypothesisListView(goalScope{All: true}),
		}

		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = updated.(tuiModel)
		Expect(m.top().kind()).To(Equal(kindDashboard))
		Expect(cmd).NotTo(BeNil())

		m = applyTUICommand(m, cmd)
		expectNoText(stripANSI(m.top().view(140, 40)), "loading…")
	})

	It("keeps event-list keys local instead of treating them as root shortcuts", func() {
		v := newEventListView(goalScope{All: true})
		es := []store.Event{
			{Ts: time.Now().UTC(), Kind: "init", Subject: "A"},
			{Ts: time.Now().UTC(), Kind: "hypothesis.add", Subject: "B"},
		}
		nv, _ := v.update(eventListLoadedMsg{events: es, replace: true}, nil)
		ev := nv.(*eventListView)
		ev.cursor = 0
		ev.follow = false

		m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
		m.stack = []tuiView{newDashboardView(goalScope{All: true}), ev}

		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
		m = model.(tuiModel)
		Expect(m.top().kind()).To(Equal(kindEventList))
		ev = m.top().(*eventListView)
		Expect(ev.cursor).To(Equal(len(ev.filtered) - 1))
		Expect(ev.follow).To(BeTrue())

		model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'W'}})
		m = model.(tuiModel)
		Expect(m.top().(*eventListView).follow).To(BeFalse())
	})

	It("uses O as the root shortcut for the goal view", func() {
		m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'O'}})
		m = model.(tuiModel)
		Expect(m.top().kind()).To(Equal(kindGoalList))
	})

	It("forwards loaded messages to the dashboard overlay", func() {
		d := newDashboardView(goalScope{All: true})
		nv, _ := d.update(dashLoadedMsg{snap: populatedDashboardSnapshot()}, nil)
		d = nv.(*dashboardView)
		d.focus = dashFocusTree
		_ = d.openSelected(nil)
		Expect(d.rightOverlay).NotTo(BeNil())

		h := &entity.Hypothesis{
			ID: "H-0001", Claim: "unrolling dsp_fir", Status: entity.StatusSupported,
			Predicts: entity.Predicts{Instrument: "qemu_cycles", Direction: "decrease"},
		}
		nv, _ = d.update(hypDetailLoadedMsg{h: h}, nil)
		d = nv.(*dashboardView)
		expectText(stripANSI(d.rightOverlay.view(60, 20)), "H-0001", "unrolling dsp_fir")
	})
})

var _ = Describe("TUI report rendering", func() {
	It("renders markdown instead of leaking raw inline sigils", func() {
		v := newReportView("H-0007")
		md := "# H-0007 — my claim\n\n**Status**: open  \n\n## Prediction\n\n- **Instrument**: `qemu_cycles`\n- _italic note_\n"
		nv, _ := v.update(reportLoadedMsg{md: md}, nil)
		rv := nv.(*reportView)
		rendered := rv.ensureRendered(100)
		plain := stripANSI(rendered)
		expectText(plain, "H-0007", "my claim", "Status", "Prediction", "Instrument", "qemu_cycles", "italic note")
		expectNoText(plain, "**Status**", "_italic note_")
		Expect(rendered).NotTo(Equal(md))
	})

	It("caches rendered markdown per width and invalidates on resize", func() {
		v := newReportView("H-0007")
		md := "# Heading\n\nlorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua ut enim ad minim veniam\n"
		nv, _ := v.update(reportLoadedMsg{md: md}, nil)
		rv := nv.(*reportView)

		at80 := rv.ensureRendered(80)
		Expect(rv.renderedWidth).To(Equal(80))
		Expect(rv.ensureRendered(80)).To(Equal(at80))

		at120 := rv.ensureRendered(120)
		Expect(rv.renderedWidth).To(Equal(120))
		Expect(at120).NotTo(Equal(at80))
	})
})

var _ = Describe("TUI visual dumps", func() {
	It("dumps a realistic dashboard snapshot", func() {
		if testing.Short() {
			Skip("visual dump skipped in short mode")
		}
		snap := populatedDashboardSnapshot()
		now := snap.CapturedAt
		for i := 0; i < 10; i++ {
			snap.RecentEvents = append(snap.RecentEvents, store.Event{
				Ts:   now.Add(time.Duration(i) * time.Second),
				Kind: "observation.record", Actor: "agent:observer",
				Subject: "O-00" + string(rune('0'+i)),
			})
		}
		snap.Paused = true
		snap.PauseReason = "10/10 experiment budget reached"

		m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
		m.width, m.height = 130, 40
		m.chrome = chromeLoadedMsg{
			paused: true, pauseReason: "10/10 experiment budget reached",
			mode:   "strict",
			counts: map[string]int{"hypotheses": 10, "experiments": 4, "observations": 37, "conclusions": 9},
		}
		top := m.top()
		nv, _ := top.update(dashLoadedMsg{snap: snap}, nil)
		m.setTop(nv)
		GinkgoWriter.Printf("\n%s\n", m.View())
	})

	It("dumps a styled markdown report", func() {
		if testing.Short() {
			Skip("visual dump skipped in short mode")
		}
		v := newReportView("H-0007")
		md := "# H-0007 — Unrolling dsp_fir inner loop by 8x is a clean multiple\n\n" +
			"**Status**: supported  \n" +
			"**Author**: agent:generator  \n\n" +
			"## Prediction\n\n" +
			"- **Instrument**: `qemu_cycles`\n" +
			"- **Target**: `dsp_fir`\n" +
			"- **Direction**: decrease\n" +
			"- **Minimum effect**: 0.1000 (fractional)\n\n" +
			"## Experiments\n\n" +
			"### E-0007 — measured (qemu tier)\n\n" +
			"- **Baseline**: `HEAD` at `abc123def456`\n" +
			"- **Instruments**: qemu_cycles, host_test\n\n" +
			"## Conclusions\n\n" +
			"### C-0001 — supported\n\n" +
			"- **Effect on `qemu_cycles`**: -0.2512 (fractional)  95% CI [-0.2801, -0.2223]\n" +
			"- **p-value**: 0.0012 (welch)\n"
		nv, _ := v.update(reportLoadedMsg{md: md}, nil)
		GinkgoWriter.Printf("\n%s\n", nv.view(100, 40))
	})

	It("dumps a colorized event payload", func() {
		if testing.Short() {
			Skip("visual dump skipped in short mode")
		}
		ev := store.Event{
			Ts:      time.Date(2026, 4, 12, 14, 30, 0, 0, time.UTC),
			Kind:    "conclusion.critic_downgrade",
			Actor:   "agent:critic",
			Subject: "C-0042",
			Data:    []byte(`{"from":"supported","to":"inconclusive","hypothesis":"H-0007","reasons":["size_flash exceeded 65536","n_candidate=3 below min_samples=5"],"n_candidate":3,"p_value":0.021,"strict":true,"reviewed_by":null}`),
		}
		d := newEventDetailView(ev)
		GinkgoWriter.Printf("\n%s\n", d.view(120, 30))
	})

	It("dumps experiment detail with observations and summary stats", func() {
		if testing.Short() {
			Skip("visual dump skipped in short mode")
		}
		v := newExperimentDetailView("E-0007")
		e := &entity.Experiment{
			ID: "E-0007", Hypothesis: "H-0002", Status: entity.ExpMeasured,
			Instruments: []string{"qemu_cycles", "host_test"}, Author: "agent:impl",
			Worktree: "/Users/bytter/Library/Caches/autoresearch/fir-ab12/worktrees/E-0007",
			Branch:   "autoresearch/E-0007",
			Baseline: entity.Baseline{Ref: "HEAD", SHA: "abc123def456789abcdef"},
			Budget:   entity.Budget{WallTimeS: 3600, MaxSamples: 20},
		}
		ciLow, ciHigh := 992.5, 1007.5
		pass := true
		obs := []*entity.Observation{
			{ID: "O-0001", Experiment: "E-0007", Instrument: "qemu_cycles", Value: 1000.42, Unit: "cycles", Samples: 5, PerSample: []float64{1000, 1010, 990, 1005, 995}, CILow: &ciLow, CIHigh: &ciHigh, Command: "./bench --runs 5"},
			{ID: "O-0002", Experiment: "E-0007", Instrument: "host_test", Value: 1, Unit: "", Samples: 1, Pass: &pass},
		}
		summ := map[string]stats.Summary{
			"qemu_cycles": {N: 5, Mean: 1000.42, StdDev: 7.070, Min: 990, Max: 1010, CILow: 992.5, CIHigh: 1007.5, CIMethod: "bca"},
			"host_test":   {N: 1, Mean: 1, StdDev: 0, Min: 1, Max: 1, CILow: 1, CIHigh: 1, CIMethod: "bca"},
		}
		nv, _ := v.update(expDetailLoadedMsg{e: testExperimentView(e, experimentClassificationLive, ""), obs: obs, summ: summ}, nil)
		GinkgoWriter.Printf("\n%s\n", nv.view(130, 50))
	})
})
