package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

type observeRecordJSON struct {
	Action       string               `json:"action"`
	ID           string               `json:"id"`
	IDs          []string             `json:"ids"`
	SamplesAdded int                  `json:"samples_added"`
	Observation  entity.Observation   `json:"observation"`
	Observations []entity.Observation `json:"observations"`
}

type observeCheckJSON struct {
	Check observeSampleCheck `json:"check"`
}

func setupObserveFixture(t *testing.T) (string, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "timing.txt"), []byte("100\n"), 0o644); err != nil {
		t.Fatalf("write timing.txt: %v", err)
	}

	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
		Instruments: map[string]store.Instrument{
			"timing": {
				Cmd:        []string{"sh", "-c", "cat timing.txt"},
				Parser:     "builtin:scalar",
				Pattern:    "([0-9]+)",
				Unit:       "ns",
				MinSamples: 5,
			},
		},
	})
	if err != nil {
		t.Fatalf("store.Create: %v", err)
	}

	now := time.Now().UTC()
	exp := &entity.Experiment{
		ID:         "E-0001",
		GoalID:     "G-0001",
		Hypothesis: "H-0001",
		Status:     entity.ExpImplemented,
		Baseline:   entity.Baseline{Ref: "HEAD"},
		Instruments: []string{
			"timing",
		},
		Worktree:  dir,
		Author:    "human:test",
		CreatedAt: now,
	}
	if err := s.WriteExperiment(exp); err != nil {
		t.Fatalf("WriteExperiment: %v", err)
	}
	if err := s.UpdateState(func(st *store.State) error {
		st.Counters["E"] = 1
		return nil
	}); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	return dir, s
}

func TestObserveSkipsWhenSamplesAlreadySatisfied(t *testing.T) {
	saveGlobals(t)
	dir, s := setupObserveFixture(t)

	first := runCLIJSON[observeRecordJSON](t, dir, "observe", "E-0001", "--instrument", "timing")
	if got, want := first.Observation.Samples, 5; got != want {
		t.Fatalf("first observe samples = %d, want %d", got, want)
	}

	out := runCLI(t, dir, "observe", "E-0001", "--instrument", "timing")
	if !strings.Contains(out, "observation already satisfied") {
		t.Fatalf("skip output missing satisfied message:\n%s", out)
	}
	if !strings.Contains(out, "have 5 samples") {
		t.Fatalf("skip output missing sample count:\n%s", out)
	}
	if !strings.Contains(out, "--append") {
		t.Fatalf("skip output missing append hint:\n%s", out)
	}

	obs, err := s.ListObservationsForExperiment("E-0001")
	if err != nil {
		t.Fatalf("ListObservationsForExperiment: %v", err)
	}
	if got, want := len(obs), 1; got != want {
		t.Fatalf("observation count after skip = %d, want %d", got, want)
	}
	if got, want := samplesForObservedInstrument(obs, "timing"), 5; got != want {
		t.Fatalf("sample total after skip = %d, want %d", got, want)
	}
}

func TestObserveTopsUpToRequestedTotal(t *testing.T) {
	saveGlobals(t)
	dir, s := setupObserveFixture(t)

	runCLIJSON[observeRecordJSON](t, dir, "observe", "E-0001", "--instrument", "timing")
	resp := runCLIJSON[observeRecordJSON](t, dir, "observe", "E-0001", "--instrument", "timing", "--samples", "7")

	if got, want := resp.Action, "recorded"; got != want {
		t.Fatalf("action = %q, want %q", got, want)
	}
	if got, want := resp.SamplesAdded, 2; got != want {
		t.Fatalf("samples_added = %d, want %d", got, want)
	}
	if got, want := resp.Observation.Samples, 2; got != want {
		t.Fatalf("latest observation samples = %d, want %d", got, want)
	}
	if got, want := len(resp.Observations), 1; got != want {
		t.Fatalf("new observation count = %d, want %d", got, want)
	}

	obs, err := s.ListObservationsForExperiment("E-0001")
	if err != nil {
		t.Fatalf("ListObservationsForExperiment: %v", err)
	}
	if got, want := len(obs), 2; got != want {
		t.Fatalf("observation count after top-up = %d, want %d", got, want)
	}
	if got, want := samplesForObservedInstrument(obs, "timing"), 7; got != want {
		t.Fatalf("sample total after top-up = %d, want %d", got, want)
	}

	exp, err := s.ReadExperiment("E-0001")
	if err != nil {
		t.Fatalf("ReadExperiment: %v", err)
	}
	if got, want := exp.Status, entity.ExpMeasured; got != want {
		t.Fatalf("experiment status = %q, want %q", got, want)
	}
}

func TestObserveAppendPreservesAnotherFullRun(t *testing.T) {
	saveGlobals(t)
	dir, s := setupObserveFixture(t)

	runCLIJSON[observeRecordJSON](t, dir, "observe", "E-0001", "--instrument", "timing")
	resp := runCLIJSON[observeRecordJSON](t, dir, "observe", "E-0001", "--instrument", "timing", "--append")

	if got, want := resp.SamplesAdded, 5; got != want {
		t.Fatalf("samples_added = %d, want %d", got, want)
	}
	if got, want := resp.Observation.Samples, 5; got != want {
		t.Fatalf("latest observation samples = %d, want %d", got, want)
	}

	obs, err := s.ListObservationsForExperiment("E-0001")
	if err != nil {
		t.Fatalf("ListObservationsForExperiment: %v", err)
	}
	if got, want := len(obs), 2; got != want {
		t.Fatalf("observation count after append = %d, want %d", got, want)
	}
	if got, want := samplesForObservedInstrument(obs, "timing"), 10; got != want {
		t.Fatalf("sample total after append = %d, want %d", got, want)
	}
}

func TestObserveCheckReportsCurrentAndNeededSamples(t *testing.T) {
	saveGlobals(t)
	dir, _ := setupObserveFixture(t)

	runCLIJSON[observeRecordJSON](t, dir, "observe", "E-0001", "--instrument", "timing")
	resp := runCLIJSON[observeCheckJSON](t, dir, "observe", "check", "E-0001", "--instrument", "timing", "--samples", "7")

	if got, want := resp.Check.CurrentSamples, 5; got != want {
		t.Fatalf("current_samples = %d, want %d", got, want)
	}
	if got, want := resp.Check.MinSamples, 5; got != want {
		t.Fatalf("min_samples = %d, want %d", got, want)
	}
	if !resp.Check.MinSatisfied {
		t.Fatal("min_satisfied = false, want true")
	}
	if got, want := resp.Check.TargetSamples, 7; got != want {
		t.Fatalf("target_samples = %d, want %d", got, want)
	}
	if got, want := resp.Check.TargetSource, "requested"; got != want {
		t.Fatalf("target_source = %q, want %q", got, want)
	}
	if resp.Check.TargetSatisfied {
		t.Fatal("target_satisfied = true, want false")
	}
	if got, want := resp.Check.AdditionalSamples, 2; got != want {
		t.Fatalf("additional_samples = %d, want %d", got, want)
	}
}
