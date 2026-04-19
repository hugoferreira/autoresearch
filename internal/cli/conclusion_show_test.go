package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

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
	if err := s.WriteObservation(&entity.Observation{
		ID:         "O-0003",
		Experiment: "E-0001",
		Instrument: "timing",
		MeasuredAt: time.Now().UTC(),
		Value:      102,
		Unit:       "ns",
		Samples:    1,
		Artifacts: []entity.Artifact{{
			Name:  "evidence/mechanism",
			SHA:   "def456",
			Path:  "artifacts/de/f456/mechanism.txt",
			Bytes: 64,
			Mime:  "text/plain",
		}},
		EvidenceFailures: []entity.EvidenceFailure{{
			Name:     "profile",
			ExitCode: 7,
			Error:    "tool crashed",
		}},
		Command: "echo cycles: 102",
		Author:  "test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteConclusion(&entity.Conclusion{
		ID:           "C-0001",
		Hypothesis:   "H-0001",
		Verdict:      entity.VerdictSupported,
		Observations: []string{"O-0001", "O-0002", "O-0003", "O-9999"},
		CandidateExp: "E-0001",
		CandidateRef: "refs/heads/candidate/E-0001-a1",
		CandidateSHA: "0123456789abcdef0123456789abcdef01234567",
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

	got := runCLIJSON[conclusionShowJSON](t, dir, "conclusion", "show", "C-0001")
	if got.Conclusion == nil {
		t.Fatal("decoded conclusion is nil")
	}
	if got.ID != "C-0001" {
		t.Fatalf("id = %q, want C-0001", got.ID)
	}
	if got.CandidateRef != "refs/heads/candidate/E-0001-a1" {
		t.Fatalf("candidate_ref = %q", got.CandidateRef)
	}
	if got.CandidateSHA != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("candidate_sha = %q", got.CandidateSHA)
	}
	if len(got.Observations) != 4 {
		t.Fatalf("observations len = %d, want 4", len(got.Observations))
	}
	if gotArts, ok := got.ObservationArtifacts["O-0001"]; !ok || len(gotArts) != 1 {
		t.Fatalf("joined artifacts for O-0001 = %+v", got.ObservationArtifacts["O-0001"])
	}
	if gotArts, ok := got.ObservationArtifacts["O-0002"]; !ok || len(gotArts) != 0 {
		t.Fatalf("joined artifacts for O-0002 = %+v", got.ObservationArtifacts["O-0002"])
	}
	if gotArts, ok := got.ObservationArtifacts["O-0003"]; !ok || len(gotArts) != 1 {
		t.Fatalf("joined artifacts for O-0003 = %+v", got.ObservationArtifacts["O-0003"])
	}
	if gotFailures, ok := got.ObservationEvidenceFailures["O-0003"]; !ok || len(gotFailures) != 1 {
		t.Fatalf("joined evidence failures for O-0003 = %+v", got.ObservationEvidenceFailures["O-0003"])
	}
	if gotFailures := got.ObservationEvidenceFailures["O-0003"][0]; gotFailures.Name != "profile" || gotFailures.ExitCode != 7 {
		t.Fatalf("unexpected evidence failure for O-0003: %+v", gotFailures)
	}
	if got.ObservationReadIssues["O-9999"] != "observation not found" {
		t.Fatalf("missing observation read issue = %q, want %q", got.ObservationReadIssues["O-9999"], "observation not found")
	}
	if _, ok := got.ObservationReadIssues["O-0001"]; ok {
		t.Fatalf("read issue unexpectedly recorded for readable observation: %+v", got.ObservationReadIssues)
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
	if _, ok := payload["observation_evidence_failures"]; ok {
		t.Fatalf("unexpected observation_evidence_failures field: %+v", payload)
	}
	if _, ok := payload["observation_read_issues"]; ok {
		t.Fatalf("unexpected observation_read_issues field: %+v", payload)
	}
}
