package cli

import (
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
)

var _ = testkit.Spec("TestObserveAllEnforcesStrictMinSamples", func(t testkit.T) {
	saveGlobals(t)
	dir := setupObserveScenarioStore(t)
	runCLI(t, dir,
		"instrument", "register", "timing",
		"--cmd", "sh",
		"--cmd", "-c",
		"--cmd", "cat timing.txt",
		"--parser", "builtin:scalar",
		"--pattern", "([0-9]+)",
		"--unit", "ns",
		"--min-samples", "5",
	)
	scenario := setupObserveScenarioExperiment(t, dir, "timing", "--constraint-max", "timing=1000")
	writeScenarioMetrics(t, scenario.Worktree, "80\n", "900\n")
	gitCommitAll(t, scenario.Worktree, "candidate")
	candidateRef := gitCreateCandidateRef(t, scenario.Worktree, "candidate/observe-all-min-samples")

	stdout, stderr, err := runCLIResult(t, dir,
		"observe", scenario.ExpID,
		"--all",
		"--candidate-ref", candidateRef,
		"--samples", "1",
	)
	if err == nil {
		t.Fatalf("observe --all unexpectedly accepted --samples below min_samples\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(err.Error(), "requires at least 5 samples") {
		t.Fatalf("observe --all error = %v, want strict min_samples error\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
})

var _ = testkit.Spec("TestObserveAllRequiresImplementedExperimentStatus", func(t testkit.T) {
	saveGlobals(t)
	dir := setupObserveScenarioStore(t)
	registerScenarioTimingInstrument(t, dir)
	scenario := setupObserveScenarioExperiment(t, dir, "timing", "--constraint-max", "timing=1000")
	writeScenarioMetrics(t, scenario.Worktree, "80\n", "900\n")
	gitCommitAll(t, scenario.Worktree, "candidate")
	candidateRef := gitCreateCandidateRef(t, scenario.Worktree, "candidate/observe-all-status")

	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	exp, err := s.ReadExperiment(scenario.ExpID)
	if err != nil {
		t.Fatalf("ReadExperiment: %v", err)
	}
	exp.Status = entity.ExpDesigned
	if err := s.WriteExperiment(exp); err != nil {
		t.Fatalf("WriteExperiment: %v", err)
	}

	stdout, stderr, err := runCLIResult(t, dir,
		"observe", scenario.ExpID,
		"--all",
		"--candidate-ref", candidateRef,
	)
	if err == nil {
		t.Fatalf("observe --all unexpectedly accepted experiment in designed status\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(err.Error(), "status") || !strings.Contains(err.Error(), "implemented") {
		t.Fatalf("observe --all error = %v, want experiment status error\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
})
