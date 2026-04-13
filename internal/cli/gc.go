package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/worktree"
	"github.com/spf13/cobra"
)

func gcCommands() []*cobra.Command {
	return []*cobra.Command{gcCmd()}
}

func gcCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gc",
		Short: "Prune disposable state (worktrees, archived branches); artifacts are preserved",
		Long: `Scan for experiment worktrees and archived branches that are safe to
remove and report what would be cleaned up.

An experiment's worktree is reclaimable when its parent hypothesis has
reached a terminal status (supported, refuted, or killed).

Archived branches (autoresearch/<exp>@<ts>, created by experiment reset)
are reclaimable under the same rule.

Currently gc is dry-run only — it reports what it would prune but does
not actually remove anything. Artifacts are never touched.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)

			s, err := openStore()
			if err != nil {
				return err
			}

			hypotheses, err := s.ListHypotheses()
			if err != nil {
				return err
			}
			hypStatus := make(map[string]string, len(hypotheses))
			for _, h := range hypotheses {
				hypStatus[h.ID] = h.Status
			}

			experiments, err := s.ListExperiments()
			if err != nil {
				return err
			}

			// Collect reclaimable worktrees.
			type reclaimableWorktree struct {
				ExperimentID string `json:"experiment_id"`
				Hypothesis   string `json:"hypothesis"`
				HypStatus    string `json:"hypothesis_status"`
				Path         string `json:"path"`
				Exists       bool   `json:"exists"`
				Files        int    `json:"files"`
				Bytes        int64  `json:"bytes"`
			}
			var reclaimWorktrees []reclaimableWorktree
			var totalFiles int
			var totalBytes int64

			for _, e := range experiments {
				if e.Worktree == "" {
					continue
				}
				status := hypStatus[e.Hypothesis]
				if !isTerminal(status) {
					continue
				}
				exists := false
				var files int
				var bytes int64
				if info, err := os.Stat(e.Worktree); err == nil && info.IsDir() {
					exists = true
					files, bytes = dirStats(e.Worktree)
				}
				totalFiles += files
				totalBytes += bytes
				reclaimWorktrees = append(reclaimWorktrees, reclaimableWorktree{
					ExperimentID: e.ID,
					Hypothesis:   e.Hypothesis,
					HypStatus:    status,
					Path:         e.Worktree,
					Exists:       exists,
					Files:        files,
					Bytes:        bytes,
				})
			}

			// Collect reclaimable archived branches.
			// Archived branches are named autoresearch/<exp-id>@<timestamp>.
			// We match them against experiments whose hypothesis is terminal.
			type reclaimableBranch struct {
				Branch       string `json:"branch"`
				ExperimentID string `json:"experiment_id,omitempty"`
				Hypothesis   string `json:"hypothesis,omitempty"`
				HypStatus    string `json:"hypothesis_status,omitempty"`
			}
			var reclaimBranches []reclaimableBranch

			archived, err := worktree.ListBranches(globalProjectDir, "autoresearch/*@*")
			if err != nil {
				// Non-fatal: git might not have any matching branches.
				archived = nil
			}

			// Index experiments by ID for branch matching.
			expByID := make(map[string]*entity.Experiment, len(experiments))
			for _, e := range experiments {
				expByID[e.ID] = e
			}

			for _, branch := range archived {
				// Branch format: autoresearch/E-NNNN@<timestamp>
				// Extract the experiment ID between "autoresearch/" and "@".
				name := branch
				if idx := len("autoresearch/"); idx < len(name) {
					name = name[idx:]
				}
				expID := name
				for i, c := range name {
					if c == '@' {
						expID = name[:i]
						break
					}
				}

				e, ok := expByID[expID]
				if !ok {
					// Orphaned archived branch — no matching experiment.
					// Still report it, but without hypothesis info.
					reclaimBranches = append(reclaimBranches, reclaimableBranch{Branch: branch})
					continue
				}
				status := hypStatus[e.Hypothesis]
				if !isTerminal(status) {
					continue
				}
				reclaimBranches = append(reclaimBranches, reclaimableBranch{
					Branch:       branch,
					ExperimentID: expID,
					Hypothesis:   e.Hypothesis,
					HypStatus:    status,
				})
			}

			result := map[string]any{
				"status":        "dry-run",
				"worktrees":     reclaimWorktrees,
				"branches":      reclaimBranches,
				"total_files":   totalFiles,
				"total_bytes":   totalBytes,
				"note":          "gc is dry-run only — nothing was removed",
			}

			if w.IsJSON() {
				return w.JSON(result)
			}

			w.Textln("[dry-run] gc would prune the following (nothing was removed):")
			w.Textln("")
			if len(reclaimWorktrees) == 0 && len(reclaimBranches) == 0 {
				w.Textln("  nothing to clean up")
				return nil
			}

			if len(reclaimWorktrees) > 0 {
				w.Textf("  worktrees (%d):\n", len(reclaimWorktrees))
				for _, wt := range reclaimWorktrees {
					if !wt.Exists {
						w.Textf("    %-8s  hyp=%-8s (%s)  [already gone]\n",
							wt.ExperimentID, wt.Hypothesis, wt.HypStatus)
					} else {
						w.Textf("    %-8s  hyp=%-8s (%s)  %d files, %s\n",
							wt.ExperimentID, wt.Hypothesis, wt.HypStatus,
							wt.Files, formatBytes(wt.Bytes))
					}
				}
				w.Textln("")
			}

			if len(reclaimBranches) > 0 {
				w.Textf("  archived branches (%d):\n", len(reclaimBranches))
				for _, b := range reclaimBranches {
					if b.ExperimentID == "" {
						w.Textf("    %s  (orphaned — no matching experiment)\n", b.Branch)
					} else {
						w.Textf("    %s  exp=%-8s hyp=%-8s (%s)\n",
							b.Branch, b.ExperimentID, b.Hypothesis, b.HypStatus)
					}
				}
				w.Textln("")
			}

			w.Textf("  total: %d worktrees (%d files, %s), %d branches would be pruned\n",
				len(reclaimWorktrees), totalFiles, formatBytes(totalBytes), len(reclaimBranches))
			return nil
		},
	}
}

func isTerminal(status string) bool {
	switch status {
	case entity.StatusSupported, entity.StatusRefuted, entity.StatusKilled:
		return true
	}
	return false
}

// dirStats walks a directory tree and returns the total file count and byte size.
func dirStats(root string) (files int, bytes int64) {
	filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort: skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		files++
		if info, err := d.Info(); err == nil {
			bytes += info.Size()
		}
		return nil
	})
	return
}

// formatBytes returns a human-readable byte size (e.g. "14.2 MB").
func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
