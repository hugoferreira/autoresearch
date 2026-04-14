package worktree_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bytter/autoresearch/internal/worktree"
)

func gitInit(t *testing.T) string {
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
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
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

func TestIsRepo(t *testing.T) {
	dir := gitInit(t)
	if !worktree.IsRepo(dir) {
		t.Error("expected IsRepo to be true")
	}
	if worktree.IsRepo(t.TempDir()) {
		t.Error("expected empty dir to not be a repo")
	}
}

func TestAddAndRemove(t *testing.T) {
	dir := gitInit(t)
	sha, err := worktree.ResolveRef(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) < 40 {
		t.Errorf("short SHA: %q", sha)
	}

	wtPath := filepath.Join(dir, ".research", "worktrees", "E-0001")
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := worktree.Add(dir, wtPath, "autoresearch/E-0001", sha); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "README.md")); err != nil {
		t.Errorf("worktree should contain README.md: %v", err)
	}

	if err := worktree.Remove(dir, wtPath); err != nil {
		t.Errorf("Remove: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree should be gone: %v", err)
	}
}

func TestDirtyPaths(t *testing.T) {
	dir := gitInit(t)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\nupdated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := worktree.DirtyPaths(dir)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"README.md", "notes.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DirtyPaths() = %v, want %v", got, want)
	}
}
