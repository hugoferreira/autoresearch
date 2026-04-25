package cli

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// researchHash walks .research/ under dir and returns a content hash over
// (relpath + size + mode + sha256 of bytes). Used to assert that a
// --dry-run invocation leaves the durable store byte-for-byte unchanged.
func researchHash(dir string) string {
	GinkgoHelper()
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
	Expect(err).NotTo(HaveOccurred(), "walk %s", root)
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
var _ = Describe("dry-run mode", func() {
	type verbCase struct {
		name    string
		setup   func(dir string) []string
		argTail func(ids []string) []string
	}

	BeforeEach(saveGlobals)

	DescribeTable("short-circuits mutating verbs without changing .research",
		func(tc verbCase) {
			dir, _ := setupGoalStore()
			var ids []string
			if tc.setup != nil {
				ids = tc.setup(dir)
			}

			root := Root()
			root.SetArgs(append([]string{"-C", dir, "--dry-run"}, tc.argTail(ids)...))

			before := researchHash(dir)
			err := root.Execute()
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, ErrDryRun)).To(BeTrue(), "expected ErrDryRun, got %v", err)
			Expect(researchHash(dir)).To(Equal(before))
		},
		Entry("hypothesis add",
			verbCase{name: "hypothesis_add",
				argTail: func(_ []string) []string {
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
		),
		Entry("hypothesis kill",
			verbCase{
				name:  "hypothesis_kill",
				setup: setupHypothesis,
				argTail: func(ids []string) []string {
					return []string{"hypothesis", "kill", ids[0], "--reason", "stale"}
				},
			}),
		Entry("hypothesis reopen",
			verbCase{
				name:  "hypothesis_reopen",
				setup: setupKilledHypothesis,
				argTail: func(ids []string) []string {
					return []string{"hypothesis", "reopen", ids[0], "--reason", "back on"}
				},
			}),
		Entry("instrument register",
			verbCase{name: "instrument_register",
				argTail: func(_ []string) []string {
					return []string{
						"instrument", "register", "extra",
						"--cmd", "true",
						"--parser", "builtin:passfail",
						"--unit", "bool",
					}
				},
			},
		),
		Entry("pause",
			verbCase{name: "pause",
				argTail: func(_ []string) []string {
					return []string{"pause", "--reason", "dry-run probe"}
				},
			},
		),
		Entry("resume",
			verbCase{
				name:  "resume",
				setup: setupPaused,
				argTail: func(_ []string) []string {
					return []string{"resume"}
				},
			}),
	)

	It("still mutates state when --dry-run is not set", func() {
		dir, _ := setupGoalStore()
		before := researchHash(dir)

		runCLI(dir,
			"hypothesis", "add",
			"--claim", "non-dry-run",
			"--predicts-instrument", "timing",
			"--predicts-target", "fir",
			"--predicts-direction", "decrease",
			"--predicts-min-effect", "0.05",
			"--kill-if", "tests fail",
		)

		Expect(researchHash(dir)).NotTo(Equal(before))
	})
})

func setupHypothesis(dir string) []string {
	GinkgoHelper()
	runCLI(dir,
		"hypothesis", "add",
		"--claim", "setup hypothesis",
		"--predicts-instrument", "timing",
		"--predicts-target", "fir",
		"--predicts-direction", "decrease",
		"--predicts-min-effect", "0.05",
		"--kill-if", "tests fail",
	)
	return []string{"H-0001"}
}

func setupKilledHypothesis(dir string) []string {
	GinkgoHelper()
	ids := setupHypothesis(dir)
	runCLI(dir,
		"hypothesis", "kill", ids[0],
		"--reason", "prereq for test",
	)
	return ids
}

func setupPaused(dir string) []string {
	GinkgoHelper()
	runCLI(dir,
		"pause", "--reason", "setup",
	)
	return nil
}
