package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bytter/autoresearch/internal/store"
)

func initGitRepo(t *testing.T) string {
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

func TestCaptureMainCheckoutState_FiltersAutoresearchManagedPaths(t *testing.T) {
	dir := initGitRepo(t)

	for path, body := range map[string]string{
		".claude/autoresearch.md": "managed\n",
		"AGENTS.md":               "managed\n",
		".gitignore":              ".research/\n",
		".research/state.json":    "{}\n",
		"bootstrap.sh":            "#!/bin/sh\n",
	} {
		abs := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := captureMainCheckoutState(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !got.Dirty {
		t.Fatal("expected main checkout to be dirty")
	}
	want := []string{"bootstrap.sh"}
	if !reflect.DeepEqual(got.Paths, want) {
		t.Fatalf("paths = %v, want %v", got.Paths, want)
	}
}

func TestCaptureDashboard_RecordsMainCheckoutWarning(t *testing.T) {
	dir := initGitRepo(t)
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "bootstrap.sh"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

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
}
