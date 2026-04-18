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
