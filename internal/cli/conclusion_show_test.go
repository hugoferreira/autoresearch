package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

type conclusionShowTestResponse struct {
	ID                   string                       `json:"id"`
	Observations         []string                     `json:"observations"`
	ObservationArtifacts map[string][]entity.Artifact `json:"observation_artifacts,omitempty"`
}

func TestConclusionShowJSON_JoinsObservationArtifacts(t *testing.T) {
	saveGlobals(t)
	dir := t.TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.WriteObservation(&entity.Observation{
		ID:         "O-0001",
		Experiment: "E-0001",
		Instrument: "timing",
		MeasuredAt: time.Now().UTC(),
		Value:      100,
		Unit:       "ns",
		Samples:    1,
		Artifacts: []entity.Artifact{{
			Name:  "scalar",
			SHA:   "abc123",
			Path:  "artifacts/ab/c123/scalar.json",
			Bytes: 42,
			Mime:  "application/json",
		}},
		Command: "echo cycles: 100",
		Author:  "test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteObservation(&entity.Observation{
		ID:         "O-0002",
		Experiment: "E-0001",
		Instrument: "timing",
		MeasuredAt: time.Now().UTC(),
		Value:      101,
		Unit:       "ns",
		Samples:    1,
		Command:    "echo cycles: 101",
		Author:     "test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteConclusion(&entity.Conclusion{
		ID:           "C-0001",
		Hypothesis:   "H-0001",
		Verdict:      entity.VerdictSupported,
		Observations: []string{"O-0001", "O-0002", "O-9999"},
		CandidateExp: "E-0001",
		BaselineExp:  "E-0000",
		Effect: entity.Effect{
			Instrument: "timing",
			DeltaAbs:   -20,
			DeltaFrac:  -0.2,
			CILowAbs:   -25,
			CIHighAbs:  -15,
			CILowFrac:  -0.25,
			CIHighFrac: -0.15,
			PValue:     0.01,
			CIMethod:   "bootstrap_bca_95",
			NCandidate: 3,
			NBaseline:  3,
		},
		StatTest:  "mann_whitney_u",
		Strict:    entity.Strict{Passed: true},
		Author:    "agent:orchestrator",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	got := runCLIJSON[conclusionShowTestResponse](t, dir, "conclusion", "show", "C-0001")
	if got.ID != "C-0001" {
		t.Fatalf("id = %q, want C-0001", got.ID)
	}
	if len(got.Observations) != 3 {
		t.Fatalf("observations len = %d, want 3", len(got.Observations))
	}
	if gotArts, ok := got.ObservationArtifacts["O-0001"]; !ok || len(gotArts) != 1 {
		t.Fatalf("joined artifacts for O-0001 = %+v", got.ObservationArtifacts["O-0001"])
	}
	if gotArts, ok := got.ObservationArtifacts["O-0002"]; !ok || len(gotArts) != 0 {
		t.Fatalf("joined artifacts for O-0002 = %+v", got.ObservationArtifacts["O-0002"])
	}
	if _, ok := got.ObservationArtifacts["O-9999"]; ok {
		t.Fatalf("missing observation should not be joined: %+v", got.ObservationArtifacts)
	}
}

func TestConclusionShowJSON_OmitsObservationArtifactsWhenNoObservations(t *testing.T) {
	saveGlobals(t)
	dir := t.TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteConclusion(&entity.Conclusion{
		ID:         "C-0002",
		Hypothesis: "H-0002",
		Verdict:    entity.VerdictInconclusive,
		Effect: entity.Effect{
			Instrument: "timing",
			CIMethod:   "bootstrap_bca_95",
		},
		StatTest:  "mann_whitney_u",
		Strict:    entity.Strict{Passed: true},
		Author:    "agent:orchestrator",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	raw := runCLI(t, dir, "--json", "conclusion", "show", "C-0002")
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, raw)
	}
	if _, ok := payload["observation_artifacts"]; ok {
		t.Fatalf("unexpected observation_artifacts field: %+v", payload)
	}
}
