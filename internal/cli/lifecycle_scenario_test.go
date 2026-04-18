package cli

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bytter/autoresearch/internal/store"
)

type cliIDResponse struct {
	ID string `json:"id"`
}

type cliImplementResponse struct {
	ID       string `json:"id"`
	Worktree string `json:"worktree"`
	Branch   string `json:"branch"`
}

type cliObserveAllResponse struct {
	Results []struct {
		ID   string `json:"id"`
		Inst string `json:"instrument"`
	} `json:"results"`
}

type cliAnalyzeResponse struct {
	Rows []struct {
		Instrument string `json:"instrument"`
		Comparison *struct {
			DeltaFrac float64 `json:"delta_frac"`
		} `json:"comparison,omitempty"`
	} `json:"rows"`
}

type cliExperimentListRow struct {
	ID               string `json:"id"`
	Classification   string `json:"classification"`
	HypothesisStatus string `json:"hypothesis_status,omitempty"`
}

type cliFrontierResponse struct {
	ScopeGoalID    string `json:"scope_goal_id,omitempty"`
	GoalID         string `json:"goal_id"`
	StalledFor     int    `json:"stalled_for"`
	GoalAssessment struct {
		Met             bool   `json:"met"`
		MetByConclusion string `json:"met_by_conclusion,omitempty"`
	} `json:"goal_assessment"`
	Frontier []struct {
		Conclusion       string `json:"conclusion"`
		Candidate        string `json:"candidate_experiment"`
		Classification   string `json:"classification"`
		HypothesisStatus string `json:"hypothesis_status,omitempty"`
	} `json:"frontier"`
}

type cliDashboardResponse struct {
	ScopeGoalID string         `json:"scope_goal_id,omitempty"`
	StalledFor  int            `json:"stalled_for"`
	Counts      map[string]int `json:"counts"`
	Frontier    []struct {
		Conclusion       string `json:"conclusion"`
		Candidate        string `json:"candidate_experiment"`
		Classification   string `json:"classification"`
		HypothesisStatus string `json:"hypothesis_status,omitempty"`
	} `json:"frontier"`
}

type cliStatusResponse struct {
	ScopeGoalID       string         `json:"scope_goal_id,omitempty"`
	MainCheckoutDirty bool           `json:"main_checkout_dirty"`
	Counts            map[string]int `json:"counts"`
}

func TestLifecycleScenario_ReadSurfacesStayConsistentAfterAcceptedWinAndLaterStall(t *testing.T) {
	saveGlobals(t)
	dir := gitInitScenarioRepo(t)
	if _, err := store.Create(dir, store.Config{
		Build:     store.CommandSpec{Command: "true"},
		Test:      store.CommandSpec{Command: "true"},
		Worktrees: store.WorktreesConfig{Root: filepath.Join(t.TempDir(), "worktrees")},
	}); err != nil {
		t.Fatalf("store.Create: %v", err)
	}

	registerScenarioInstruments(t, dir)

	goal := runCLIJSON[cliIDResponse](t, dir,
		"goal", "set",
		"--objective-instrument", "timing",
		"--objective-target", "kernel",
		"--objective-direction", "decrease",
		"--success-threshold", "0.1",
		"--on-success", "stop",
		"--constraint-max", "binary_size=1000",
		"--constraint-require", "host_test=pass",
	)
	baseline := runCLIJSON[cliIDResponse](t, dir, "experiment", "baseline")

	hyp1 := runCLIJSON[cliIDResponse](t, dir,
		"hypothesis", "add",
		"--claim", "tighten the hot loop",
		"--predicts-instrument", "timing",
		"--predicts-target", "kernel",
		"--predicts-direction", "decrease",
		"--predicts-min-effect", "0.1",
		"--kill-if", "tests fail",
	)
	exp1 := runCLIJSON[cliIDResponse](t, dir,
		"experiment", "design", hyp1.ID,
		"--baseline", "HEAD",
		"--instruments", "timing,binary_size,host_test",
	)
	impl1 := runCLIJSON[cliImplementResponse](t, dir, "experiment", "implement", exp1.ID)
	writeScenarioMetrics(t, impl1.Worktree, "80\n", "900\n")
	gitCommitAll(t, impl1.Worktree, "improve timing")

	obs1 := runCLIJSON[cliObserveAllResponse](t, dir, "observe", exp1.ID, "--all")
	analyze1 := runCLIJSON[cliAnalyzeResponse](t, dir, "analyze", exp1.ID, "--baseline", baseline.ID)
	if got, want := len(analyze1.Rows), 3; got != want {
		t.Fatalf("analyze rows len = %d, want %d", got, want)
	}
	if got := analyzeComparisonDeltaFrac(t, analyze1, "timing"); got >= 0 {
		t.Fatalf("timing delta_frac = %v, want negative improvement", got)
	}
	concl1 := runCLIJSON[cliIDResponse](t, dir,
		"conclude", hyp1.ID,
		"--verdict", "supported",
		"--baseline-experiment", baseline.ID,
		"--observations", observeResultID(t, obs1, "timing"),
	)
	runCLIJSON[cliIDResponse](t, dir,
		"conclusion", "accept", concl1.ID,
		"--reviewed-by", "human:gate",
		"--rationale", "Stats confirmed. Code matches the mechanism. No gaming or metric manipulation was detected.",
	)

	hyp2 := runCLIJSON[cliIDResponse](t, dir,
		"hypothesis", "add",
		"--claim", "a smaller tweak might still help",
		"--predicts-instrument", "timing",
		"--predicts-target", "kernel",
		"--predicts-direction", "decrease",
		"--predicts-min-effect", "0.05",
		"--kill-if", "tests fail",
	)
	exp2 := runCLIJSON[cliIDResponse](t, dir,
		"experiment", "design", hyp2.ID,
		"--baseline", "HEAD",
		"--instruments", "timing,binary_size,host_test",
	)
	impl2 := runCLIJSON[cliImplementResponse](t, dir, "experiment", "implement", exp2.ID)
	writeScenarioMetrics(t, impl2.Worktree, "95\n", "900\n")
	gitCommitAll(t, impl2.Worktree, "small tweak")

	obs2 := runCLIJSON[cliObserveAllResponse](t, dir, "observe", exp2.ID, "--all")
	runCLIJSON[cliIDResponse](t, dir,
		"conclude", hyp2.ID,
		"--verdict", "inconclusive",
		"--baseline-experiment", baseline.ID,
		"--observations", observeResultID(t, obs2, "timing"),
	)

	dead := runCLIJSON[[]cliExperimentListRow](t, dir, "experiment", "list", "--goal", goal.ID, "--classification", experimentClassificationDead)
	if got, want := len(dead), 1; got != want {
		t.Fatalf("dead experiment list len = %d, want %d", got, want)
	}
	if dead[0].ID != exp1.ID || dead[0].HypothesisStatus != "supported" {
		t.Fatalf("unexpected dead experiment row: %+v", dead[0])
	}

	frontier := runCLIJSON[cliFrontierResponse](t, dir, "frontier", "--goal", goal.ID)
	if frontier.ScopeGoalID != goal.ID || frontier.GoalID != goal.ID {
		t.Fatalf("frontier goal scope mismatch: %+v", frontier)
	}
	if !frontier.GoalAssessment.Met || frontier.GoalAssessment.MetByConclusion != concl1.ID {
		t.Fatalf("unexpected frontier goal_assessment: %+v", frontier.GoalAssessment)
	}
	if got, want := frontier.StalledFor, 1; got != want {
		t.Fatalf("frontier stalled_for = %d, want %d", got, want)
	}
	if got, want := len(frontier.Frontier), 1; got != want {
		t.Fatalf("frontier rows len = %d, want %d", got, want)
	}
	if frontier.Frontier[0].Candidate != exp1.ID ||
		frontier.Frontier[0].Conclusion != concl1.ID ||
		frontier.Frontier[0].Classification != experimentClassificationDead ||
		frontier.Frontier[0].HypothesisStatus != "supported" {
		t.Fatalf("unexpected frontier row: %+v", frontier.Frontier[0])
	}

	dashboard := runCLIJSON[cliDashboardResponse](t, dir, "dashboard", "--goal", goal.ID)
	if dashboard.ScopeGoalID != goal.ID {
		t.Fatalf("dashboard scope_goal_id = %q, want %q", dashboard.ScopeGoalID, goal.ID)
	}
	if got, want := dashboard.StalledFor, frontier.StalledFor; got != want {
		t.Fatalf("dashboard stalled_for = %d, want %d", got, want)
	}
	if got, want := dashboard.Counts["hypotheses"], 2; got != want {
		t.Fatalf("dashboard counts[hypotheses] = %d, want %d", got, want)
	}
	if got, want := dashboard.Counts["experiments"], 3; got != want {
		t.Fatalf("dashboard counts[experiments] = %d, want %d", got, want)
	}
	if got, want := dashboard.Counts["observations"], 9; got != want {
		t.Fatalf("dashboard counts[observations] = %d, want %d", got, want)
	}
	if got, want := dashboard.Counts["conclusions"], 2; got != want {
		t.Fatalf("dashboard counts[conclusions] = %d, want %d", got, want)
	}
	if got, want := len(dashboard.Frontier), 1; got != want {
		t.Fatalf("dashboard frontier len = %d, want %d", got, want)
	}
	if dashboard.Frontier[0].Candidate != exp1.ID ||
		dashboard.Frontier[0].Conclusion != concl1.ID ||
		dashboard.Frontier[0].Classification != experimentClassificationDead {
		t.Fatalf("unexpected dashboard frontier row: %+v", dashboard.Frontier[0])
	}

	status := runCLIJSON[cliStatusResponse](t, dir, "status", "--goal", goal.ID)
	if status.ScopeGoalID != goal.ID {
		t.Fatalf("status scope_goal_id = %q, want %q", status.ScopeGoalID, goal.ID)
	}
	if status.MainCheckoutDirty {
		t.Fatalf("status reported dirty main checkout for clean scenario")
	}
	if got, want := status.Counts["hypotheses"], 2; got != want {
		t.Fatalf("status counts[hypotheses] = %d, want %d", got, want)
	}
	if got, want := status.Counts["experiments"], 3; got != want {
		t.Fatalf("status counts[experiments] = %d, want %d", got, want)
	}
	if got, want := status.Counts["observations"], 9; got != want {
		t.Fatalf("status counts[observations] = %d, want %d", got, want)
	}
	if got, want := status.Counts["conclusions"], 2; got != want {
		t.Fatalf("status counts[conclusions] = %d, want %d", got, want)
	}
}

func registerScenarioInstruments(t *testing.T, dir string) {
	t.Helper()
	runCLI(t, dir,
		"instrument", "register", "timing",
		"--cmd", "sh",
		"--cmd", "-c",
		"--cmd", "cat timing.txt",
		"--parser", "builtin:scalar",
		"--pattern", "([0-9]+)",
		"--unit", "ns",
	)
	runCLI(t, dir,
		"instrument", "register", "binary_size",
		"--cmd", "sh",
		"--cmd", "-c",
		"--cmd", "cat size.txt",
		"--parser", "builtin:scalar",
		"--pattern", "([0-9]+)",
		"--unit", "bytes",
	)
	runCLI(t, dir,
		"instrument", "register", "host_test",
		"--cmd", "sh",
		"--cmd", "-c",
		"--cmd", "test -f PASS",
		"--parser", "builtin:passfail",
		"--unit", "bool",
	)
}

func gitInitScenarioRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		gitRun(t, dir, args...)
	}
	writeScenarioMetrics(t, dir, "100\n", "900\n")
	if err := os.WriteFile(filepath.Join(dir, "PASS"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write PASS: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("baseline\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	gitRun(t, dir, "add", "timing.txt", "size.txt", "PASS", "README.md")
	gitRun(t, dir, "commit", "-m", "init")
	return dir
}

func writeScenarioMetrics(t *testing.T, dir, timing, size string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "timing.txt"), []byte(timing), 0o644); err != nil {
		t.Fatalf("write timing.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "size.txt"), []byte(size), 0o644); err != nil {
		t.Fatalf("write size.txt: %v", err)
	}
}

func gitCommitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gitRun(t, dir, "add", "timing.txt", "size.txt")
	gitRun(t, dir, "commit", "-m", msg)
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func runCLI(t *testing.T, dir string, args ...string) string {
	t.Helper()

	oldStdout, oldStderr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	outCh := make(chan string, 1)
	errCh := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(rOut)
		outCh <- string(data)
	}()
	go func() {
		data, _ := io.ReadAll(rErr)
		errCh <- string(data)
	}()

	os.Stdout = wOut
	os.Stderr = wErr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	root := Root()
	root.SetArgs(append([]string{"-C", dir}, args...))
	execErr := root.Execute()

	_ = wOut.Close()
	_ = wErr.Close()
	stdout := <-outCh
	stderr := <-errCh
	if execErr != nil {
		t.Fatalf("autoresearch %s: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), execErr, stdout, stderr)
	}
	return stdout
}

func runCLIJSON[T any](t *testing.T, dir string, args ...string) T {
	t.Helper()
	out := runCLI(t, dir, append([]string{"--json"}, args...)...)
	var got T
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode JSON for %q: %v\n%s", strings.Join(args, " "), err, out)
	}
	return got
}

func observeResultID(t *testing.T, resp cliObserveAllResponse, inst string) string {
	t.Helper()
	for _, r := range resp.Results {
		if r.Inst == inst {
			return r.ID
		}
	}
	t.Fatalf("observe result for %q not found in %+v", inst, resp.Results)
	return ""
}

func analyzeComparisonDeltaFrac(t *testing.T, resp cliAnalyzeResponse, inst string) float64 {
	t.Helper()
	for _, row := range resp.Rows {
		if row.Instrument == inst {
			if row.Comparison == nil {
				t.Fatalf("analyze row %q missing comparison", inst)
			}
			return row.Comparison.DeltaFrac
		}
	}
	t.Fatalf("analyze row %q not found", inst)
	return 0
}
