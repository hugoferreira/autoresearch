package worktree_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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

func gitCommit(t *testing.T, dir, file, body, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", file},
		{"commit", "-m", msg},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestDiff(t *testing.T) {
	dir := gitInit(t)
	baseSHA, err := worktree.ResolveRef(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "-C", dir, "checkout", "-b", "feature")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout -b: %v\n%s", err, out)
	}
	gitCommit(t, dir, "feature.txt", "feature\n", "add feature")

	diff, err := worktree.Diff(dir, baseSHA, "feature")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "feature.txt") {
		t.Errorf("diff should mention feature.txt, got: %q", diff)
	}
	if !strings.Contains(diff, "+feature") {
		t.Errorf("diff should contain the new line, got: %q", diff)
	}
}

func TestCherryPick(t *testing.T) {
	dir := gitInit(t)

	cmd := exec.Command("git", "-C", dir, "checkout", "-b", "feature")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout -b feature: %v\n%s", err, out)
	}
	featureBaseSHA, err := worktree.ResolveRef(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "feature.txt", "feature\n", "add feature")

	cmd = exec.Command("git", "-C", dir, "checkout", "main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout main: %v\n%s", err, out)
	}

	if _, err := worktree.CherryPick(dir, featureBaseSHA, "feature"); err != nil {
		t.Fatalf("CherryPick: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err != nil {
		t.Errorf("cherry-pick should have created feature.txt: %v", err)
	}
}

func TestMerge(t *testing.T) {
	dir := gitInit(t)

	cmd := exec.Command("git", "-C", dir, "checkout", "-b", "feature")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout -b feature: %v\n%s", err, out)
	}
	gitCommit(t, dir, "feature.txt", "feature\n", "add feature")

	cmd = exec.Command("git", "-C", dir, "checkout", "main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout main: %v\n%s", err, out)
	}

	if _, err := worktree.Merge(dir, "feature"); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err != nil {
		t.Errorf("merge should have brought in feature.txt: %v", err)
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
