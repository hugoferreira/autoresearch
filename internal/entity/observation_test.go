package entity_test

import (
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
)

func TestObservationRoundTrip(t *testing.T) {
	pass := true
	low := 0.0045
	high := 0.0051
	o := &entity.Observation{
		ID:         "O-0001",
		Experiment: "E-0001",
		Instrument: "host_timing",
		MeasuredAt: time.Date(2026, 4, 11, 14, 0, 0, 0, time.UTC),
		Value:      0.0048,
		Unit:       "seconds",
		Samples:    10,
		PerSample:  []float64{0.0045, 0.0046, 0.0050, 0.0047, 0.0048, 0.0049, 0.0051, 0.0046, 0.0047, 0.0051},
		CILow:      &low,
		CIHigh:     &high,
		CIMethod:   "bootstrap_bca_95",
		Pass:       &pass,
		Artifacts: []entity.Artifact{
			{Name: "timing", SHA: "abcd1234", Path: "artifacts/ab/cd/timing.json", Bytes: 480},
		},
		Command:  "./a.out",
		ExitCode: 0,
		Author:   "agent:observer",
	}
	data, err := o.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := entity.ParseObservation(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Value != 0.0048 || back.Samples != 10 {
		t.Errorf("round trip: %+v", back)
	}
	if back.CILow == nil || *back.CILow != 0.0045 {
		t.Errorf("ci_low round trip: %v", back.CILow)
	}
	if len(back.Artifacts) != 1 || back.Artifacts[0].Name != "timing" {
		t.Errorf("artifacts round trip: %+v", back.Artifacts)
	}
	// Legacy pointers must be kept in sync by Normalize.
	if back.RawSHA != "abcd1234" || back.RawArtifact != "artifacts/ab/cd/timing.json" {
		t.Errorf("legacy fields: sha=%q path=%q", back.RawSHA, back.RawArtifact)
	}
}

func TestObservationMultipleArtifacts(t *testing.T) {
	o := &entity.Observation{
		ID:         "O-0005",
		Experiment: "E-0002",
		Instrument: "objdump",
		Value:      1247,
		Unit:       "instructions",
		Samples:    1,
		Artifacts: []entity.Artifact{
			{Name: "disasm", SHA: "aaaa", Path: "artifacts/aa/aa/disasm.txt", Bytes: 18234112},
			{Name: "symbols", SHA: "bbbb", Path: "artifacts/bb/bb/symbols.txt", Bytes: 42310},
			{Name: "sections", SHA: "cccc", Path: "artifacts/cc/cc/sections.txt", Bytes: 1120},
		},
	}
	data, err := o.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := entity.ParseObservation(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Artifacts) != 3 {
		t.Fatalf("artifacts count: %d", len(back.Artifacts))
	}
	if p := back.Primary(); p == nil || p.Name != "disasm" {
		t.Errorf("primary: %+v", p)
	}
	if back.RawSHA != "aaaa" {
		t.Errorf("legacy sha tracks primary: got %q", back.RawSHA)
	}
}

func TestObservationLegacyBackfill(t *testing.T) {
	// An observation written by M5 (before the Artifacts field existed).
	legacy := []byte(`{
		"id": "O-0001",
		"experiment": "E-0001",
		"instrument": "host_timing",
		"measured_at": "2026-04-11T14:00:00Z",
		"value": 0.002,
		"unit": "seconds",
		"samples": 5,
		"raw_artifact": "artifacts/ab/cd/timing.json",
		"raw_sha": "abcd1234",
		"command": "./a.out",
		"exit_code": 0,
		"author": "agent:observer"
	}`)
	o, err := entity.ParseObservation(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Artifacts) != 1 {
		t.Fatalf("backfill: artifacts=%+v", o.Artifacts)
	}
	if o.Artifacts[0].Path != "artifacts/ab/cd/timing.json" || o.Artifacts[0].SHA != "abcd1234" {
		t.Errorf("backfilled artifact: %+v", o.Artifacts[0])
	}
}
