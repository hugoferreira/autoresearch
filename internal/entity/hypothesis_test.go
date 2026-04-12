package entity_test

import (
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
)

func TestHypothesisRoundTrip(t *testing.T) {
	h := &entity.Hypothesis{
		ID:     "H-0001",
		Parent: "",
		Claim:  "unrolling dsp_fir by 4 reduces cycles >10%",
		Predicts: entity.Predicts{
			Instrument: "qemu_cycles",
			Target:     "dsp_fir_bench",
			Direction:  "decrease",
			MinEffect:  0.10,
		},
		KillIf:    []string{"flash delta > 1024 bytes", "CI crosses zero"},
		Status:    entity.StatusOpen,
		Author:    "human:alice",
		CreatedAt: time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC),
		Tags:      []string{"perf", "unroll"},
		Body:      "# Notes\n\nIdea from the CMSIS reference.\n",
	}
	data, err := h.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := entity.ParseHypothesis(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != h.ID || back.Claim != h.Claim || back.Predicts.MinEffect != 0.10 {
		t.Errorf("round trip mismatch: %+v", back)
	}
	if len(back.KillIf) != 2 {
		t.Errorf("kill_if count: got %d, want 2", len(back.KillIf))
	}
	if back.Predicts.Instrument != "qemu_cycles" {
		t.Errorf("predicts instrument: %q", back.Predicts.Instrument)
	}
	if back.Body != h.Body {
		t.Errorf("body round-trip:\n want: %q\n  got: %q", h.Body, back.Body)
	}
}
