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
)

// Smoke tests for TUI views. We skip the real Bubble Tea program loop and
// exercise each view by feeding it the *Loaded message it would normally
// receive from its store-read cmd, then calling View(). This verifies the
// render pipeline without needing a real .research/ on disk.

func tuiRichSnapshot() *dashboardSnapshot {
	now := time.Date(2026, 4, 11, 18, 42, 0, 0, time.UTC)
	flash := 65536.0
	impAt := now.Add(-2 * time.Minute)
	snap := &dashboardSnapshot{
		Project:                "/tmp/fir",
		Mode:                   "strict",
		MainCheckoutDirtyPaths: []string{},
		Goal: &entity.Goal{
			Objective: entity.Objective{
				Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease",
			},
			Completion: &entity.Completion{Threshold: 0.2, OnThreshold: entity.GoalOnThresholdAskHuman},
			Constraints: []entity.Constraint{
				{Instrument: "size_flash", Max: &flash},
				{Instrument: "host_test", Require: "pass"},
			},
		},
		Counts: map[string]int{"hypotheses": 3, "experiments": 5, "observations": 12, "conclusions": 2},
		Tree: []*treeNode{
			{ID: "H-0001", Claim: "unrolling dsp_fir", Status: entity.StatusSupported, Author: "human"},
			{ID: "H-0002", Claim: "fixed-point rewrite", Status: entity.StatusOpen, Author: "agent:gen",
				Children: []*treeNode{
					{ID: "H-0003", Claim: "sub: Q15 only", Status: entity.StatusInconclusive, Author: "agent:gen"},
				}},
		},
		Frontier:   []frontierRow{{Conclusion: "C-0001", Hypothesis: "H-0001", Value: 750067, DeltaFrac: -0.25}},
		StalledFor: 2,
		InFlight: []dashboardInFlight{{
			ID: "E-0007", Hypothesis: "H-0002", Status: entity.ExpMeasured,
			Instruments: []string{"qemu_cycles", "host_test"}, ImplementedAt: &impAt, ElapsedS: 120,
		}},
		RecentEvents: []store.Event{
			{Ts: now.Add(time.Second), Kind: "experiment.design", Actor: "agent:des", Subject: "E-0007"},
			{Ts: now, Kind: "hypothesis.add", Actor: "agent:gen", Subject: "H-0003"},
		},
		CapturedAt: now,
	}
	snap.Budgets.Limits.MaxExperiments = 20
	snap.Budgets.Limits.MaxWallTimeH = 8
	snap.Budgets.Limits.FrontierStallK = 5
	snap.Budgets.Usage.Experiments = 5
	snap.Budgets.Usage.ElapsedH = 1.2
	return snap
}

func lineContaining(s, needle string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func TestTUI_DashboardRenders(t *testing.T) {
	v := newDashboardView(goalScope{All: true})
	nv, _ := v.update(dashLoadedMsg{snap: tuiRichSnapshot()}, nil)
	out := nv.view(140, 40)
	for _, want := range []string{
		"Goal:", "decrease qemu_cycles", "size_flash", "5/20 exp",
		"Hypothesis tree", "H-0001", "H-0003",
		"Frontier", "C-0001",
		"In flight", "E-0007",
		"Recent events", "hypothesis.add",
	} {
		if !strings.Contains(stripANSI(out), want) {
			t.Errorf("dashboard missing %q:\n%s", want, stripANSI(out))
		}
	}
}

func TestTUI_DashboardNarrowFallback(t *testing.T) {
	v := newDashboardView(goalScope{All: true})
	nv, _ := v.update(dashLoadedMsg{snap: tuiRichSnapshot()}, nil)
	out := stripANSI(nv.view(80, 40))
	// Narrow width still renders the key sections in single-column layout.
	for _, want := range []string{"Hypothesis tree", "Frontier", "H-0001"} {
		if !strings.Contains(out, want) {
			t.Errorf("narrow dashboard missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_DashboardLessonPanelUsesAccuracyArrows(t *testing.T) {
	snap := tuiRichSnapshot()
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
	d := nv.(*dashboardView)
	out := stripANSI(d.renderLessonsPanel(70, 8))

	overshoot := lineContaining(out, "L-0001")
	if !strings.Contains(overshoot, "↓") {
		t.Fatalf("overshoot row missing down arrow:\n%s", out)
	}
	undershoot := lineContaining(out, "L-0002")
	if !strings.Contains(undershoot, "↑") {
		t.Fatalf("undershoot row missing up arrow:\n%s", out)
	}
	noData := lineContaining(out, "L-0003")
	if strings.Contains(noData, "↓") || strings.Contains(noData, "↑") {
		t.Fatalf("no-data row should not show an accuracy arrow:\n%s", out)
	}
}

func TestTUI_HypothesisList(t *testing.T) {
	v := newHypothesisListView(goalScope{All: true})
	hs := []*entity.Hypothesis{
		{ID: "H-0001", Claim: "one", Status: entity.StatusOpen, Author: "human"},
		{ID: "H-0002", Claim: "two", Status: entity.StatusSupported, Author: "agent"},
	}
	nv, _ := v.update(hypListLoadedMsg{list: hs}, nil)
	out := stripANSI(nv.view(100, 20))
	for _, want := range []string{"2 hypotheses", "H-0001", "H-0002", "supported"} {
		if !strings.Contains(out, want) {
			t.Errorf("hypothesis list missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_HypothesisDetail(t *testing.T) {
	v := newHypothesisDetailView("H-0007")
	h := &entity.Hypothesis{
		ID: "H-0007", Parent: "H-0001", Claim: "tiled kernel", Status: entity.StatusOpen,
		Author: "agent:gen",
		Predicts: entity.Predicts{
			Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease", MinEffect: 0.1,
		},
		KillIf: []string{"size_flash grows >10%"},
	}
	nv, _ := v.update(hypDetailLoadedMsg{h: h}, nil)
	out := stripANSI(nv.view(100, 30))
	for _, want := range []string{"H-0007", "tiled kernel", "Predicts:", "decrease qemu_cycles", "Kill if:", "size_flash grows"} {
		if !strings.Contains(out, want) {
			t.Errorf("hypothesis detail missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_HypothesisDetailOpensLinkedEntities(t *testing.T) {
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
	nv, _ := v.update(hypDetailLoadedMsg{h: h, exps: exps, concls: concls, obs: obs}, nil)
	hv := nv.(*hypothesisDetailView)

	_, cmd := hv.update(tea.KeyMsg{Type: tea.KeyEnter}, nil)
	push := cmd().(tuiPushMsg)
	if got, want := push.v.kind(), kindExperimentDetail; got != want {
		t.Fatalf("first linked target kind = %s, want %s", got, want)
	}

	nv, _ = hv.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, nil)
	hv = nv.(*hypothesisDetailView)
	_, cmd = hv.update(tea.KeyMsg{Type: tea.KeyEnter}, nil)
	push = cmd().(tuiPushMsg)
	if got, want := push.v.kind(), kindConclusionDetail; got != want {
		t.Fatalf("second linked target kind = %s, want %s", got, want)
	}

	nv, _ = hv.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, nil)
	hv = nv.(*hypothesisDetailView)
	_, cmd = hv.update(tea.KeyMsg{Type: tea.KeyEnter}, nil)
	push = cmd().(tuiPushMsg)
	if got, want := push.v.kind(), kindObservationDetail; got != want {
		t.Fatalf("third linked target kind = %s, want %s", got, want)
	}
}

func TestTUI_ExperimentList(t *testing.T) {
	v := newExperimentListView(goalScope{All: true})
	es := []*entity.Experiment{
		{ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpImplemented, Instruments: []string{"qemu_cycles"}},
	}
	nv, _ := v.update(expListLoadedMsg{list: es}, nil)
	out := stripANSI(nv.view(100, 20))
	for _, want := range []string{"1 experiments", "E-0001", "implemented", "qemu_cycles"} {
		if !strings.Contains(out, want) {
			t.Errorf("experiment list missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_ExperimentDetail(t *testing.T) {
	v := newExperimentDetailView("E-0007")
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
	nv, _ := v.update(expDetailLoadedMsg{e: e, obs: obs}, nil)
	out := stripANSI(nv.view(120, 40))
	for _, want := range []string{"E-0007", "measured", "abc123def456", "O-0001", "qemu_cycles=750000", "pass"} {
		if !strings.Contains(out, want) {
			t.Errorf("experiment detail missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_ObservationDetail(t *testing.T) {
	v := newObservationDetailView("O-0003")
	pass := true
	ciLow, ciHigh := 1.1, 1.3
	o := &entity.Observation{
		ID:         "O-0003",
		Experiment: "E-0007",
		Instrument: "host_timing",
		MeasuredAt: time.Date(2026, 4, 12, 11, 0, 0, 0, time.UTC),
		Value:      1.2,
		Unit:       "s",
		Samples:    5,
		PerSample:  []float64{1.1, 1.2, 1.3},
		CILow:      &ciLow,
		CIHigh:     &ciHigh,
		CIMethod:   "bca",
		Pass:       &pass,
		Artifacts: []entity.Artifact{{
			Name: "primary", SHA: "abcdef1234567890", Path: "artifacts/ab/abcdef1234567890", Bytes: 2048,
		}},
		Command:     "make test",
		ExitCode:    0,
		Worktree:    "/tmp/wt",
		BaselineSHA: "fedcba9876543210",
		Author:      "agent:observer",
		Aux:         map[string]any{"stdev": 0.04, "warm": true},
	}
	nv, _ := v.update(obsDetailLoadedMsg{o: o}, nil)
	out := stripANSI(nv.view(120, 25))
	for _, want := range []string{"O-0003", "host_timing=1.2 s", "experiment=E-0007", "Artifacts (1):", "make test", "stdev", "warm"} {
		if !strings.Contains(out, want) {
			t.Errorf("observation detail missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_ConclusionList(t *testing.T) {
	v := newConclusionListView(goalScope{All: true})
	cs := []*entity.Conclusion{
		{ID: "C-0001", Hypothesis: "H-0001", Verdict: entity.VerdictSupported,
			Effect: entity.Effect{DeltaFrac: -0.25, PValue: 0.01}},
		{ID: "C-0002", Hypothesis: "H-0002", Verdict: entity.VerdictInconclusive,
			Strict: entity.Strict{RequestedFrom: "supported", Reasons: []string{"p>0.05"}}},
	}
	nv, _ := v.update(concListLoadedMsg{list: cs}, nil)
	out := stripANSI(nv.view(120, 20))
	for _, want := range []string{"2 conclusions", "C-0001", "supported", "C-0002", "↓from supported"} {
		if !strings.Contains(out, want) {
			t.Errorf("conclusion list missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_ConclusionDetail(t *testing.T) {
	v := newConclusionDetailView("C-0042")
	c := &entity.Conclusion{
		ID: "C-0042", Hypothesis: "H-0007", Verdict: entity.VerdictInconclusive,
		Author: "agent:analyst", CandidateExp: "E-0007",
		Strict:   entity.Strict{RequestedFrom: "supported", Reasons: []string{"size_flash exceeded"}},
		Effect:   entity.Effect{Instrument: "qemu_cycles", DeltaFrac: -0.1, PValue: 0.02, NCandidate: 10, NBaseline: 10, CIMethod: "bca"},
		StatTest: "welch",
	}
	nv, _ := v.update(concDetailLoadedMsg{c: c}, nil)
	out := stripANSI(nv.view(120, 30))
	for _, want := range []string{"C-0042", "inconclusive", "downgraded from supported", "size_flash exceeded", "Effect:", "welch", "delta_frac"} {
		if !strings.Contains(out, want) {
			t.Errorf("conclusion detail missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_EventListAndDetail(t *testing.T) {
	v := newEventListView(goalScope{All: true})
	es := []store.Event{
		{Ts: time.Now().UTC(), Kind: "experiment.implement", Actor: "agent:impl", Subject: "E-0007",
			Data: []byte(`{"worktree":"/tmp/wt","branch":"autoresearch/E-0007","samples":5,"pass":true}`)},
	}
	nv, _ := v.update(eventListLoadedMsg{list: es}, nil)
	out := stripANSI(nv.view(120, 20))
	for _, want := range []string{"1 events", "experiment.implement", "E-0007"} {
		if !strings.Contains(out, want) {
			t.Errorf("event list missing %q:\n%s", want, out)
		}
	}
	d := newEventDetailView(es[0])
	raw := d.view(120, 25)
	dout := stripANSI(raw)
	for _, want := range []string{"experiment.implement", "Payload:", "worktree", "autoresearch/E-0007"} {
		if !strings.Contains(dout, want) {
			t.Errorf("event detail missing %q:\n%s", want, dout)
		}
	}
	// The payload must be multi-line (indented) and not a one-liner blob.
	payloadStart := strings.Index(dout, "Payload:")
	if payloadStart < 0 {
		t.Fatalf("no Payload: heading")
	}
	payload := dout[payloadStart:]
	if strings.Count(payload, "\n") < 3 {
		t.Errorf("payload does not look indented (too few newlines):\n%s", payload)
	}
	// Key should be on its own line and followed by the opening brace of
	// the object. Check for a real key/value pair layout.
	if !strings.Contains(payload, `"worktree": "/tmp/wt"`) {
		t.Errorf("payload missing indented worktree entry:\n%s", payload)
	}
	_ = raw // ANSI color escapes are stripped in non-TTY tests; visual dump
	// exercises the styled path.
}

func TestTUI_LessonDetailRendersMarkdown(t *testing.T) {
	v := newLessonDetailView("L-0007")
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
	nv, _ := v.update(lessonDetailLoadedMsg{l: l}, nil)
	raw := nv.view(100, 24)
	out := stripANSI(raw)
	for _, want := range []string{"L-0007", "keep the 8x-unrolled FIR tap loop", "Why", "host_timing", "cache stays warm", "single-accumulator"} {
		if !strings.Contains(out, want) {
			t.Errorf("lesson detail missing %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{"**Why**", "_single-accumulator_"} {
		if strings.Contains(out, bad) {
			t.Errorf("lesson detail leaked raw markdown %q:\n%s", bad, out)
		}
	}
}

func TestTUI_LessonListUsesAccuracyArrows(t *testing.T) {
	v := newLessonListView(goalScope{All: true})
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
	nv, _ := v.update(lessonListLoadedMsg{
		list: lessons,
		accuracy: map[string]lessonAccuracySummary{
			"L-0002": {Overshoot: 1, Undershoot: 2},
		},
	}, nil)
	out := stripANSI(nv.view(120, 20))

	noData := lineContaining(out, "L-0001")
	if strings.Contains(noData, "↓") || strings.Contains(noData, "↑") {
		t.Fatalf("validated lesson without comparisons should not show an arrow:\n%s", out)
	}
	undershoot := lineContaining(out, "L-0002")
	if !strings.Contains(undershoot, "↑≥10%") {
		t.Fatalf("undershooting lesson row missing up arrow:\n%s", out)
	}
}

func TestTUI_PrettyJSON(t *testing.T) {
	raw := []byte(`{"name":"fir","count":3,"nested":{"k":true,"v":null,"n":-2.5e3},"arr":[1,"two",false]}`)
	plain := stripANSI(prettyJSON(raw, "  "))
	// Expected structural layout: every line prefixed by "  ", nested
	// objects/arrays indented by an additional 2 spaces per level.
	for _, want := range []string{
		"  {",
		`    "name": "fir"`,
		`    "count": 3`,
		`    "nested": {`,
		`      "k": true`,
		`      "v": null`,
		`    "arr": [`,
		"  }",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("prettyJSON missing %q:\n%s", want, plain)
		}
	}
	// Invalid JSON falls back to raw bytes (still prefixed).
	bad := prettyJSON([]byte("not json"), "  ")
	if bad != "  not json" {
		t.Errorf("prettyJSON fallback wrong: %q", bad)
	}
	// Empty payload shows a dim marker.
	empty := stripANSI(prettyJSON(nil, "  "))
	if !strings.Contains(empty, "(empty)") {
		t.Errorf("prettyJSON empty = %q, want '(empty)'", empty)
	}
}

func TestTUI_TreeView(t *testing.T) {
	v := newTreeView(goalScope{All: true})
	nodes := []*treeNode{
		{ID: "H-0001", Claim: "root hyp", Status: entity.StatusOpen},
		{ID: "H-0002", Claim: "another", Status: entity.StatusSupported},
	}
	nv, _ := v.update(treeLoadedMsg{roots: nodes}, nil)
	out := stripANSI(nv.view(100, 20))
	for _, want := range []string{"H-0001", "root hyp", "H-0002", "another"} {
		if !strings.Contains(out, want) {
			t.Errorf("tree view missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_FrontierView(t *testing.T) {
	v := newFrontierView(goalScope{GoalID: "G-0001"})
	g := &entity.Goal{Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"}}
	rows := []frontierRow{{Conclusion: "C-0001", Hypothesis: "H-0001", Value: 750067, DeltaFrac: -0.25}}
	nv, _ := v.update(frontierLoadedMsg{
		goal:       g,
		rows:       rows,
		assessment: frontierGoalAssessment{Mode: "open_ended", RecommendedAction: "continue"},
		stalled:    2,
	}, nil)
	out := stripANSI(nv.view(120, 20))
	for _, want := range []string{"decrease qemu_cycles", "stalled_for=2", "open-ended -> continue_until_stall", "C-0001", "H-0001", "750067"} {
		if !strings.Contains(out, want) {
			t.Errorf("frontier view missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_GoalDetailView(t *testing.T) {
	v := newGoalDetailView("G-0001")
	flash := 65536.0
	g := &entity.Goal{
		ID:          "G-0001",
		Status:      entity.GoalStatusActive,
		Objective:   entity.Objective{Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease"},
		Completion:  &entity.Completion{Threshold: 0.2, OnThreshold: entity.GoalOnThresholdAskHuman},
		Constraints: []entity.Constraint{{Instrument: "size_flash", Max: &flash}, {Instrument: "host_test", Require: "pass"}},
	}
	nv, _ := v.update(goalDetailLoadedMsg{g: g}, nil)
	out := stripANSI(nv.view(100, 20))
	for _, want := range []string{"G-0001", "Objective:", "decrease qemu_cycles", "Completion:", "threshold=0.2 -> ask_human", "size_flash", "host_test", "require=pass"} {
		if !strings.Contains(out, want) {
			t.Errorf("goal detail missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_GoalListView(t *testing.T) {
	v := newGoalListView()
	goals := []*entity.Goal{
		{ID: "G-0001", Status: entity.GoalStatusConcluded, Objective: entity.Objective{Instrument: "host_timing", Direction: "decrease"}},
		{ID: "G-0002", Status: entity.GoalStatusActive, Objective: entity.Objective{Instrument: "qemu_cycles", Direction: "decrease"}},
	}
	nv, _ := v.update(goalListLoadedMsg{all: goals, current: "G-0002"}, nil)
	out := stripANSI(nv.view(100, 20))
	for _, want := range []string{"2 goals", "G-0001", "G-0002", "qemu_cycles"} {
		if !strings.Contains(out, want) {
			t.Errorf("goal list missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_StatusView(t *testing.T) {
	snap := tuiRichSnapshot()
	snap.ScopeAll = true

	v := newStatusView(goalScope{All: true})
	nv, _ := v.update(dashLoadedMsg{snap: snap}, nil)
	out := stripANSI(nv.view(100, 30))
	for _, want := range []string{"Scope:", "all", "State:", "active", "Mode:", "strict", "Main checkout:", "clean", "Budget:", "5/20 experiments", "Counts:"} {
		if !strings.Contains(out, want) {
			t.Errorf("status view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "stalled") {
		t.Fatalf("all-goal status view should not show stalled meter:\n%s", out)
	}
}

func TestTUI_StatusViewShowsStalledMeterForGoalScope(t *testing.T) {
	snap := tuiRichSnapshot()
	snap.ScopeGoalID = "G-0002"

	v := newStatusView(goalScope{GoalID: "G-0002"})
	nv, _ := v.update(dashLoadedMsg{snap: snap}, nil)
	out := stripANSI(nv.view(100, 30))
	if !strings.Contains(out, "stalled 2/5") {
		t.Fatalf("goal-scoped status view should show stalled meter:\n%s", out)
	}
}

func TestTUI_DashboardAndStatusShowDirtyMainCheckout(t *testing.T) {
	snap := tuiRichSnapshot()
	snap.MainCheckoutDirty = true
	snap.MainCheckoutDirtyPaths = []string{"bootstrap.sh"}

	dash := newDashboardView(goalScope{All: true})
	nv, _ := dash.update(dashLoadedMsg{snap: snap}, nil)
	out := stripANSI(nv.view(140, 40))
	for _, want := range []string{"Main checkout dirty:", "bootstrap.sh"} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard dirty warning missing %q:\n%s", want, out)
		}
	}

	status := newStatusView(goalScope{All: true})
	nv, _ = status.update(dashLoadedMsg{snap: snap}, nil)
	out = stripANSI(nv.view(100, 30))
	for _, want := range []string{"Main checkout:", "dirty outside autoresearch-managed files", "bootstrap.sh"} {
		if !strings.Contains(out, want) {
			t.Errorf("status dirty warning missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_ModelHeaderAndHints(t *testing.T) {
	m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
	m.width, m.height = 120, 30
	// Feed a loaded snapshot to the dashboard so it renders non-empty.
	top := m.top()
	nv, _ := top.update(dashLoadedMsg{snap: tuiRichSnapshot()}, nil)
	m.setTop(nv)
	out := stripANSI(m.View())
	for _, want := range []string{"Dashboard", "help", "quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("model chrome missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_ArtifactList(t *testing.T) {
	v := newArtifactListView(goalScope{All: true})
	rows := []artifactRow{
		{Observation: "O-0001", Instrument: "qemu_cycles", Name: "primary", SHA: "abc123def45678", Bytes: 2048, Path: "artifacts/ab/abc123def45678"},
		{Observation: "O-0002", Instrument: "host_test", Name: "primary", SHA: "fed987abc12345", Bytes: 512000, Path: "artifacts/fe/fed987abc12345"},
	}
	nv, _ := v.update(artifactListLoadedMsg{rows: rows}, nil)
	out := stripANSI(nv.view(120, 20))
	for _, want := range []string{"2 artifacts", "O-0001", "qemu_cycles", "abc123def456", "500.0K", "host_test"} {
		if !strings.Contains(out, want) {
			t.Errorf("artifact list missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_ArtifactViewHeader(t *testing.T) {
	row := artifactRow{
		Observation: "O-0001", Instrument: "qemu_cycles", Name: "primary",
		SHA: "abcdef1234567890", Bytes: 4096, Path: "artifacts/ab/abcdef1234567890",
	}
	v := newArtifactView(row)
	// Feed a synthetic loaded message: no real file, but the header still
	// renders correctly.
	nv, _ := v.update(artifactLoadedMsg{
		sha:   "abcdef1234567890",
		abs:   "/fake/abs",
		rel:   "artifacts/ab/abcdef1234567890",
		lines: []string{"line 1", "line 2", "line 3"},
		total: 3,
		bytes: 4096,
	}, nil)
	out := stripANSI(nv.view(100, 20))
	for _, want := range []string{"abcdef1234567890", "artifacts/ab/abcdef", "lines=3", "mode=head"} {
		if !strings.Contains(out, want) {
			t.Errorf("artifact view missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_ArtifactDiffView(t *testing.T) {
	v := newArtifactDiffView(
		"abcdef1234567890", "a/path",
		"1234567890abcdef", "b/path",
		[]string{"@@ -1,3 +1,3 @@", "-old", "+new", " same"},
	)
	out := stripANSI(v.view(100, 20))
	for _, want := range []string{"a/path", "b/path", "-old", "+new"} {
		if !strings.Contains(out, want) {
			t.Errorf("artifact diff missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_InstrumentList(t *testing.T) {
	v := newInstrumentListView()
	by := map[string]store.Instrument{
		"qemu_cycles": {Cmd: []string{"bash", "-c", "./run"}, Parser: "builtin:scalar", Pattern: "cycles:(\\d+)", Unit: "cycles", MinSamples: 5},
		"host_test":   {Parser: "builtin:passfail"},
	}
	nv, _ := v.update(instrumentListLoadedMsg{by: by}, nil)
	out := stripANSI(nv.view(120, 20))
	for _, want := range []string{"2 instruments", "qemu_cycles", "host_test", "builtin:scalar", "pattern=/cycles", "min_samples=5"} {
		if !strings.Contains(out, want) {
			t.Errorf("instrument list missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_ReportView(t *testing.T) {
	v := newReportView("H-0007")
	nv, _ := v.update(reportLoadedMsg{md: "# H-0007 — my claim\n\n**Status**: open\n\n## Prediction\n\n- stuff"}, nil)
	out := stripANSI(nv.view(100, 20))
	for _, want := range []string{"H-0007", "my claim", "Prediction"} {
		if !strings.Contains(out, want) {
			t.Errorf("report view missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_ExperimentDetailWithStats(t *testing.T) {
	v := newExperimentDetailView("E-0007")
	e := &entity.Experiment{
		ID: "E-0007", Hypothesis: "H-0002", Status: entity.ExpMeasured,
		Instruments: []string{"qemu_cycles"}, Author: "agent:impl",
	}
	obs := []*entity.Observation{
		{ID: "O-0001", Experiment: "E-0007", Instrument: "qemu_cycles", Value: 1000, Unit: "cycles", Samples: 5, PerSample: []float64{1000, 1010, 990, 1005, 995}},
	}
	summ := map[string]stats.Summary{
		"qemu_cycles": {N: 5, Mean: 1000, StdDev: 7.07, Min: 990, Max: 1010, CILow: 993, CIHigh: 1007, CIMethod: "bca"},
	}
	nv, _ := v.update(expDetailLoadedMsg{e: e, obs: obs, summ: summ}, nil)
	out := stripANSI(nv.view(130, 40))
	for _, want := range []string{"E-0007", "Summary (per instrument):", "qemu_cycles", "1000", "bca"} {
		if !strings.Contains(out, want) {
			t.Errorf("experiment detail stats missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_JumpToCanonicalizes(t *testing.T) {
	m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
	// Start: [dashboard]
	if got := len(m.stack); got != 1 {
		t.Fatalf("initial stack depth = %d, want 1", got)
	}
	// Jump to hypotheses: [dashboard, hypotheses]
	m.jumpTo(newHypothesisListView(goalScope{All: true}), nil)
	if got := len(m.stack); got != 2 || m.top().kind() != kindHypothesisList {
		t.Fatalf("after H: depth=%d top=%s", got, m.top().kind())
	}
	// Jump to hypotheses again: still [dashboard, hypotheses], not 3-deep.
	m.jumpTo(newHypothesisListView(goalScope{All: true}), nil)
	if got := len(m.stack); got != 2 {
		t.Errorf("H after H: depth=%d want 2", got)
	}
	// Push a detail: [dashboard, hypotheses, detail]
	m.push(newHypothesisDetailView("H-0001"))
	if got := len(m.stack); got != 3 {
		t.Fatalf("after push detail: depth=%d want 3", got)
	}
	// Jump to hypotheses again: truncates to [dashboard, hypotheses], dropping the detail.
	m.jumpTo(newHypothesisListView(goalScope{All: true}), nil)
	if got := len(m.stack); got != 2 {
		t.Errorf("H from detail: depth=%d want 2", got)
	}
	if m.top().kind() != kindHypothesisList {
		t.Errorf("top = %s, want hypothesis.list", m.top().kind())
	}
	// Jump to experiments from hypotheses: [dashboard, experiments].
	m.jumpTo(newExperimentListView(goalScope{All: true}), nil)
	if got := len(m.stack); got != 2 || m.top().kind() != kindExperimentList {
		t.Fatalf("E after H: depth=%d top=%s", got, m.top().kind())
	}
	// R opens the report-mode hypothesis list — a different kind, so it pushes.
	m.jumpTo(newHypothesisListViewForReport(goalScope{All: true}), nil)
	if got := len(m.stack); got != 2 || m.top().kind() != kindHypothesisReport {
		t.Errorf("R after E: depth=%d top=%s", got, m.top().kind())
	}
}

func TestTUI_EventListKeysDoNotConflictWithRootShortcuts(t *testing.T) {
	v := newEventListView(goalScope{All: true})
	es := []store.Event{
		{Ts: time.Now().UTC(), Kind: "init", Subject: "A"},
		{Ts: time.Now().UTC(), Kind: "hypothesis.add", Subject: "B"},
	}
	nv, _ := v.update(eventListLoadedMsg{list: es}, nil)
	ev := nv.(*eventListView)
	ev.cursor = 0
	ev.follow = false

	m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
	m.stack = []tuiView{newDashboardView(goalScope{All: true}), ev}

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = model.(tuiModel)
	if m.top().kind() != kindEventList {
		t.Fatalf("G should stay in event list, top=%s", m.top().kind())
	}
	ev = m.top().(*eventListView)
	if got, want := ev.cursor, len(ev.filtered)-1; got != want {
		t.Fatalf("G should move cursor to bottom, got %d want %d", got, want)
	}
	if !ev.follow {
		t.Fatalf("G should re-enable follow mode at the tail")
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'W'}})
	m = model.(tuiModel)
	ev = m.top().(*eventListView)
	if ev.follow {
		t.Fatalf("W should toggle follow mode off")
	}
}

func TestTUI_GoalShortcutUsesO(t *testing.T) {
	m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'O'}})
	m = model.(tuiModel)
	if m.top().kind() != kindGoalList {
		t.Fatalf("O should jump to goal view, top=%s", m.top().kind())
	}
}

func TestTUI_DashboardForwardsLoadMsgToOverlay(t *testing.T) {
	d := newDashboardView(goalScope{All: true})
	// Prime the dashboard with a snapshot so focus/cursor are meaningful.
	snap := tuiRichSnapshot()
	nv, _ := d.update(dashLoadedMsg{snap: snap}, nil)
	d = nv.(*dashboardView)
	// Simulate the user drilling down with Enter on the tree panel.
	d.focus = dashFocusTree
	_ = d.openSelected(nil) // rightOverlay is now a hypothesisDetailView (compact)
	if d.rightOverlay == nil {
		t.Fatalf("expected rightOverlay after drill-down")
	}
	// Feed a loaded detail message — should reach the overlay, not be
	// swallowed by the dashboard. Before the fix it would be ignored and
	// the compact detail would stay on "loading…".
	h := &entity.Hypothesis{
		ID: "H-0001", Claim: "unrolling dsp_fir", Status: entity.StatusSupported,
		Predicts: entity.Predicts{Instrument: "qemu_cycles", Direction: "decrease"},
	}
	nv, _ = d.update(hypDetailLoadedMsg{h: h}, nil)
	d = nv.(*dashboardView)
	out := stripANSI(d.rightOverlay.view(60, 20))
	for _, want := range []string{"H-0001", "unrolling dsp_fir"} {
		if !strings.Contains(out, want) {
			t.Errorf("overlay still showing loading state after load msg, missing %q:\n%s", want, out)
		}
	}
}

func TestTUI_DashboardTreePanelTitleUsesBorder(t *testing.T) {
	d := newDashboardView(goalScope{All: true})
	d.snap = tuiRichSnapshot()

	out := stripANSI(d.renderTreePanel(60, 8))
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("tree panel rendered too few lines:\n%s", out)
	}
	if !strings.Contains(lines[0], "┤ Hypothesis tree ├") {
		t.Fatalf("top border missing titled border treatment:\n%s", out)
	}
	if strings.Contains(lines[1], "Hypothesis tree") {
		t.Fatalf("title should not consume the first content row:\n%s", out)
	}
}

func TestTUI_DashboardRecentEventsPaging(t *testing.T) {
	s := newStoreWithBuiltins(t)
	base := time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 70; i++ {
		if err := s.AppendEvent(store.Event{
			Ts:      base.Add(time.Duration(i) * time.Second),
			Kind:    "observation.record",
			Subject: fmt.Sprintf("O-%03d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	d := newDashboardView(goalScope{All: true})
	nv, _ := d.update(dashLoadedMsg{snap: baseSnapshot()}, s)
	d = nv.(*dashboardView)
	d.focus = dashFocusEvents

	msg := loadDashboardEventsCmd(s, goalScope{All: true}, 0, dashboardRecentEventsPageSize, true)()
	nv, _ = d.update(msg, s)
	d = nv.(*dashboardView)

	if got, want := len(d.events), dashboardRecentEventsPageSize; got != want {
		t.Fatalf("initial event page len = %d, want %d", got, want)
	}
	if got, want := d.events[0].Subject, "O-069"; got != want {
		t.Fatalf("newest loaded event = %q, want %q", got, want)
	}
	if got, want := d.events[len(d.events)-1].Subject, "O-006"; got != want {
		t.Fatalf("last event in first page = %q, want %q", got, want)
	}

	d.cursors[dashFocusEvents] = len(d.events) - 1
	cmd := d.maybeLoadMoreEvents(s)
	if cmd == nil {
		t.Fatal("expected lazy-load command when cursor reaches older edge")
	}
	nv, _ = d.update(cmd(), s)
	d = nv.(*dashboardView)

	if got, want := len(d.events), 70; got != want {
		t.Fatalf("paged event len = %d, want %d", got, want)
	}
	if got, want := d.events[len(d.events)-1].Subject, "O-000"; got != want {
		t.Fatalf("oldest loaded event = %q, want %q", got, want)
	}
	if !d.eventsAllLoaded {
		t.Fatalf("expected all events to be loaded after paging")
	}
}

func TestTUI_DashboardRightColumnGivesEventsRemainder(t *testing.T) {
	d := newDashboardView(goalScope{All: true})
	d.snap = tuiRichSnapshot()

	frontierH, inFlightH, eventsH := d.rightColumnHeights(30)
	if eventsH <= frontierH {
		t.Fatalf("events panel height = %d, want > frontier height %d", eventsH, frontierH)
	}
	if eventsH <= inFlightH {
		t.Fatalf("events panel height = %d, want > in-flight height %d", eventsH, inFlightH)
	}
}

// TestTUI_DashboardVisualDump renders a realistic dashboard snapshot and
// dumps it with t.Log so `go test -v -run VisualDump` lets a human
// eyeball the layout. No assertions — this is a development aid.
func TestTUI_DashboardVisualDump(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	snap := tuiRichSnapshot()
	// Add more events + in-flight to stress the layout.
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
	t.Log("\n" + m.View())
}

// TestTUI_ReportViewRendersMarkdown asserts that glamour is actually styling
// the report body — i.e. the raw `**Status**:` sigils are consumed by the
// renderer and the plain-text word "Status" survives in the output. If
// glamour stops running (dep removed, style missing, whatever) the raw `**`
// would leak through and this test would fail.
func TestTUI_ReportViewRendersMarkdown(t *testing.T) {
	v := newReportView("H-0007")
	md := "# H-0007 — my claim\n\n**Status**: open  \n\n## Prediction\n\n- **Instrument**: `qemu_cycles`\n- _italic note_\n"
	nv, _ := v.update(reportLoadedMsg{md: md}, nil)
	rv := nv.(*reportView)
	rendered := rv.ensureRendered(100)
	plain := stripANSI(rendered)
	// Content must survive:
	for _, want := range []string{"H-0007", "my claim", "Status", "Prediction", "Instrument", "qemu_cycles", "italic note"} {
		if !strings.Contains(plain, want) {
			t.Errorf("rendered report missing %q:\n%s", want, plain)
		}
	}
	// Raw inline sigils must NOT survive — glamour replaced them with ANSI
	// styling. (H2/H3 prefixes like `## Prediction` are intentionally kept
	// by glamour's dark style as a visual marker, so we don't check those.)
	for _, bad := range []string{"**Status**", "_italic note_"} {
		if strings.Contains(plain, bad) {
			t.Errorf("rendered report still contains raw markdown sigil %q:\n%s", bad, plain)
		}
	}
	// Rendered output must differ from raw — glamour injected at least some ANSI.
	if rendered == md {
		t.Errorf("rendered == raw markdown (glamour did not run):\n%s", rendered)
	}
}

// TestTUI_ReportViewCachesByWidth ensures the glamour cache invalidates on
// resize. Rendering at width 80 then 120 should produce different outputs,
// and a repeated render at width 80 should hit the cache (same result).
func TestTUI_ReportViewCachesByWidth(t *testing.T) {
	v := newReportView("H-0007")
	// A paragraph long enough that 80 vs 120 col wrapping produces visibly
	// different line counts.
	md := "# Heading\n\nlorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua ut enim ad minim veniam\n"
	nv, _ := v.update(reportLoadedMsg{md: md}, nil)
	rv := nv.(*reportView)

	at80 := rv.ensureRendered(80)
	if rv.renderedWidth != 80 {
		t.Errorf("renderedWidth = %d, want 80", rv.renderedWidth)
	}
	at80Again := rv.ensureRendered(80)
	if at80 != at80Again {
		t.Errorf("second render at same width returned different output — cache miss")
	}
	at120 := rv.ensureRendered(120)
	if rv.renderedWidth != 120 {
		t.Errorf("renderedWidth = %d after resize, want 120", rv.renderedWidth)
	}
	if at80 == at120 {
		t.Errorf("rendered output unchanged between width 80 and 120 — word wrap is not width-aware")
	}
}

// TestTUI_ReportViewVisualDump dumps a realistic report so the eyeball-check
// `go test -run ReportViewVisualDump -v` shows styled markdown.
func TestTUI_ReportViewVisualDump(t *testing.T) {
	if testing.Short() {
		t.Skip()
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
	t.Log("\n" + nv.view(100, 40))
}

// TestTUI_EventDetailVisualDump dumps a realistic event payload so
// `go test -run EventDetailVisualDump -v` shows the ANSI-colored JSON.
func TestTUI_EventDetailVisualDump(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	ev := store.Event{
		Ts:      time.Date(2026, 4, 12, 14, 30, 0, 0, time.UTC),
		Kind:    "conclusion.critic_downgrade",
		Actor:   "agent:critic",
		Subject: "C-0042",
		Data:    []byte(`{"from":"supported","to":"inconclusive","hypothesis":"H-0007","reasons":["size_flash exceeded 65536","n_candidate=3 below min_samples=5"],"n_candidate":3,"p_value":0.021,"strict":true,"reviewed_by":null}`),
	}
	d := newEventDetailView(ev)
	t.Log("\n" + d.view(120, 30))
}

// TestTUI_ExperimentDetailVisualDump renders the experiment detail with a
// realistic set of observations + summary stats to eyeball the alignment.
func TestTUI_ExperimentDetailVisualDump(t *testing.T) {
	if testing.Short() {
		t.Skip()
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
	nv, _ := v.update(expDetailLoadedMsg{e: e, obs: obs, summ: summ}, nil)
	t.Log("\n" + nv.view(130, 50))
}
