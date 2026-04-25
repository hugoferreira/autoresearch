package cli

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type cliIDResponse struct {
	ID string `json:"id"`
}

type cliImplementResponse struct {
	Worktree string `json:"worktree"`
}

type cliObserveAllResponse struct {
	Action             string   `json:"action"`
	Observations       []string `json:"observations"`
	NewObservations    []string `json:"new_observations"`
	ReusedObservations []string `json:"reused_observations"`
	Results            []struct {
		ID     string `json:"id"`
		Inst   string `json:"instrument"`
		Action string `json:"action"`
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

type observeScenarioExperiment struct {
	ExpID    string
	Worktree string
}

func setupObserveScenarioStore() string {
	GinkgoHelper()
	dir := gitInitScenarioRepo()
	_, err := store.Create(dir, store.Config{
		Build:     store.CommandSpec{Command: "true"},
		Test:      store.CommandSpec{Command: "true"},
		Worktrees: store.WorktreesConfig{Root: filepath.Join(GinkgoT().TempDir(), "worktrees")},
	})
	Expect(err).NotTo(HaveOccurred())
	return dir
}

func setupObserveScenarioExperiment(dir, instruments string, goalArgs ...string) observeScenarioExperiment {
	GinkgoHelper()

	args := []string{
		"goal", "set",
		"--objective-instrument", "timing",
		"--objective-target", "kernel",
		"--objective-direction", "decrease",
	}
	args = append(args, goalArgs...)
	runCLIJSON[cliIDResponse](dir, args...)

	hyp := runCLIJSON[cliIDResponse](dir,
		"hypothesis", "add",
		"--claim", "tighten the hot loop",
		"--predicts-instrument", "timing",
		"--predicts-target", "kernel",
		"--predicts-direction", "decrease",
		"--predicts-min-effect", "0.1",
		"--kill-if", "tests fail",
	)
	exp := runCLIJSON[cliIDResponse](dir,
		"experiment", "design", hyp.ID,
		"--baseline", "HEAD",
		"--instruments", instruments,
	)
	impl := runCLIJSON[cliImplementResponse](dir, "experiment", "implement", exp.ID)
	return observeScenarioExperiment{
		ExpID:    exp.ID,
		Worktree: impl.Worktree,
	}
}

func setupTimingObserveScenario() (string, observeScenarioExperiment) {
	GinkgoHelper()
	dir := setupObserveScenarioStore()
	registerScenarioInstruments(dir)
	return dir, setupObserveScenarioExperiment(dir, "timing", "--constraint-max", "binary_size=1000")
}

func commitScenarioMetricsCandidate(worktree, refName, message, timing, size string) string {
	GinkgoHelper()
	writeScenarioMetrics(worktree, timing, size)
	gitCommitAll(worktree, message)
	return gitCreateCandidateRef(worktree, refName)
}

func registerScenarioInstruments(dir string) {
	GinkgoHelper()
	registerScenarioTimingInstrument(dir)
	registerScenarioSupportInstruments(dir)
}

func registerScenarioTimingInstrument(dir string, evidence ...string) {
	GinkgoHelper()
	args := []string{
		"instrument", "register", "timing",
		"--cmd", "sh",
		"--cmd", "-c",
		"--cmd", "cat timing.txt",
		"--parser", "builtin:scalar",
		"--pattern", "([0-9]+)",
		"--unit", "ns",
	}
	for _, ev := range evidence {
		args = append(args, "--evidence", ev)
	}
	runCLI(dir, args...)
}

func registerScenarioSupportInstruments(dir string) {
	GinkgoHelper()
	runCLI(dir,
		"instrument", "register", "binary_size",
		"--cmd", "sh",
		"--cmd", "-c",
		"--cmd", "cat size.txt",
		"--parser", "builtin:scalar",
		"--pattern", "([0-9]+)",
		"--unit", "bytes",
	)
	runCLI(dir,
		"instrument", "register", "host_test",
		"--cmd", "sh",
		"--cmd", "-c",
		"--cmd", "test -f PASS",
		"--parser", "builtin:passfail",
		"--unit", "bool",
	)
}

func gitInitScenarioRepo() string {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		gitRun(dir, args...)
	}
	writeScenarioMetrics(dir, "100\n", "900\n")
	Expect(os.WriteFile(filepath.Join(dir, "PASS"), []byte("ok\n"), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(dir, "README.md"), []byte("baseline\n"), 0o644)).To(Succeed())
	gitRun(dir, "add", "timing.txt", "size.txt", "PASS", "README.md")
	gitRun(dir, "commit", "-m", "init")
	return dir
}

func writeScenarioMetrics(dir, timing, size string) {
	GinkgoHelper()
	Expect(os.WriteFile(filepath.Join(dir, "timing.txt"), []byte(timing), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(dir, "size.txt"), []byte(size), 0o644)).To(Succeed())
}

func writeScenarioMechanism(dir, content string) {
	GinkgoHelper()
	Expect(os.WriteFile(filepath.Join(dir, "mechanism.txt"), []byte(content), 0o644)).To(Succeed())
}

func gitCommitAll(dir, msg string) {
	GinkgoHelper()
	gitRun(dir, "add", "timing.txt", "size.txt")
	gitRun(dir, "commit", "-m", msg)
}

func gitCreateCandidateRef(dir, ref string) string {
	GinkgoHelper()
	gitRun(dir, "branch", ref, "HEAD")
	return ref
}

func gitRun(dir string, args ...string) {
	GinkgoHelper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "git %v failed:\n%s", args, out)
}
