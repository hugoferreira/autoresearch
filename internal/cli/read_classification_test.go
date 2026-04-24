package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
)

var _ = testkit.Spec("TestListExperimentsForHypothesisForRead_AnnotatesResults", func(t testkit.T) {
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

	for _, e := range []*entity.Experiment{
		{ID: "E-0001", Hypothesis: "H-0001", Status: entity.ExpMeasured, CreatedAt: now},
		{ID: "E-0002", Hypothesis: "H-0002", Status: entity.ExpMeasured, CreatedAt: now},
	} {
		if err := s.WriteExperiment(e); err != nil {
			t.Fatal(err)
		}
	}

	views, err := listExperimentsForHypothesisForRead(s, "H-0001")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(views), 1; got != want {
		t.Fatalf("len(views) = %d, want %d", got, want)
	}
	if got, want := views[0].ID, "E-0001"; got != want {
		t.Fatalf("views[0].ID = %q, want %q", got, want)
	}
	if got, want := views[0].Classification, experimentClassificationDead; got != want {
		t.Fatalf("views[0].Classification = %q, want %q", got, want)
	}
	if got, want := views[0].HypothesisStatus, entity.StatusSupported; got != want {
		t.Fatalf("views[0].HypothesisStatus = %q, want %q", got, want)
	}
})

var _ = testkit.Spec("TestExperimentClassificationHelpers", func(t testkit.T) {
	if err := validateExperimentClassificationFilter(""); err != nil {
		t.Fatalf("validate empty: %v", err)
	}
	if err := validateExperimentClassificationFilter(experimentClassificationLive); err != nil {
		t.Fatalf("validate live: %v", err)
	}
	if err := validateExperimentClassificationFilter(experimentClassificationDead); err != nil {
		t.Fatalf("validate dead: %v", err)
	}
	if err := validateExperimentClassificationFilter("zombie"); err == nil {
		t.Fatal("expected invalid classification to be rejected")
	}

	if got, want := experimentClassificationSummary("", ""), experimentClassificationLive; got != want {
		t.Fatalf("summary(empty) = %q, want %q", got, want)
	}
	if got, want := experimentClassificationSummary(experimentClassificationDead, entity.StatusSupported), "dead (hypothesis=supported)"; got != want {
		t.Fatalf("summary(dead) = %q, want %q", got, want)
	}
	if got, want := experimentClassificationMarker(experimentClassificationDead), "[dead]"; got != want {
		t.Fatalf("marker(dead) = %q, want %q", got, want)
	}
	if got := experimentClassificationMarker(experimentClassificationLive); got != "" {
		t.Fatalf("marker(live) = %q, want empty", got)
	}
	if got, want := frontierClassificationMarker(experimentClassificationDead, entity.StatusSupported), "[supported]"; got != want {
		t.Fatalf("frontier marker(supported) = %q, want %q", got, want)
	}
	if got, want := frontierClassificationMarker(experimentClassificationDead, entity.StatusRefuted), "[refuted]"; got != want {
		t.Fatalf("frontier marker(refuted) = %q, want %q", got, want)
	}
	if got, want := frontierClassificationMarker(experimentClassificationDead, entity.StatusKilled), "[killed]"; got != want {
		t.Fatalf("frontier marker(killed) = %q, want %q", got, want)
	}
	if got, want := frontierClassificationMarker(experimentClassificationDead, entity.StatusUnreviewed), "[pending review]"; got != want {
		t.Fatalf("frontier marker(unreviewed) = %q, want %q", got, want)
	}
	if got, want := frontierClassificationMarker(experimentClassificationDead, ""), "[dead]"; got != want {
		t.Fatalf("frontier marker(fallback) = %q, want %q", got, want)
	}
	if got := frontierClassificationMarker(experimentClassificationLive, entity.StatusSupported); got != "" {
		t.Fatalf("frontier marker(live) = %q, want empty", got)
	}
})
