package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

// captureDashboardInFlight_ExcludesReferencedBaselines exercises the
// dashboard's capture path against a real store to lock in the rule that
// experiments already referenced as a baseline drop out of the "in flight"
// panel regardless of their status.
func TestCaptureDashboard_InFlightExcludesReferencedBaselines(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	baseline := &entity.Experiment{
		ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpMeasured,
		Baseline:               entity.Baseline{Ref: "HEAD"},
		Instruments:            []string{"host_timing"},
		Author:                 "agent:designer",
		CreatedAt:              now,
		ReferencedAsBaselineBy: []string{"C-0001"},
	}
	candidate := &entity.Experiment{
		ID: "E-0002", Hypothesis: "H-0001", Status: entity.ExpMeasured,
		Baseline:    entity.Baseline{Ref: "HEAD"},
		Instruments: []string{"host_timing"},
		Author:      "agent:designer",
		CreatedAt:   now,
	}
	stuck := &entity.Experiment{
		ID: "E-0003", Hypothesis: "H-0002", Status: entity.ExpImplemented,
		Baseline:    entity.Baseline{Ref: "HEAD"},
		Instruments: []string{"host_timing"},
		Author:      "agent:implementer",
		CreatedAt:   now,
	}
	for _, e := range []*entity.Experiment{baseline, candidate, stuck} {
		if err := s.WriteExperiment(e); err != nil {
			t.Fatal(err)
		}
	}

	snap, err := captureDashboard(s)
	if err != nil {
		t.Fatal(err)
	}

	// Baseline (E-0001) must be excluded — it has a back-reference.
	// Candidate (E-0002) is still in-flight (no back-reference, status=measured).
	// Implementer-abandoned (E-0003) is still in-flight (status=implemented).
	ids := make(map[string]bool, len(snap.InFlight))
	for _, r := range snap.InFlight {
		ids[r.ID] = true
	}
	if ids["E-0001"] {
		t.Error("E-0001 is a referenced baseline and should NOT appear in-flight")
	}
	if !ids["E-0002"] {
		t.Error("E-0002 is an unreferenced measured candidate and SHOULD appear in-flight")
	}
	if !ids["E-0003"] {
		t.Error("E-0003 is an unreferenced implemented experiment and SHOULD appear in-flight")
	}
}

// Synthetic snapshot factory: avoids needing a real on-disk store for the
// pure-rendering tests. The capture path is exercised separately via the
// shell smoke tests where a real init + state is cheap.

func baseSnapshot() *dashboardSnapshot {
	now := time.Date(2026, 4, 11, 18, 42, 0, 0, time.UTC)
	return &dashboardSnapshot{
		Project:      "/tmp/fir",
		Mode:         "strict",
		Counts:       map[string]int{"hypotheses": 0, "experiments": 0, "observations": 0, "conclusions": 0},
		Tree:         []*treeNode{},
		Frontier:     []frontierRow{},
		InFlight:     []dashboardInFlight{},
		RecentEvents: []store.Event{},
		CapturedAt:   now,
	}
}

func TestRenderDashboard_Empty(t *testing.T) {
	snap := baseSnapshot()
	var buf bytes.Buffer
	renderDashboard(&buf, snap, 80, "snapshot", nil)
	out := buf.String()

	for _, want := range []string{
		"autoresearch — /tmp/fir",
		"[active]",
		"(no goal set",
		"0 experiments",
		"(no hypotheses)",
		"(no goal set)",
		"(no events yet)",
		"snapshot · Ctrl-C to exit",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("empty dashboard missing %q:\n%s", want, out)
		}
	}
}

func TestRenderDashboard_FullState(t *testing.T) {
	snap := baseSnapshot()
	flash := 65536.0
	snap.Goal = &entity.Goal{
		Objective: entity.Objective{
			Instrument:   "qemu_cycles",
			Target:       "dsp_fir",
			Direction:    "decrease",
			TargetEffect: 0.20,
		},
		Constraints: []entity.Constraint{
			{Instrument: "size_flash", Max: &flash},
			{Instrument: "host_test", Require: "pass"},
		},
	}
	snap.Counts = map[string]int{"hypotheses": 3, "experiments": 5, "observations": 12, "conclusions": 2}
	snap.Budgets.Limits.MaxExperiments = 20
	snap.Budgets.Limits.MaxWallTimeH = 8
	snap.Budgets.Limits.FrontierStallK = 5
	snap.Budgets.Usage.Experiments = 5
	snap.Budgets.Usage.ElapsedH = 1.2

	snap.Tree = []*treeNode{
		{ID: "H-0001", Claim: "unrolling dsp_fir", Status: entity.StatusSupported, Author: "human:alice"},
		{ID: "H-0002", Claim: "fixed-point rewrite", Status: entity.StatusOpen, Author: "agent:generator",
			Children: []*treeNode{
				{ID: "H-0003", Claim: "sub: Q15 only", Status: entity.StatusInconclusive, Author: "agent:generator"},
			}},
	}

	snap.Frontier = []frontierRow{
		{Conclusion: "C-0001", Hypothesis: "H-0001", Value: 750067, DeltaFrac: -0.25},
	}
	snap.StalledFor = 2

	impAt := time.Now().UTC().Add(-2*time.Minute - 14*time.Second)
	snap.InFlight = []dashboardInFlight{
		{
			ID: "E-0007", Hypothesis: "H-0002", Status: entity.ExpMeasured,
			Instruments:   []string{"qemu_cycles", "host_test"},
			ImplementedAt: &impAt,
			ElapsedS:      time.Since(impAt).Seconds(),
		},
	}

	snap.RecentEvents = []store.Event{
		{Ts: time.Date(2026, 4, 11, 18, 42, 1, 0, time.UTC), Kind: "hypothesis.add", Actor: "agent:generator", Subject: "H-0003"},
		{Ts: time.Date(2026, 4, 11, 18, 42, 5, 0, time.UTC), Kind: "experiment.design", Actor: "agent:designer", Subject: "E-0007"},
	}

	var buf bytes.Buffer
	renderDashboard(&buf, snap, 120, "refreshing every 2s", nil)
	out := buf.String()

	expectations := []string{
		"Goal: decrease qemu_cycles on dsp_fir (target_effect=0.2)",
		"size_flash ≤ 65536",
		"host_test require=pass",
		"5/20 experiments",
		"1.2h/8h elapsed",
		"stalled 2/5",
		"3 hypotheses · 5 experiments · 12 observations · 2 conclusions",
		"H-0001",
		"H-0003", // child rendered
		"C-0001  H-0001  qemu_cycles=750067",
		"(stalled_for: 2 of 5)",
		"E-0007",
		"instruments=qemu_cycles,host_test",
		"hypothesis.add",
		"experiment.design",
		"refreshing every 2s · Ctrl-C to exit",
	}
	for _, want := range expectations {
		if !strings.Contains(out, want) {
			t.Errorf("full-state dashboard missing %q:\n%s", want, out)
		}
	}
}

func TestRenderDashboard_Paused(t *testing.T) {
	snap := baseSnapshot()
	snap.Paused = true
	snap.PauseReason = "human review of E-0005"

	var buf bytes.Buffer
	renderDashboard(&buf, snap, 120, "snapshot", nil)
	out := buf.String()

	if !strings.Contains(out, "[PAUSED: human review of E-0005]") {
		t.Errorf("paused header missing:\n%s", out)
	}
}

func TestRenderDashboard_StatusGlyphs(t *testing.T) {
	snap := baseSnapshot()
	snap.Tree = []*treeNode{
		{ID: "H-0001", Claim: "supported", Status: entity.StatusSupported},
		{ID: "H-0002", Claim: "refuted", Status: entity.StatusRefuted},
		{ID: "H-0003", Claim: "inconclusive", Status: entity.StatusInconclusive},
		{ID: "H-0004", Claim: "killed", Status: entity.StatusKilled},
		{ID: "H-0005", Claim: "open", Status: entity.StatusOpen},
	}
	var buf bytes.Buffer
	renderDashboard(&buf, snap, 120, "snapshot", nil)
	out := buf.String()
	for _, glyph := range []string{"✓", "✗", "?", "☠", "•"} {
		if !strings.Contains(out, glyph) {
			t.Errorf("missing status glyph %q:\n%s", glyph, out)
		}
	}
}

// TestRenderDashboard_Colored exercises the color-on rendering path. We
// bypass TTY detection by constructing an *ansi{enabled: true} directly, so
// the test is deterministic regardless of the host environment, NO_COLOR, or
// whether `go test` is run under a TTY.
func TestRenderDashboard_Colored(t *testing.T) {
	snap := baseSnapshot()
	flash := 65536.0
	snap.Goal = &entity.Goal{
		Objective: entity.Objective{
			Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease", TargetEffect: 0.2,
		},
		Constraints: []entity.Constraint{{Instrument: "size_flash", Max: &flash}},
	}
	snap.Counts = map[string]int{"hypotheses": 1, "experiments": 1, "observations": 0, "conclusions": 0}
	snap.Budgets.Limits.MaxExperiments = 20
	snap.Budgets.Usage.Experiments = 18 // 90% → should land in the red bucket
	snap.Tree = []*treeNode{
		{ID: "H-0001", Claim: "supported hypo", Status: entity.StatusSupported},
	}
	snap.Frontier = []frontierRow{
		{Conclusion: "C-0001", Hypothesis: "H-0001", Value: 750067, DeltaFrac: -0.25},
	}
	snap.RecentEvents = []store.Event{
		{Ts: time.Date(2026, 4, 11, 18, 42, 1, 0, time.UTC), Kind: "hypothesis.add", Actor: "agent:generator", Subject: "H-0001"},
		{Ts: time.Date(2026, 4, 11, 18, 42, 5, 0, time.UTC), Kind: "experiment.design", Actor: "agent:designer", Subject: "E-0001"},
	}

	var buf bytes.Buffer
	renderDashboard(&buf, snap, 100, "snapshot", &ansi{enabled: true})
	out := buf.String()

	// Any output at all should contain escape sequences when color is on.
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ANSI escapes in colored output:\n%s", out)
	}
	// Supported glyph should be wrapped in green (code 32).
	if !strings.Contains(out, "\x1b[32m✓\x1b[0m") {
		t.Errorf("supported glyph not green:\n%s", out)
	}
	// Budget at 90% usage should use red (code 31).
	if !strings.Contains(out, "\x1b[31m18/20 experiments\x1b[0m") {
		t.Errorf("budget meter not red at 90%% usage:\n%s", out)
	}
	// Stripping escapes should give back the same visible content as the
	// no-color render.
	var plainBuf bytes.Buffer
	renderDashboard(&plainBuf, snap, 100, "snapshot", nil)
	if got, want := stripANSI(out), plainBuf.String(); got != want {
		t.Errorf("stripped colored output differs from plain render\n--- stripped ---\n%s\n--- plain ---\n%s", got, want)
	}
}

func TestRenderDashboard_NoColorWhenNil(t *testing.T) {
	// Sanity: passing nil (and &ansi{} explicitly) should emit zero escape
	// sequences, so callers on pipes/non-TTYs get clean bytes.
	snap := baseSnapshot()
	snap.Tree = []*treeNode{
		{ID: "H-0001", Claim: "hypo", Status: entity.StatusSupported},
	}
	for _, a := range []*ansi{nil, {}} {
		var buf bytes.Buffer
		renderDashboard(&buf, snap, 80, "snapshot", a)
		if strings.Contains(buf.String(), "\x1b[") {
			t.Errorf("disabled colorizer emitted ANSI escapes:\n%s", buf.String())
		}
	}
}

// ---- capture path tests against a real store ----

// newStoreWithBuiltins spins up a temp store + registers the minimum
// instrument set the dashboard capture paths expect.
func newStoreWithBuiltins(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterInstrument("host_timing", store.Instrument{Unit: "seconds"}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCaptureDashboard_EmptyStore(t *testing.T) {
	s := newStoreWithBuiltins(t)
	snap, err := captureDashboard(s)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Paused {
		t.Errorf("paused on fresh store")
	}
	if snap.Mode != "strict" {
		t.Errorf("mode: %q", snap.Mode)
	}
	if snap.Goal != nil {
		t.Errorf("goal should be nil on fresh store")
	}
	if len(snap.Tree) != 0 {
		t.Errorf("tree should be empty")
	}
	if len(snap.Frontier) != 0 {
		t.Errorf("frontier should be empty: %+v", snap.Frontier)
	}
}

func TestCaptureDashboard_PauseStateReflected(t *testing.T) {
	s := newStoreWithBuiltins(t)
	now := time.Now().UTC()
	if err := s.UpdateState(func(st *store.State) error {
		st.Paused = true
		st.PauseReason = "testing"
		st.PausedAt = &now
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := captureDashboard(s)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Paused || snap.PauseReason != "testing" {
		t.Errorf("pause state not reflected: %+v", snap)
	}
}

func TestReadDashboardRecentEvents_PagedNewestFirst(t *testing.T) {
	s := newStoreWithBuiltins(t)
	base := time.Date(2026, 4, 12, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 12; i++ {
		if err := s.AppendEvent(store.Event{
			Ts:      base.Add(time.Duration(i) * time.Second),
			Kind:    "observation.record",
			Subject: fmt.Sprintf("O-%04d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	first, allLoaded, err := readDashboardRecentEvents(s, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if allLoaded {
		t.Fatalf("first page unexpectedly reported allLoaded")
	}
	if got, want := len(first), 5; got != want {
		t.Fatalf("first page len = %d, want %d", got, want)
	}
	if got, want := first[0].Subject, "O-0011"; got != want {
		t.Fatalf("newest event subject = %q, want %q", got, want)
	}
	if got, want := first[4].Subject, "O-0007"; got != want {
		t.Fatalf("fifth event subject = %q, want %q", got, want)
	}

	second, allLoaded, err := readDashboardRecentEvents(s, 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	if allLoaded {
		t.Fatalf("second page unexpectedly reported allLoaded")
	}
	if got, want := second[0].Subject, "O-0006"; got != want {
		t.Fatalf("second page first subject = %q, want %q", got, want)
	}
	if got, want := second[4].Subject, "O-0002"; got != want {
		t.Fatalf("second page fifth subject = %q, want %q", got, want)
	}

	last, allLoaded, err := readDashboardRecentEvents(s, 10, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !allLoaded {
		t.Fatalf("last page should report allLoaded")
	}
	if got, want := len(last), 2; got != want {
		t.Fatalf("last page len = %d, want %d", got, want)
	}
	if got, want := last[0].Subject, "O-0001"; got != want {
		t.Fatalf("last page first subject = %q, want %q", got, want)
	}
	if got, want := last[1].Subject, "O-0000"; got != want {
		t.Fatalf("last page second subject = %q, want %q", got, want)
	}
}

// Sanity: runeLen and formatElapsed behave correctly.
func TestRuneLen(t *testing.T) {
	if runeLen("autoresearch — /tmp/fir") != 23 {
		t.Errorf("runeLen of em-dash string wrong: %d", runeLen("autoresearch — /tmp/fir"))
	}
}

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "00:00"},
		{45 * time.Second, "00:45"},
		{2*time.Minute + 14*time.Second, "02:14"},
		{65 * time.Minute, "65:00"},
	}
	for _, c := range cases {
		if got := formatElapsed(c.d); got != c.want {
			t.Errorf("formatElapsed(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}
