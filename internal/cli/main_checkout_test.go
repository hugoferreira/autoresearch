package cli

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bytter/autoresearch/internal/integration"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func initGitRepo() string {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		runGit(dir, args...)
	}
	writeFile(dir, "README.md", "hello\n")
	runGit(dir, "add", "README.md")
	runGit(dir, "commit", "-m", "init")
	return dir
}

func runGit(dir string, args ...string) {
	GinkgoHelper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "git %v failed:\n%s", args, out)
}

func writeFile(root, rel, body string) {
	GinkgoHelper()
	abs := filepath.Join(root, rel)
	Expect(os.MkdirAll(filepath.Dir(abs), 0o755)).To(Succeed())
	Expect(os.WriteFile(abs, []byte(body), 0o644)).To(Succeed())
}

var _ = Describe("main checkout dirtiness", func() {
	It("filters managed autoresearch files out of dirty path warnings", func() {
		dir := initGitRepo()

		for _, rel := range []string{
			integration.ClaudeDocRelPath,
			integration.CodexDocRelPath,
			".claude/agents/research-orchestrator.md",
			".claude/agents/research-gate-reviewer.md",
			".codex/agents/research-orchestrator.toml",
			".codex/agents/research-gate-reviewer.toml",
			".research/state.json",
		} {
			writeFile(dir, rel, "managed\n")
		}
		for path, body := range map[string]string{
			"AGENTS.md":             "team notes\n",
			".gitignore":            ".cache/\n",
			".claude/settings.json": "{\n  \"permissions\": {}\n}\n",
			"bootstrap.sh":          "#!/bin/sh\n",
		} {
			writeFile(dir, path, body)
		}

		got, err := captureMainCheckoutState(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Dirty).To(BeTrue())
		Expect(got.Paths).To(Equal([]string{
			".claude/settings.json",
			".gitignore",
			"AGENTS.md",
			"bootstrap.sh",
		}))
	})

	It("records dirty main checkout warnings in dashboard snapshots", func() {
		dir := initGitRepo()
		s, err := store.Create(dir, store.Config{
			Build: store.CommandSpec{Command: "true"},
			Test:  store.CommandSpec{Command: "true"},
		})
		Expect(err).NotTo(HaveOccurred())

		writeFile(dir, "bootstrap.sh", "#!/bin/sh\n")
		snap, err := captureDashboard(s)
		Expect(err).NotTo(HaveOccurred())
		Expect(snap.MainCheckoutDirty).To(BeTrue())
		Expect(snap.MainCheckoutDirtyPaths).To(Equal([]string{"bootstrap.sh"}))
	})
})
