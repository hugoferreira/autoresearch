package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"

	"github.com/bytter/autoresearch/internal/integration"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

func initGitRepo(t testkit.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "README.md"},
		{"commit", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func writeFile(t testkit.T, root, rel, body string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

var _ = ginkgo.Describe("TestCaptureMainCheckoutState_FiltersOnlyFullyManagedPaths", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := initGitRepo(t)

		for _, rel := range []string{
			integration.ClaudeDocRelPath,
			integration.CodexDocRelPath,
			".claude/agents/research-orchestrator.md",
			".claude/agents/research-gate-reviewer.md",
			".codex/agents/research-orchestrator.toml",
			".codex/agents/research-gate-reviewer.toml",
			".research/state.json",
		} {
			writeFile(t, dir, rel, "managed\n")
		}

		for path, body := range map[string]string{
			"AGENTS.md":             "team notes\n",
			".gitignore":            ".cache/\n",
			".claude/settings.json": "{\n  \"permissions\": {}\n}\n",
			"bootstrap.sh":          "#!/bin/sh\n",
		} {
			writeFile(t, dir, path, body)
		}

		got, err := captureMainCheckoutState(dir)
		if err != nil {
			t.Fatal(err)
		}

		if !got.Dirty {
			t.Fatal("expected main checkout to be dirty")
		}
		want := []string{
			".claude/settings.json",
			".gitignore",
			"AGENTS.md",
			"bootstrap.sh",
		}
		if !reflect.DeepEqual(got.Paths, want) {
			t.Fatalf("paths = %v, want %v", got.Paths, want)
		}
	})
})

var _ = ginkgo.Describe("TestCaptureDashboard_RecordsMainCheckoutWarning", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		dir := initGitRepo(t)
		s, err := store.Create(dir, store.Config{
			Build: store.CommandSpec{Command: "true"},
			Test:  store.CommandSpec{Command: "true"},
		})
		if err != nil {
			t.Fatal(err)
		}

		writeFile(t, dir, "bootstrap.sh", "#!/bin/sh\n")

		snap, err := captureDashboard(s)
		if err != nil {
			t.Fatal(err)
		}
		if !snap.MainCheckoutDirty {
			t.Fatal("expected dashboard snapshot to flag dirty main checkout")
		}
		want := []string{"bootstrap.sh"}
		if !reflect.DeepEqual(snap.MainCheckoutDirtyPaths, want) {
			t.Fatalf("paths = %v, want %v", snap.MainCheckoutDirtyPaths, want)
		}
	})
})
