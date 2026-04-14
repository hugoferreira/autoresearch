package cli

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/worktree"
)

type mainCheckoutState struct {
	Dirty bool     `json:"dirty"`
	Paths []string `json:"paths"`
}

func captureMainCheckoutState(projectDir string) (mainCheckoutState, error) {
	paths, err := worktree.DirtyPaths(projectDir)
	if err != nil {
		return mainCheckoutState{}, err
	}

	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if isAutoresearchManagedCheckoutPath(path) {
			continue
		}
		filtered = append(filtered, path)
	}
	sort.Strings(filtered)
	return mainCheckoutState{
		Dirty: len(filtered) > 0,
		Paths: filtered,
	}, nil
}

func isAutoresearchManagedCheckoutPath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	switch path {
	case "AGENTS.md", ".gitignore":
		return true
	}
	return strings.HasPrefix(path, ".claude/") ||
		strings.HasPrefix(path, ".codex/") ||
		strings.HasPrefix(path, ".research/")
}
