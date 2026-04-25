package cli

import (
	"bytes"
	"fmt"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func baseSnapshot() *dashboardSnapshot {
	now := time.Date(2026, 4, 11, 18, 42, 0, 0, time.UTC)
	return &dashboardSnapshot{
		Project:                "/tmp/fir",
		Mode:                   "strict",
		MainCheckoutDirtyPaths: []string{},
		Counts:                 map[string]int{"hypotheses": 0, "experiments": 0, "observations": 0, "conclusions": 0},
		Tree:                   []*treeNode{},
		Frontier:               []frontierRow{},
		InFlight:               []dashboardInFlight{},
		RecentEvents:           []store.Event{},
		CapturedAt:             now,
	}
}

func populatedDashboardSnapshot() *dashboardSnapshot {
	snap := baseSnapshot()
	flash := 65536.0
	impAt := snap.CapturedAt.Add(-2 * time.Minute)
	snap.Goal = &entity.Goal{
		Objective: entity.Objective{
			Instrument: "qemu_cycles",
			Target:     "dsp_fir",
			Direction:  "decrease",
		},
		Completion: &entity.Completion{
			Threshold:   0.2,
			OnThreshold: entity.GoalOnThresholdAskHuman,
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
		{ID: "H-0001", Claim: "unrolling dsp_fir", Status: entity.StatusSupported, Author: "human"},
		{ID: "H-0002", Claim: "fixed-point rewrite", Status: entity.StatusOpen, Author: "agent:gen",
			Children: []*treeNode{
				{ID: "H-0003", Claim: "sub: Q15 only", Status: entity.StatusInconclusive, Author: "agent:gen"},
			}},
	}
	snap.Frontier = []frontierRow{{Conclusion: "C-0001", Hypothesis: "H-0001", Value: 750067, DeltaFrac: -0.25}}
	snap.StalledFor = 2
	snap.InFlight = []dashboardInFlight{{
		ID: "E-0007", Hypothesis: "H-0002", Status: entity.ExpMeasured,
		Instruments: []string{"qemu_cycles", "host_test"}, ImplementedAt: &impAt, ElapsedS: 120,
	}}
	snap.RecentEvents = []store.Event{
		{Ts: snap.CapturedAt.Add(time.Second), Kind: "experiment.design", Actor: "agent:des", Subject: "E-0007"},
		{Ts: snap.CapturedAt, Kind: "hypothesis.add", Actor: "agent:gen", Subject: "H-0003"},
	}
	return snap
}

func newStoreWithBuiltins() *store.Store {
	GinkgoHelper()
	s := createCLIStore()
	Expect(s.RegisterInstrument("host_timing", store.Instrument{Unit: "seconds"})).To(Succeed())
	return s
}

func dashboardTestHypothesis(id, status string, now time.Time) *entity.Hypothesis {
	GinkgoHelper()
	return &entity.Hypothesis{
		ID: id, GoalID: "G-0001", Claim: id + " claim",
		Predicts:  entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease"},
		KillIf:    []string{"tests fail"},
		Status:    status,
		Author:    "human",
		CreatedAt: now,
	}
}

func dashboardTestExperiment(id, hypothesis, status string, now time.Time, referencedBy ...string) *entity.Experiment {
	GinkgoHelper()
	return &entity.Experiment{
		ID: id, Hypothesis: hypothesis, Status: status,
		Baseline: entity.Baseline{Ref: "HEAD"}, Instruments: []string{"host_timing"},
		Author:                 "agent:designer",
		CreatedAt:              now,
		ReferencedAsBaselineBy: referencedBy,
	}
}

var _ = Describe("dashboard capture", func() {
	It("limits in-flight work to actionable experiments that are not already baselines", func() {
		s := createCLIStore()
		now := time.Now().UTC()
		for _, h := range []*entity.Hypothesis{
			dashboardTestHypothesis("H-0001", entity.StatusSupported, now),
			dashboardTestHypothesis("H-0002", entity.StatusOpen, now),
			dashboardTestHypothesis("H-0003", entity.StatusOpen, now),
		} {
			Expect(s.WriteHypothesis(h)).To(Succeed())
		}
		for _, e := range []*entity.Experiment{
			dashboardTestExperiment("E-0001", "H-0003", entity.ExpMeasured, now, "C-0001"),
			dashboardTestExperiment("E-0002", "H-0002", entity.ExpMeasured, now),
			dashboardTestExperiment("E-0003", "H-0002", entity.ExpImplemented, now),
			dashboardTestExperiment("E-0004", "H-0001", entity.ExpMeasured, now),
		} {
			Expect(s.WriteExperiment(e)).To(Succeed())
		}

		snap, err := captureDashboard(s)
		Expect(err).NotTo(HaveOccurred())
		Expect(snap.InFlight).To(ConsistOf(
			HaveField("ID", "E-0002"),
			HaveField("ID", "E-0003"),
		), "in-flight experiments")
	})

	It("limits stale experiments to work that is still actionable", func() {
		s := newStoreWithBuiltins()
		now := time.Now().UTC()

		Expect(s.UpdateConfig(func(cfg *store.Config) error {
			cfg.Budgets.StaleExperimentMinutes = 5
			return nil
		})).To(Succeed())

		for _, h := range []*entity.Hypothesis{
			dashboardTestHypothesis("H-0001", entity.StatusSupported, now),
			dashboardTestHypothesis("H-0002", entity.StatusUnreviewed, now),
			dashboardTestHypothesis("H-0003", entity.StatusOpen, now),
		} {
			Expect(s.WriteHypothesis(h)).To(Succeed())
		}
		for _, e := range []*entity.Experiment{
			dashboardTestExperiment("E-0001", "H-0001", entity.ExpMeasured, now),
			dashboardTestExperiment("E-0002", "H-0002", entity.ExpMeasured, now),
			dashboardTestExperiment("E-0003", "H-0003", entity.ExpMeasured, now),
		} {
			Expect(s.WriteExperiment(e)).To(Succeed())
		}

		staleAt := now.Add(-10 * time.Minute)
		for _, expID := range []string{"E-0001", "E-0002", "E-0003"} {
			Expect(s.AppendEvent(store.Event{
				Ts:      staleAt,
				Kind:    "experiment.measure",
				Subject: expID,
			})).To(Succeed())
		}

		snap, err := captureDashboard(s)
		Expect(err).NotTo(HaveOccurred())
		Expect(snap.InFlight).To(ConsistOf(HaveField("ID", "E-0003")), "in-flight experiments")
		Expect(snap.StaleExperiments).To(ConsistOf(HaveField("ID", "E-0003")), "stale experiments")
	})

	It("captures an empty initialized store", func() {
		s := newStoreWithBuiltins()
		snap, err := captureDashboard(s)
		Expect(err).NotTo(HaveOccurred())
		Expect(snap.Paused).To(BeFalse())
		Expect(snap.Mode).To(Equal("strict"))
		Expect(snap.Goal).To(BeNil())
		Expect(snap.Tree).To(BeEmpty())
		Expect(snap.Frontier).To(BeEmpty())
	})

	It("reflects pause state in the dashboard snapshot", func() {
		s := newStoreWithBuiltins()
		now := time.Now().UTC()
		Expect(s.UpdateState(func(st *store.State) error {
			st.Paused = true
			st.PauseReason = "testing"
			st.PausedAt = &now
			return nil
		})).To(Succeed())

		snap, err := captureDashboard(s)
		Expect(err).NotTo(HaveOccurred())
		Expect(snap.Paused).To(BeTrue())
		Expect(snap.PauseReason).To(Equal("testing"))
	})
})

var _ = Describe("dashboard rendering", func() {
	It("renders an empty snapshot", func() {
		snap := baseSnapshot()
		var buf bytes.Buffer
		renderDashboard(&buf, snap, 80, "snapshot", nil)

		expectText(buf.String(),
			"autoresearch — /tmp/fir",
			"[active]",
			"(no goal set",
			"0 experiments",
			"(no hypotheses)",
			"(no goal set)",
			"(no events yet)",
			"snapshot · Ctrl-C to exit",
		)
	})

	It("renders a populated snapshot with goal, budget, tree, frontier, activity, and events", func() {
		snap := populatedDashboardSnapshot()

		var buf bytes.Buffer
		renderDashboard(&buf, snap, 120, "refreshing every 2s", nil)
		expectText(buf.String(),
			"Goal: decrease qemu_cycles on dsp_fir",
			"Completion: threshold=0.2 -> ask_human",
			"size_flash ≤ 65536",
			"host_test require=pass",
			"5/20 experiments",
			"1.2h/8h elapsed",
			"stalled 2/5",
			"3 hypotheses · 5 experiments · 12 observations · 2 conclusions",
			"H-0001",
			"H-0003",
			"C-0001  H-0001  qemu_cycles=750067",
			"(stalled_for: 2 of 5)",
			"E-0007",
			"instruments=qemu_cycles,host_test",
			"hypothesis.add",
			"experiment.design",
			"refreshing every 2s · Ctrl-C to exit",
		)
	})

	It("renders pause state and dirty main checkout warnings", func() {
		paused := baseSnapshot()
		paused.Paused = true
		paused.PauseReason = "human review of E-0005"
		var pausedBuf bytes.Buffer
		renderDashboard(&pausedBuf, paused, 120, "snapshot", nil)
		expectText(pausedBuf.String(), "[PAUSED: human review of E-0005]")

		dirty := baseSnapshot()
		dirty.MainCheckoutDirty = true
		dirty.MainCheckoutDirtyPaths = []string{"bootstrap.sh", "scripts/measure.sh"}
		var dirtyBuf bytes.Buffer
		renderDashboard(&dirtyBuf, dirty, 120, "snapshot", nil)
		expectText(dirtyBuf.String(),
			"Main checkout",
			"WARNING: dirty outside autoresearch-managed files",
			"research should keep experiment edits in worktrees",
			"bootstrap.sh",
			"scripts/measure.sh",
		)
	})

	It("renders hypothesis status glyphs in the tree", func() {
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
		expectText(buf.String(), "✓", "✗", "?", "☠", "•")
	})

	It("uses hypothesis status rather than experiment classification for frontier markers", func() {
		snap := baseSnapshot()
		snap.Goal = &entity.Goal{
			Objective: entity.Objective{Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease"},
		}
		snap.Frontier = []frontierRow{{
			Conclusion: "C-0001", Hypothesis: "H-0001", Value: 750067, DeltaFrac: -0.25,
			Classification: experimentClassificationDead, HypothesisStatus: entity.StatusSupported,
		}}

		var buf bytes.Buffer
		renderDashboard(&buf, snap, 120, "snapshot", nil)
		out := buf.String()
		expectText(out, "[supported]")
		expectNoText(out, "[dead]")
	})

	It("wraps colored dashboard output without changing visible content", func() {
		snap := baseSnapshot()
		flash := 65536.0
		snap.Goal = &entity.Goal{
			Objective:   entity.Objective{Instrument: "qemu_cycles", Target: "dsp_fir", Direction: "decrease"},
			Completion:  &entity.Completion{Threshold: 0.2, OnThreshold: entity.GoalOnThresholdAskHuman},
			Constraints: []entity.Constraint{{Instrument: "size_flash", Max: &flash}},
		}
		snap.Counts = map[string]int{"hypotheses": 1, "experiments": 1, "observations": 0, "conclusions": 0}
		snap.Budgets.Limits.MaxExperiments = 20
		snap.Budgets.Usage.Experiments = 18
		snap.Tree = []*treeNode{{ID: "H-0001", Claim: "supported hypo", Status: entity.StatusSupported}}
		snap.Frontier = []frontierRow{{Conclusion: "C-0001", Hypothesis: "H-0001", Value: 750067, DeltaFrac: -0.25}}
		snap.RecentEvents = []store.Event{
			{Ts: time.Date(2026, 4, 11, 18, 42, 1, 0, time.UTC), Kind: "hypothesis.add", Actor: "agent:generator", Subject: "H-0001"},
			{Ts: time.Date(2026, 4, 11, 18, 42, 5, 0, time.UTC), Kind: "experiment.design", Actor: "agent:designer", Subject: "E-0001"},
		}

		var colored bytes.Buffer
		renderDashboard(&colored, snap, 100, "snapshot", &ansi{enabled: true})
		out := colored.String()
		expectText(out, "\x1b[", "\x1b[32m✓\x1b[0m", "\x1b[31m18/20 experiments\x1b[0m")

		var plain bytes.Buffer
		renderDashboard(&plain, snap, 100, "snapshot", nil)
		Expect(stripANSI(out)).To(Equal(plain.String()))
	})

	It("does not emit ANSI escapes when the colorizer is nil or disabled", func() {
		snap := baseSnapshot()
		snap.Tree = []*treeNode{{ID: "H-0001", Claim: "hypo", Status: entity.StatusSupported}}

		for _, a := range []*ansi{nil, {}} {
			var buf bytes.Buffer
			renderDashboard(&buf, snap, 80, "snapshot", a)
			Expect(buf.String()).NotTo(ContainSubstring("\x1b["))
		}
	})
})

var _ = Describe("dashboard recent events", func() {
	It("pages newest-first and reports when all events have been loaded", func() {
		s := newStoreWithBuiltins()
		base := time.Date(2026, 4, 12, 9, 0, 0, 0, time.UTC)
		for i := 0; i < 12; i++ {
			Expect(s.AppendEvent(store.Event{
				Ts:      base.Add(time.Duration(i) * time.Second),
				Kind:    "observation.record",
				Subject: fmt.Sprintf("O-%04d", i),
			})).To(Succeed())
		}

		all, err := s.Events(0)
		Expect(err).NotTo(HaveOccurred())

		first, allLoaded := readDashboardRecentEvents(all, 0, 5)
		Expect(allLoaded).To(BeFalse())
		Expect(first).To(HaveLen(5))
		Expect(first[0].Subject).To(Equal("O-0011"))
		Expect(first[4].Subject).To(Equal("O-0007"))

		second, allLoaded := readDashboardRecentEvents(all, 5, 5)
		Expect(allLoaded).To(BeFalse())
		Expect(second[0].Subject).To(Equal("O-0006"))
		Expect(second[4].Subject).To(Equal("O-0002"))

		last, allLoaded := readDashboardRecentEvents(all, 10, 5)
		Expect(allLoaded).To(BeTrue())
		Expect(last).To(HaveLen(2))
		Expect(last[0].Subject).To(Equal("O-0001"))
		Expect(last[1].Subject).To(Equal("O-0000"))
	})
})

var _ = Describe("formatElapsed", func() {
	DescribeTable("renders minute-second durations",
		func(d time.Duration, want string) {
			Expect(formatElapsed(d)).To(Equal(want))
		},
		Entry("zero", time.Duration(0), "00:00"),
		Entry("sub-minute", 45*time.Second, "00:45"),
		Entry("minutes and seconds", 2*time.Minute+14*time.Second, "02:14"),
		Entry("over an hour", 65*time.Minute, "65:00"),
	)
})
