package cli

import (
	"strings"
	"testing"

	"github.com/bytter/autoresearch/internal/entity"
)

func TestAnalyzeCandidateRefUsesStoredRefAfterDeletion(t *testing.T) {
	saveGlobals(t)

	dir := setupObserveScenarioStore(t)
	registerScenarioInstruments(t, dir)
	scenario := setupObserveScenarioExperiment(t, dir, "timing", "--constraint-max", "binary_size=1000")

	writeScenarioMetrics(t, scenario.Worktree, "90\n", "900\n")
	gitCommitAll(t, scenario.Worktree, "candidate a")
	candidateRef := gitCreateCandidateRef(t, scenario.Worktree, "candidate/analyze-deleted-ref")
	runCLIJSON[observeRecordJSON](t, dir,
		"observe", scenario.ExpID,
		"--instrument", "timing",
		"--candidate-ref", candidateRef,
	)

	fullRef := "refs/heads/" + candidateRef
	gitRun(t, scenario.Worktree, "branch", "-D", candidateRef)

	resp := runCLIJSON[cliAnalyzeResponse](t, dir,
		"analyze", scenario.ExpID,
		"--candidate-ref", fullRef,
	)
	if got, want := len(resp.Rows), 1; got != want {
		t.Fatalf("rows len = %d, want %d", got, want)
	}
	if got, want := resp.Rows[0].Instrument, "timing"; got != want {
		t.Fatalf("instrument = %q, want %q", got, want)
	}
}

func TestFilterAnalyzeObservationsByCandidateRef_RejectsMixedAttempts(t *testing.T) {
	obs := []*entity.Observation{
		{
			ID:           "O-0001",
			Attempt:      1,
			CandidateRef: "refs/heads/candidate/E-0001-a1",
			CandidateSHA: "0123456789abcdef0123456789abcdef01234567",
		},
		{
			ID:           "O-0002",
			Attempt:      2,
			CandidateRef: "refs/heads/candidate/E-0001-a1",
			CandidateSHA: "0123456789abcdef0123456789abcdef01234567",
		},
	}

	_, err := filterAnalyzeObservationsByCandidateRef(obs, "refs/heads/candidate/E-0001-a1")
	if err == nil {
		t.Fatal("filterAnalyzeObservationsByCandidateRef unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "multiple recorded candidate scopes") {
		t.Fatalf("error = %q, want multiple recorded candidate scopes", err)
	}
}
