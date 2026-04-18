package cli

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

// researchHash walks .research/ under dir and returns a content hash over
// (relpath + size + mode + sha256 of bytes). Used to assert that a
// --dry-run invocation leaves the durable store byte-for-byte unchanged.
func researchHash(t *testing.T, dir string) string {
	t.Helper()
	root := filepath.Join(dir, ".research")
	h := sha256.New()
	var entries []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			entries = append(entries, "d:"+rel)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fh := sha256.Sum256(data)
		entries = append(entries, fmt.Sprintf("f:%s:%d:%o:%x", rel, info.Size(), info.Mode(), fh))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(entries)
	for _, e := range entries {
		h.Write([]byte(e))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// TestDryRun_ShortCircuitsAllMutatingVerbs drives a representative sample of
// mutating verbs with --dry-run and asserts (1) the RunE returns ErrDryRun
// (so main() exits 0) and (2) the .research/ tree is byte-for-byte
// unchanged.
//
// Regression anchor for #19: the shared dryRun() helper previously returned
// whatever w.Emit() returned (nil on success), so every `if err :=
// dryRun(...); err != nil { return err }` guard fell through and the
// mutation ran despite the preview being printed.
func TestDryRun_ShortCircuitsAllMutatingVerbs(t *testing.T) {
	type verbCase struct {
		name string
		// setup runs against an already-initialized store with an active
		// goal and returns any extra args the verb needs (e.g. a
		// hypothesis ID the verb will mutate).
		setup func(t *testing.T, dir string) []string
		// argsTail is appended to the base args after setup.
		argsTail func(ids []string) []string
	}

	cases := []verbCase{
		{
			name: "hypothesis_add",
			argsTail: func(_ []string) []string {
				return []string{
					"hypothesis", "add",
					"--claim", "tighten inner loop",
					"--predicts-instrument", "timing",
					"--predicts-target", "fir",
					"--predicts-direction", "decrease",
					"--predicts-min-effect", "0.05",
					"--kill-if", "tests fail",
				}
			},
		},
		{
			name:  "hypothesis_kill",
			setup: setupHypothesis,
			argsTail: func(ids []string) []string {
				return []string{"hypothesis", "kill", ids[0], "--reason", "stale"}
			},
		},
		{
			name:  "hypothesis_reopen",
			setup: setupKilledHypothesis,
			argsTail: func(ids []string) []string {
				return []string{"hypothesis", "reopen", ids[0], "--reason", "back on"}
			},
		},
		{
			name:  "conclusion_withdraw",
			setup: setupSupportedConclusion,
			argsTail: func(ids []string) []string {
				return []string{"conclusion", "withdraw", ids[0], "--reason", "bad benchmark"}
			},
		},
		{
			name: "instrument_register",
			argsTail: func(_ []string) []string {
				return []string{
					"instrument", "register", "extra",
					"--cmd", "true",
					"--parser", "builtin:passfail",
					"--unit", "bool",
				}
			},
		},
		{
			name: "pause",
			argsTail: func(_ []string) []string {
				return []string{"pause", "--reason", "dry-run probe"}
			},
		},
		{
			name:  "resume",
			setup: setupPaused,
			argsTail: func(_ []string) []string {
				return []string{"resume"}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			saveGlobals(t)
			dir, _ := setupGoalStore(t)

			var ids []string
			if tc.setup != nil {
				ids = tc.setup(t, dir)
			}

			root := Root()
			args := append([]string{"-C", dir, "--dry-run"}, tc.argsTail(ids)...)
			root.SetArgs(args)

			before := researchHash(t, dir)

			err := root.Execute()
			if err == nil {
				t.Fatalf("expected ErrDryRun, got nil")
			}
			if !errors.Is(err, ErrDryRun) {
				t.Fatalf("expected ErrDryRun, got %v", err)
			}

			after := researchHash(t, dir)
			if before != after {
				t.Errorf(".research/ changed under --dry-run\n  before=%s\n   after=%s", before, after)
			}
		})
	}
}

func setupHypothesis(t *testing.T, dir string) []string {
	t.Helper()
	root := Root()
	root.SetArgs([]string{
		"-C", dir,
		"hypothesis", "add",
		"--claim", "setup hypothesis",
		"--predicts-instrument", "timing",
		"--predicts-target", "fir",
		"--predicts-direction", "decrease",
		"--predicts-min-effect", "0.05",
		"--kill-if", "tests fail",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("setup hypothesis add: %v", err)
	}
	return []string{"H-0001"}
}

func setupKilledHypothesis(t *testing.T, dir string) []string {
	t.Helper()
	ids := setupHypothesis(t, dir)

	root := Root()
	root.SetArgs([]string{
		"-C", dir,
		"hypothesis", "kill", ids[0],
		"--reason", "prereq for test",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("setup kill: %v", err)
	}
	return ids
}

func setupPaused(t *testing.T, dir string) []string {
	t.Helper()
	root := Root()
	root.SetArgs([]string{
		"-C", dir,
		"pause", "--reason", "setup",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("setup pause: %v", err)
	}
	return nil
}

func setupSupportedConclusion(t *testing.T, dir string) []string {
	t.Helper()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	now := nowUTC()
	h := &entity.Hypothesis{
		ID:        "H-0001",
		GoalID:    "G-0001",
		Claim:     "setup hypothesis",
		Predicts:  entity.Predicts{Instrument: "timing", Target: "fir", Direction: "decrease", MinEffect: 0.05},
		KillIf:    []string{"tests fail"},
		Status:    entity.StatusSupported,
		Author:    "agent:analyst",
		CreatedAt: now,
	}
	if err := s.WriteHypothesis(h); err != nil {
		t.Fatalf("write hypothesis: %v", err)
	}
	c := &entity.Conclusion{
		ID:           "C-0001",
		Hypothesis:   h.ID,
		Verdict:      entity.VerdictSupported,
		CandidateExp: "E-0001",
		Effect:       entity.Effect{Instrument: "timing", DeltaFrac: -0.1},
		ReviewedBy:   "human:gate",
		Author:       "agent:analyst",
		CreatedAt:    now,
	}
	if err := s.WriteConclusion(c); err != nil {
		t.Fatalf("write conclusion: %v", err)
	}
	return []string{"C-0001"}
}

// TestDryRun_NonDryRunStillMutates sanity-checks that the fix didn't
// accidentally break the non-dry-run path — a mutating verb without
// --dry-run must still mutate state.
func TestDryRun_NonDryRunStillMutates(t *testing.T) {
	saveGlobals(t)
	dir, _ := setupGoalStore(t)

	before := researchHash(t, dir)

	root := Root()
	root.SetArgs([]string{
		"-C", dir,
		"hypothesis", "add",
		"--claim", "non-dry-run",
		"--predicts-instrument", "timing",
		"--predicts-target", "fir",
		"--predicts-direction", "decrease",
		"--predicts-min-effect", "0.05",
		"--kill-if", "tests fail",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("hypothesis add: %v", err)
	}

	after := researchHash(t, dir)
	if before == after {
		t.Error("non-dry-run hypothesis add did not change .research/")
	}
}
