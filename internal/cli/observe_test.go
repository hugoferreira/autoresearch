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

type observeScenarioExperiment struct {
	ExpID    string
	Worktree string
}

func timingSampleTotal(observations []*entity.Observation) int {
	return samplesForObservedInstrument(store.Instrument{Parser: "builtin:scalar"}, observations, "timing")
}

func setupObserveScenarioStore(t *testing.T) string {
	t.Helper()
	dir := gitInitScenarioRepo(t)
	if _, err := store.Create(dir, store.Config{
		Build:     store.CommandSpec{Command: "true"},
		Test:      store.CommandSpec{Command: "true"},
		Worktrees: store.WorktreesConfig{Root: filepath.Join(t.TempDir(), "worktrees")},
	}); err != nil {
		t.Fatalf("store.Create: %v", err)
	}
	return dir
}

func setupObserveScenarioExperiment(t *testing.T, dir, instruments string, goalArgs ...string) observeScenarioExperiment {
	t.Helper()

	args := []string{
		"goal", "set",
		"--objective-instrument", "timing",
		"--objective-target", "kernel",
		"--objective-direction", "decrease",
	}
	args = append(args, goalArgs...)
	runCLIJSON[cliIDResponse](t, dir, args...)

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
		"--instruments", instruments,
	)
	impl := runCLIJSON[cliImplementResponse](t, dir, "experiment", "implement", exp.ID)
	return observeScenarioExperiment{
		ExpID:    exp.ID,
		Worktree: impl.Worktree,
	}
}

func setupObserveFixture(t *testing.T) (string, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "timing.txt"), []byte("100\n"), 0o644); err != nil {
		t.Fatalf("write timing.txt: %v", err)
	}
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		gitRun(t, dir, args...)
	}
	gitRun(t, dir, "add", "timing.txt")
	gitRun(t, dir, "commit", "-m", "init")

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
		Attempt:    1,
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
	if got, want := timingSampleTotal(obs), 5; got != want {
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
	if got, want := timingSampleTotal(obs), 7; got != want {
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
	if got, want := timingSampleTotal(obs), 10; got != want {
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

func TestObserveCheckIgnoresObservationsFromResetAttempts(t *testing.T) {
	saveGlobals(t)
	dir := setupObserveScenarioStore(t)
	registerScenarioInstruments(t, dir)
	scenario := setupObserveScenarioExperiment(t, dir, "timing", "--constraint-max", "binary_size=1000")
	first := runCLIJSON[observeRecordJSON](t, dir,
		"observe", scenario.ExpID,
		"--instrument", "timing",
		"--allow-unchanged",
	)
	if first.ID == "" {
		t.Fatal("first observation id missing")
	}
	runCLI(t, dir, "experiment", "reset", scenario.ExpID, "--reason", "retry measurement")
	runCLIJSON[cliImplementResponse](t, dir, "experiment", "implement", scenario.ExpID)

	check := runCLIJSON[observeCheckJSON](t, dir,
		"observe", "check", scenario.ExpID,
		"--instrument", "timing",
	)
	if got, want := check.Check.CurrentSamples, 0; got != want {
		t.Fatalf("current_samples after reset = %d, want %d", got, want)
	}
	if check.Check.TargetSatisfied {
		t.Fatal("target_satisfied = true after reset, want false")
	}

	second := runCLIJSON[observeRecordJSON](t, dir,
		"observe", scenario.ExpID,
		"--instrument", "timing",
		"--allow-unchanged",
	)
	if second.ID == first.ID {
		t.Fatalf("reused stale observation id %q after reset", second.ID)
	}

	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	expEntity, err := s.ReadExperiment(scenario.ExpID)
	if err != nil {
		t.Fatalf("ReadExperiment: %v", err)
	}
	if got, want := expEntity.Attempt, 2; got != want {
		t.Fatalf("experiment attempt = %d, want %d", got, want)
	}
}

func TestObserveCheckIgnoresObservationsAfterCandidateCommitChanges(t *testing.T) {
	saveGlobals(t)
	dir := setupObserveScenarioStore(t)
	registerScenarioInstruments(t, dir)
	scenario := setupObserveScenarioExperiment(t, dir, "timing", "--constraint-max", "binary_size=1000")
	writeScenarioMetrics(t, scenario.Worktree, "90\n", "900\n")
	gitCommitAll(t, scenario.Worktree, "candidate a")
	runCLIJSON[observeRecordJSON](t, dir, "observe", scenario.ExpID, "--instrument", "timing")
	writeScenarioMetrics(t, scenario.Worktree, "85\n", "900\n")
	gitCommitAll(t, scenario.Worktree, "candidate b")

	check := runCLIJSON[observeCheckJSON](t, dir,
		"observe", "check", scenario.ExpID,
		"--instrument", "timing",
	)
	if got, want := check.Check.CurrentSamples, 0; got != want {
		t.Fatalf("current_samples after new commit = %d, want %d", got, want)
	}
	if check.Check.TargetSatisfied {
		t.Fatal("target_satisfied = true after new commit, want false")
	}
}

func TestObserveAllJSONReturnsCurrentObservationSetOnIdempotentRerun(t *testing.T) {
	saveGlobals(t)
	dir := setupObserveScenarioStore(t)
	registerScenarioInstruments(t, dir)
	scenario := setupObserveScenarioExperiment(t, dir, "timing,binary_size,host_test",
		"--constraint-max", "binary_size=1000",
		"--constraint-require", "host_test=pass",
	)
	writeScenarioMetrics(t, scenario.Worktree, "80\n", "900\n")
	gitCommitAll(t, scenario.Worktree, "candidate")

	first := runCLIJSON[cliObserveAllResponse](t, dir, "observe", scenario.ExpID, "--all")
	if got, want := len(first.Observations), 3; got != want {
		t.Fatalf("first observations len = %d, want %d", got, want)
	}
	if got, want := len(first.NewObservations), 3; got != want {
		t.Fatalf("first new_observations len = %d, want %d", got, want)
	}
	if got, want := len(first.ReusedObservations), 0; got != want {
		t.Fatalf("first reused_observations len = %d, want %d", got, want)
	}

	second := runCLIJSON[cliObserveAllResponse](t, dir, "observe", scenario.ExpID, "--all")
	if got, want := second.Action, observeActionSkipped; got != want {
		t.Fatalf("second action = %q, want %q", got, want)
	}
	if got, want := len(second.Observations), 3; got != want {
		t.Fatalf("second observations len = %d, want %d", got, want)
	}
	if got, want := len(second.NewObservations), 0; got != want {
		t.Fatalf("second new_observations len = %d, want %d", got, want)
	}
	if got, want := len(second.ReusedObservations), 3; got != want {
		t.Fatalf("second reused_observations len = %d, want %d", got, want)
	}
}

func TestObserveAllRerunsFailedPrerequisitesInsteadOfSkippingByCount(t *testing.T) {
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
		"--requires", "host_test=pass",
	)
	runCLI(t, dir,
		"instrument", "register", "host_test",
		"--cmd", "sh",
		"--cmd", "-c",
		"--cmd", "test -f PASS",
		"--parser", "builtin:passfail",
		"--unit", "bool",
	)
	scenario := setupObserveScenarioExperiment(t, dir, "timing,host_test", "--constraint-require", "host_test=pass")
	writeScenarioMetrics(t, scenario.Worktree, "80\n", "900\n")
	gitCommitAll(t, scenario.Worktree, "candidate")
	if err := os.Remove(filepath.Join(scenario.Worktree, "PASS")); err != nil {
		t.Fatalf("remove PASS: %v", err)
	}

	_, _, err := runCLIResult(t, dir, "observe", scenario.ExpID, "--all")
	if err == nil {
		t.Fatal("observe --all unexpectedly succeeded with failed prerequisite")
	}
	if !strings.Contains(err.Error(), "stuck: instruments [timing] have unsatisfied dependencies") {
		t.Fatalf("unexpected observe --all error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(scenario.Worktree, "PASS"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("restore PASS: %v", err)
	}
	resp := runCLIJSON[cliObserveAllResponse](t, dir, "observe", scenario.ExpID, "--all")

	hostTestRecorded := false
	timingRecorded := false
	for _, result := range resp.Results {
		switch result.Inst {
		case "host_test":
			hostTestRecorded = result.Action == observeActionRecorded
		case "timing":
			timingRecorded = result.Action == observeActionRecorded
		}
	}
	if !hostTestRecorded || !timingRecorded {
		t.Fatalf("expected host_test and timing to record on recovery run, got %+v", resp.Results)
	}
}
