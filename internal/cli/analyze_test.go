package cli

import (
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
)

var _ = testkit.Spec("TestAnalyzeCandidateRefUsesStoredRefAfterDeletion", func(t testkit.T) {
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
})

var _ = testkit.Spec("TestAnalyzeRejectsBaselineExperimentWithMultipleScopes", func(t testkit.T) {
	saveGlobals(t)

	dir, baselineID := setupAnalyzeAmbiguousBaseline(t)

	_, _, err := runCLIResult(t, dir, "analyze", baselineID)
	if err == nil {
		t.Fatal("analyze baseline unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "experiment "+baselineID+" has observations for multiple recorded scopes") {
		t.Fatalf("error = %q, want multiple recorded scopes for %s", err, baselineID)
	}
})

var _ = testkit.Spec("TestAnalyzeRejectsAmbiguousBaselineArgument", func(t testkit.T) {
	saveGlobals(t)

	dir, baselineID := setupAnalyzeAmbiguousBaseline(t)
	hyp := runCLIJSON[cliIDResponse](t, dir,
		"hypothesis", "add",
		"--claim", "tighten the hot loop",
		"--predicts-instrument", "timing",
		"--predicts-target", "kernel",
		"--predicts-direction", "decrease",
		"--predicts-min-effect", "0.1",
		"--kill-if", "tests fail",
	)
	exp := runCLIJSON[cliIDResponse](t, dir,
		"experiment", "design", hyp.ID,
		"--baseline", "HEAD",
		"--instruments", "timing",
	)
	impl := runCLIJSON[cliImplementResponse](t, dir, "experiment", "implement", exp.ID)

	writeScenarioMetrics(t, impl.Worktree, "90\n", "900\n")
	gitCommitAll(t, impl.Worktree, "candidate a")
	candidateRef := gitCreateCandidateRef(t, impl.Worktree, "candidate/analyze-ambiguous-baseline")
	runCLIJSON[observeRecordJSON](t, dir,
		"observe", exp.ID,
		"--instrument", "timing",
		"--candidate-ref", candidateRef,
	)

	_, _, err := runCLIResult(t, dir,
		"analyze", exp.ID,
		"--candidate-ref", candidateRef,
		"--baseline", baselineID,
	)
	if err == nil {
		t.Fatal("analyze with ambiguous baseline unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "baseline experiment "+baselineID+" has observations for multiple recorded scopes") {
		t.Fatalf("error = %q, want ambiguous baseline scope for %s", err, baselineID)
	}
})

var _ = testkit.Spec("TestFilterAnalyzeObservationsByCandidateRef_RejectsMixedAttempts", func(t testkit.T) {
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
})

func setupAnalyzeAmbiguousBaseline(t testkit.T) (string, string) {
	t.Helper()

	dir := setupObserveScenarioStore(t)
	registerScenarioInstruments(t, dir)
	runCLIJSON[cliIDResponse](t, dir,
		"goal", "set",
		"--objective-instrument", "timing",
		"--objective-target", "kernel",
		"--objective-direction", "decrease",
		"--constraint-max", "binary_size=1000",
	)
	baseline := runCLIJSON[cliIDResponse](t, dir, "experiment", "baseline")
	addAnalyzeBaselineScope(t, dir, baseline.ID, 2, 95)
	return dir, baseline.ID
}

func addAnalyzeBaselineScope(t testkit.T, dir, baselineID string, attempt int, value float64) {
	t.Helper()

	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	exp, err := s.ReadExperiment(baselineID)
	if err != nil {
		t.Fatalf("ReadExperiment: %v", err)
	}
	id, err := s.AllocID(store.KindObservation)
	if err != nil {
		t.Fatalf("AllocID: %v", err)
	}
	if err := s.WriteObservation(&entity.Observation{
		ID:           id,
		Experiment:   baselineID,
		Instrument:   "timing",
		MeasuredAt:   time.Now().UTC(),
		Value:        value,
		Unit:         "ns",
		Samples:      1,
		Command:      "sh -c cat timing.txt",
		ExitCode:     0,
		Attempt:      attempt,
		CandidateSHA: exp.Baseline.SHA,
		Author:       "test",
	}); err != nil {
		t.Fatalf("WriteObservation: %v", err)
	}
}
