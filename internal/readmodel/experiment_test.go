package readmodel

import (
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

func TestClassifyHypothesisStatusForExperimentRead(t *testing.T) {
	cases := []struct {
		name       string
		status     string
		class      string
		actionable bool
		wantStatus string
	}{
		{name: "missing defaults live", status: "", class: ExperimentClassificationLive, actionable: true, wantStatus: ""},
		{name: "open stays live", status: entity.StatusOpen, class: ExperimentClassificationLive, actionable: true, wantStatus: ""},
		{name: "inconclusive stays live", status: entity.StatusInconclusive, class: ExperimentClassificationLive, actionable: true, wantStatus: ""},
		{name: "unreviewed is dead", status: entity.StatusUnreviewed, class: ExperimentClassificationDead, actionable: false, wantStatus: entity.StatusUnreviewed},
		{name: "supported is dead", status: entity.StatusSupported, class: ExperimentClassificationDead, actionable: false, wantStatus: entity.StatusSupported},
		{name: "refuted is dead", status: entity.StatusRefuted, class: ExperimentClassificationDead, actionable: false, wantStatus: entity.StatusRefuted},
		{name: "killed is dead", status: entity.StatusKilled, class: ExperimentClassificationDead, actionable: false, wantStatus: entity.StatusKilled},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyHypothesisStatusForExperimentRead(tc.status)
			if got.Classification != tc.class {
				t.Fatalf("classification = %q, want %q", got.Classification, tc.class)
			}
			if got.HypothesisStatus != tc.wantStatus {
				t.Fatalf("hypothesis_status = %q, want %q", got.HypothesisStatus, tc.wantStatus)
			}
			if got.LoopActionable() != tc.actionable {
				t.Fatalf("LoopActionable = %v, want %v", got.LoopActionable(), tc.actionable)
			}
		})
	}
}

func TestFindStaleExperimentsForRead_ExcludesNonActionableWork(t *testing.T) {
	now := time.Date(2026, 4, 18, 20, 0, 0, 0, time.UTC)
	exps := []*entity.Experiment{
		{ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpMeasured},
		{ID: "E-0002", Hypothesis: "H-0002", Status: entity.ExpMeasured},
		{ID: "E-0003", Hypothesis: "H-0003", Status: entity.ExpMeasured},
	}
	classByID := map[string]ExperimentReadClass{
		"E-0001": ClassifyHypothesisStatusForExperimentRead(entity.StatusSupported),
		"E-0002": ClassifyHypothesisStatusForExperimentRead(entity.StatusUnreviewed),
		"E-0003": ClassifyHypothesisStatusForExperimentRead(entity.StatusOpen),
	}
	events := []store.Event{
		{Ts: now.Add(-10 * time.Minute), Kind: "experiment.measure", Subject: "E-0001"},
		{Ts: now.Add(-10 * time.Minute), Kind: "experiment.measure", Subject: "E-0002"},
		{Ts: now.Add(-10 * time.Minute), Kind: "experiment.measure", Subject: "E-0003"},
	}

	stale := FindStaleExperimentsForRead(exps, classByID, events, 5*time.Minute, now)
	if got, want := len(stale), 1; got != want {
		t.Fatalf("stale len = %d, want %d", got, want)
	}
	if got, want := stale[0].ID, "E-0003"; got != want {
		t.Fatalf("stale[0].ID = %q, want %q", got, want)
	}
}

func TestBuildInFlightExperiments_FiltersAndSorts(t *testing.T) {
	now := time.Date(2026, 4, 18, 20, 0, 0, 0, time.UTC)
	exps := []*entity.Experiment{
		{
			ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpMeasured,
			Instruments:            []string{"host_timing"},
			ReferencedAsBaselineBy: []string{"C-0001"},
		},
		{
			ID: "E-0002", Hypothesis: "H-0002", Status: entity.ExpMeasured,
			Instruments: []string{"host_timing"},
		},
		{
			ID: "E-0003", Hypothesis: "H-0003", Status: entity.ExpImplemented,
			Instruments: []string{"host_timing"},
		},
		{
			ID: "E-0004", Hypothesis: "H-0004", Status: entity.ExpMeasured,
			Instruments: []string{"host_timing", "host_test"},
		},
		{
			ID: "E-0005", Hypothesis: "H-0005", Status: entity.ExpMeasured,
			Instruments: []string{"host_timing"},
		},
	}
	classByID := map[string]ExperimentReadClass{
		"E-0002": ClassifyHypothesisStatusForExperimentRead(entity.StatusSupported),
	}
	old := now.Add(-10 * time.Minute)
	recent := now.Add(-2 * time.Minute)
	events := []store.Event{
		{Ts: old, Kind: "experiment.implement", Subject: "E-0003"},
		{Ts: recent, Kind: "experiment.implement", Subject: "E-0004"},
	}

	inFlight := BuildInFlightExperiments(exps, classByID, events, now)
	if got, want := len(inFlight), 3; got != want {
		t.Fatalf("inFlight len = %d, want %d", got, want)
	}
	if got, want := inFlight[0].ID, "E-0004"; got != want {
		t.Fatalf("inFlight[0].ID = %q, want %q", got, want)
	}
	if got, want := inFlight[1].ID, "E-0003"; got != want {
		t.Fatalf("inFlight[1].ID = %q, want %q", got, want)
	}
	if got, want := inFlight[2].ID, "E-0005"; got != want {
		t.Fatalf("inFlight[2].ID = %q, want %q", got, want)
	}
	if got, want := inFlight[0].ElapsedS, 120.0; got != want {
		t.Fatalf("inFlight[0].ElapsedS = %v, want %v", got, want)
	}
	if got, want := inFlight[1].ElapsedS, 600.0; got != want {
		t.Fatalf("inFlight[1].ElapsedS = %v, want %v", got, want)
	}
	if inFlight[2].ImplementedAt != nil {
		t.Fatalf("inFlight[2].ImplementedAt = %v, want nil", inFlight[2].ImplementedAt)
	}
}

func TestBuildExperimentActivity_UsesSharedThresholdAndNow(t *testing.T) {
	now := time.Date(2026, 4, 18, 20, 0, 0, 0, time.UTC)
	exps := []*entity.Experiment{
		{ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpMeasured, Instruments: []string{"host_timing"}},
	}
	classByID := map[string]ExperimentReadClass{
		"E-0001": ClassifyHypothesisStatusForExperimentRead(entity.StatusOpen),
	}
	events := []store.Event{
		{Ts: now.Add(-10 * time.Minute), Kind: "experiment.implement", Subject: "E-0001"},
	}

	inFlight, stale := BuildExperimentActivity(exps, classByID, events, 5*time.Minute, now)
	if got, want := len(inFlight), 1; got != want {
		t.Fatalf("inFlight len = %d, want %d", got, want)
	}
	if got, want := len(stale), 1; got != want {
		t.Fatalf("stale len = %d, want %d", got, want)
	}
	if got, want := inFlight[0].ElapsedS, 600.0; got != want {
		t.Fatalf("inFlight[0].ElapsedS = %v, want %v", got, want)
	}
	if got, want := stale[0].StaleMinutes, 10.0; got != want {
		t.Fatalf("stale[0].StaleMinutes = %v, want %v", got, want)
	}
}
