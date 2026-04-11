package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	Dir             = ".research"
	ConfigFile      = "config.yaml"
	StateFile       = "state.json"
	EventsFile      = "events.jsonl"
	GoalFile        = "goal.md"
	HypothesesDir   = "hypotheses"
	ExperimentsDir  = "experiments"
	ObservationsDir = "observations"
	ConclusionsDir  = "conclusions"
	ArtifactsDir    = "artifacts"
)

var (
	ErrNotInitialized     = errors.New("autoresearch is not initialized in this directory (no .research/)")
	ErrAlreadyInitialized = errors.New("autoresearch is already initialized in this directory")
)

type Store struct {
	root string
}

// Open finds a .research/ store by walking up from projectDir toward the
// filesystem root. This is the same discovery pattern `git` uses for `.git`.
// Callers can run autoresearch commands from any subdirectory of a project;
// they do not need to pass `-C <project-root>` every time. Running from
// inside an experiment worktree intentionally does NOT find the main
// project's store (different directory tree entirely), so worktree-scoped
// commands still need an explicit `-C`.
func Open(projectDir string) (*Store, error) {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, err
	}
	dir := abs
	for {
		candidate := filepath.Join(dir, Dir)
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return &Store{root: dir}, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotInitialized
		}
		dir = parent
	}
}

func Create(projectDir string, cfg Config) (*Store, error) {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, err
	}
	s := &Store{root: abs}
	if _, err := os.Stat(s.DirPath()); err == nil {
		return nil, ErrAlreadyInitialized
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// Default the worktrees root to the user cache dir, keyed by a hash of
	// the project's absolute path. Humans can edit config.yaml to override.
	if cfg.Worktrees.Root == "" {
		root, err := DefaultWorktreesRoot(abs)
		if err != nil {
			return nil, fmt.Errorf("derive default worktrees root: %w", err)
		}
		cfg.Worktrees.Root = root
	}

	for _, d := range []string{
		s.DirPath(),
		s.HypothesesDir(),
		s.ExperimentsDir(),
		s.ObservationsDir(),
		s.ConclusionsDir(),
		s.ArtifactsDir(),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", d, err)
		}
	}

	if err := s.writeConfig(cfg); err != nil {
		return nil, err
	}
	if err := s.writeState(State{SchemaVersion: 1, Counters: map[string]int{}}); err != nil {
		return nil, err
	}
	if err := s.initEvents(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Root() string            { return s.root }
func (s *Store) DirPath() string         { return filepath.Join(s.root, Dir) }
func (s *Store) ConfigPath() string      { return filepath.Join(s.DirPath(), ConfigFile) }
func (s *Store) StatePath() string       { return filepath.Join(s.DirPath(), StateFile) }
func (s *Store) EventsPath() string      { return filepath.Join(s.DirPath(), EventsFile) }
func (s *Store) GoalPath() string        { return filepath.Join(s.DirPath(), GoalFile) }
func (s *Store) HypothesesDir() string   { return filepath.Join(s.DirPath(), HypothesesDir) }
func (s *Store) ExperimentsDir() string  { return filepath.Join(s.DirPath(), ExperimentsDir) }
func (s *Store) ObservationsDir() string { return filepath.Join(s.DirPath(), ObservationsDir) }
func (s *Store) ConclusionsDir() string  { return filepath.Join(s.DirPath(), ConclusionsDir) }
func (s *Store) ArtifactsDir() string    { return filepath.Join(s.DirPath(), ArtifactsDir) }

// WorktreesRoot returns the configured absolute root directory under which
// this project's experiment worktrees live. It is deliberately outside the
// project tree so that naive grep/find cannot descend into the source-tree-
// shaped scratch copies of in-flight experiments.
func (s *Store) WorktreesRoot() (string, error) {
	cfg, err := s.Config()
	if err != nil {
		return "", err
	}
	if cfg.Worktrees.Root == "" {
		return "", fmt.Errorf("config.worktrees.root is not set")
	}
	return cfg.Worktrees.Root, nil
}

// DefaultWorktreesRoot derives the default worktrees root for a given project
// directory: <UserCacheDir>/autoresearch/<basename>-<8hexhash>/worktrees.
// The hash keys on the absolute project path, so two projects with the same
// basename coexist without collision.
func DefaultWorktreesRoot(projectDir string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "autoresearch", projectKey(abs), "worktrees"), nil
}

func projectKey(absPath string) string {
	h := sha256.Sum256([]byte(absPath))
	base := filepath.Base(absPath)
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, base)
	if safe == "" {
		safe = "project"
	}
	return fmt.Sprintf("%s-%s", safe, hex.EncodeToString(h[:4]))
}

func (s *Store) Counts() (map[string]int, error) {
	dirs := map[string]string{
		"hypotheses":   s.HypothesesDir(),
		"experiments":  s.ExperimentsDir(),
		"observations": s.ObservationsDir(),
		"conclusions":  s.ConclusionsDir(),
	}
	out := map[string]int{}
	for name, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", d, err)
		}
		n := 0
		for _, e := range entries {
			if !e.IsDir() {
				n++
			}
		}
		out[name] = n
	}
	return out, nil
}
