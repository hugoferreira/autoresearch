// Package worktree wraps the git CLI for worktree lifecycle operations.
// Shelling out to git is intentional — it avoids a heavy go-git dependency and
// keeps behavior identical to what a human operator would see.
package worktree

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// IsRepo reports whether dir is inside a git work tree.
func IsRepo(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// ResolveRef returns the full SHA for a ref (HEAD, main, a short SHA, etc.).
func ResolveRef(dir, ref string) (string, error) {
	return run(dir, "rev-parse", ref)
}

// SymbolicFullName resolves a rev-like input to its full symbolic ref name
// (for example "main" -> "refs/heads/main"). Non-symbolic inputs such as raw
// SHAs resolve to the empty string.
func SymbolicFullName(dir, ref string) (string, error) {
	return run(dir, "rev-parse", "--symbolic-full-name", ref)
}

// Add creates a new git worktree at path, checking out a new branch off baseline.
func Add(projectDir, path, branch, baseline string) error {
	_, err := run(projectDir, "worktree", "add", "-b", branch, path, baseline)
	return err
}

// Remove deletes a worktree (but not the backing branch).
func Remove(projectDir, path string) error {
	_, err := run(projectDir, "worktree", "remove", "--force", path)
	return err
}

// RenameBranch renames a branch that is not currently checked out in any worktree.
// Used by `experiment reset` to preserve an abandoned attempt under a timestamped name.
func RenameBranch(projectDir, oldName, newName string) error {
	_, err := run(projectDir, "branch", "-m", oldName, newName)
	return err
}

// ListBranches returns branch names matching a glob pattern (e.g.
// "autoresearch/*@*" for archived experiment branches).
func ListBranches(projectDir, pattern string) ([]string, error) {
	out, err := run(projectDir, "for-each-ref", "--format=%(refname:short)", "refs/heads/"+pattern)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// HasCommitsAbove reports whether branch has at least one commit beyond
// baseline. Used to detect experiments where the worktree was created but
// no code was committed.
func HasCommitsAbove(projectDir, branch, baselineSHA string) (bool, error) {
	out, err := run(projectDir, "log", "--oneline", baselineSHA+".."+branch)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// List returns the absolute paths of all worktrees known to the repo.
func List(projectDir string) ([]string, error) {
	out, err := run(projectDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			paths = append(paths, strings.TrimPrefix(line, "worktree "))
		}
	}
	return paths, nil
}

// Diff returns the unified diff between base and branch. Output is not
// trimmed so the caller can print it verbatim.
func Diff(projectDir, base, branch string) (string, error) {
	cmd := exec.Command("git", "-C", projectDir, "diff", base+".."+branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// CherryPick applies the commits in baseSHA..branch onto the current HEAD.
func CherryPick(projectDir, baseSHA, branch string) (string, error) {
	return run(projectDir, "cherry-pick", baseSHA+".."+branch)
}

// Merge merges branch into the current HEAD with --no-edit.
func Merge(projectDir, branch string) (string, error) {
	return run(projectDir, "merge", branch, "--no-edit")
}

// DirtyPaths returns relative paths in the given checkout that differ from
// HEAD, including untracked files. The list is sorted and deduplicated. If the
// directory is not a git repo, it returns an empty list.
func DirtyPaths(projectDir string) ([]string, error) {
	if !IsRepo(projectDir) {
		return []string{}, nil
	}

	seen := map[string]bool{}
	addLines := func(out string) {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			seen[line] = true
		}
	}

	out, err := run(projectDir, "diff", "--name-only", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	addLines(out)

	out, err = run(projectDir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	addLines(out)

	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}
