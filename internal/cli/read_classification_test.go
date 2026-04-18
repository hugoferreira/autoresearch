package cli

import (
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

func TestAnnotateExperimentsForRead_UsesParentHypothesisStatus(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	for _, h := range []*entity.Hypothesis{
		{
			ID: "H-0001", Claim: "done", Status: entity.StatusSupported,
			Predicts: entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease"},
			KillIf:   []string{"tests fail"}, Author: "human", CreatedAt: now,
		},
		{
			ID: "H-0002", Claim: "live", Status: entity.StatusOpen,
			Predicts: entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease"},
			KillIf:   []string{"tests fail"}, Author: "human", CreatedAt: now,
		},
	} {
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}
	}

	views, err := annotateExperimentsForRead(s, []*entity.Experiment{
		{ID: "E-0001", Hypothesis: "H-0001"},
		{ID: "E-0002", Hypothesis: "H-0002"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := views[0].Classification, experimentClassificationDead; got != want {
		t.Fatalf("E-0001 classification = %q, want %q", got, want)
	}
	if got, want := views[0].HypothesisStatus, entity.StatusSupported; got != want {
		t.Fatalf("E-0001 hypothesis_status = %q, want %q", got, want)
	}
	if got, want := views[1].Classification, experimentClassificationLive; got != want {
		t.Fatalf("E-0002 classification = %q, want %q", got, want)
	}
}

func TestClassifyHypothesisStatusForExperimentRead(t *testing.T) {
	cases := []struct {
		name       string
		status     string
		class      string
		actionable bool
		wantStatus string
	}{
		{name: "missing defaults live", status: "", class: experimentClassificationLive, actionable: true, wantStatus: ""},
		{name: "open stays live", status: entity.StatusOpen, class: experimentClassificationLive, actionable: true, wantStatus: ""},
		{name: "inconclusive stays live", status: entity.StatusInconclusive, class: experimentClassificationLive, actionable: true, wantStatus: ""},
		{name: "unreviewed is dead", status: entity.StatusUnreviewed, class: experimentClassificationDead, actionable: false, wantStatus: entity.StatusUnreviewed},
		{name: "supported is dead", status: entity.StatusSupported, class: experimentClassificationDead, actionable: false, wantStatus: entity.StatusSupported},
		{name: "refuted is dead", status: entity.StatusRefuted, class: experimentClassificationDead, actionable: false, wantStatus: entity.StatusRefuted},
		{name: "killed is dead", status: entity.StatusKilled, class: experimentClassificationDead, actionable: false, wantStatus: entity.StatusKilled},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyHypothesisStatusForExperimentRead(tc.status)
			if got.Classification != tc.class {
				t.Fatalf("classification = %q, want %q", got.Classification, tc.class)
			}
			if got.HypothesisStatus != tc.wantStatus {
				t.Fatalf("hypothesis_status = %q, want %q", got.HypothesisStatus, tc.wantStatus)
			}
			if got.LoopActionable() != tc.actionable {
				t.Fatalf("loopActionable = %v, want %v", got.LoopActionable(), tc.actionable)
			}
		})
	}
}
