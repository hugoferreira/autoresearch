package entity_test

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("TestExperimentRoundTrip", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		e := &entity.Experiment{
			ID:         "E-0001",
			GoalID:     "G-0001",
			Hypothesis: "H-0001",
			Status:     entity.ExpDesigned,
			Baseline: entity.Baseline{
				Ref: "HEAD",
				SHA: "abcdef1234567890",
			},
			Instruments: []string{"host_compile", "host_test", "host_timing"},
			Attempt:     2,
			Budget: entity.Budget{
				WallTimeS:  600,
				MaxSamples: 30,
			},
			Author:                 "agent:designer",
			CreatedAt:              time.Date(2026, 4, 11, 14, 0, 0, 0, time.UTC),
			Body:                   "# Plan\n\nUnroll the inner loop.\n",
			ReferencedAsBaselineBy: []string{"C-0002", "C-0005"},
		}
		data, err := e.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		back, err := entity.ParseExperiment(data)
		if err != nil {
			t.Fatal(err)
		}
		if back.ID != "E-0001" || back.Hypothesis != "H-0001" {
			t.Errorf("round trip mismatch: %+v", back)
		}
		if back.GoalID != "G-0001" {
			t.Errorf("goal_id round-trip: got %q, want G-0001", back.GoalID)
		}
		if len(back.Instruments) != 3 {
			t.Errorf("instruments: got %d, want 3", len(back.Instruments))
		}
		if back.Baseline.SHA != "abcdef1234567890" {
			t.Errorf("baseline SHA: %q", back.Baseline.SHA)
		}
		if back.Budget.MaxSamples != 30 {
			t.Errorf("budget max_samples: %d", back.Budget.MaxSamples)
		}
		if back.Attempt != 2 {
			t.Errorf("attempt round-trip: got %d, want 2", back.Attempt)
		}
		if back.Body != e.Body {
			t.Errorf("body round-trip:\n want: %q\n  got: %q", e.Body, back.Body)
		}
		if len(back.ReferencedAsBaselineBy) != 2 ||
			back.ReferencedAsBaselineBy[0] != "C-0002" ||
			back.ReferencedAsBaselineBy[1] != "C-0005" {
			t.Errorf("referenced_as_baseline_by round-trip: got %+v", back.ReferencedAsBaselineBy)
		}
	})
})
