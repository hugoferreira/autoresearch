package cli

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bytter/autoresearch/internal/integration"
	"github.com/bytter/autoresearch/internal/worktree"
)

type mainCheckoutState struct {
	Dirty bool     `json:"dirty"`
	Paths []string `json:"paths"`
}

var managedCheckoutPaths struct {
	once  sync.Once
	paths map[string]bool
	err   error
}

func captureMainCheckoutState(projectDir string) (mainCheckoutState, error) {
	paths, err := worktree.DirtyPaths(projectDir)
	if err != nil {
		return mainCheckoutState{}, err
	}
	managed, err := autoresearchManagedCheckoutPaths()
	if err != nil {
		return mainCheckoutState{}, err
	}

	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if isAutoresearchManagedCheckoutPath(path, managed) {
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

func autoresearchManagedCheckoutPaths() (map[string]bool, error) {
	managedCheckoutPaths.once.Do(func() {
		// Only exclude files autoresearch owns end-to-end. Merged surfaces like
		// AGENTS.md, .gitignore, and .claude/settings.json remain visible because
		// user-owned content in those files can drift independently.
		files := map[string]bool{}
		add := func(path string) {
			path = filepath.ToSlash(strings.TrimSpace(path))
			if path != "" {
				files[path] = true
			}
		}

		add(integration.ClaudeDocRelPath)
		add(integration.CodexDocRelPath)

		claudeAgents, err := integration.EmbeddedAgents()
		if err != nil {
			managedCheckoutPaths.err = err
			return
		}
		for _, a := range claudeAgents {
			add(filepath.Join(".claude", "agents", a.Filename))
		}

		codexAgents, err := integration.EmbeddedCodexAgents()
		if err != nil {
			managedCheckoutPaths.err = err
			return
		}
		for _, a := range codexAgents {
			add(filepath.Join(".codex", "agents", a.Filename))
		}

		managedCheckoutPaths.paths = files
	})
	if managedCheckoutPaths.err != nil {
		return nil, managedCheckoutPaths.err
	}
	return managedCheckoutPaths.paths, nil
}

func isAutoresearchManagedCheckoutPath(path string, managed map[string]bool) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	if path == ".research" || strings.HasPrefix(path, ".research/") {
		return true
	}
	return managed[path]
}
