package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/worktree"
	"github.com/spf13/cobra"
)

const scratchPromotionInstruction = "Promote useful scratch findings by creating a normal hypothesis and experiment, then capture evidence through observe/artifact before concluding."

func scratchCommands() []*cobra.Command {
	c := &cobra.Command{
		Use:   "scratch",
		Short: "Manage temporary probe workspaces outside the main checkout",
		Long: `Create and clean up temporary git worktrees for premise checks.

Scratch workspaces are intentionally outside the main checkout and outside
experiment branches. They are not frontier entries and are not conclusion
evidence unless their results are captured later through normal observation
or artifact mechanisms.`,
	}
	c.AddCommand(
		scratchCreateCmd(),
		scratchPathCmd(),
		scratchShowCmd(),
		scratchListCmd(),
		scratchCleanupCmd(),
	)
	return []*cobra.Command{c}
}

func scratchCreateCmd() *cobra.Command {
	var fromRef, name, author, notes string
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a temporary probe worktree",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			name = strings.TrimSpace(name)
			fromRef = strings.TrimSpace(fromRef)
			if name == "" {
				return errors.New("--name is required")
			}
			if fromRef == "" {
				fromRef = "HEAD"
			}

			s, err := openStoreLive()
			if err != nil {
				return err
			}
			fromSHA, err := worktree.ResolveRef(globalProjectDir, fromRef)
			if err != nil {
				return fmt.Errorf("resolve scratch source %q: %w", fromRef, err)
			}
			wtRoot, err := s.WorktreesRoot()
			if err != nil {
				return err
			}
			dryPath := filepath.Join(wtRoot, "scratch", "S-XXXX")
			if err := dryRun(w, fmt.Sprintf("create scratch workspace %q at %s", name, dryPath), map[string]any{
				"name":     name,
				"from_ref": fromRef,
				"from_sha": fromSHA,
				"worktree": dryPath,
			}); err != nil {
				return err
			}

			id, err := s.AllocID(store.KindScratch)
			if err != nil {
				return err
			}
			wtPath := filepath.Join(wtRoot, "scratch", id)
			branch := "autoresearch/scratch/" + id + "-" + scratchSlug(name)
			if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
				return err
			}
			if err := worktree.Add(globalProjectDir, wtPath, branch, fromSHA); err != nil {
				return fmt.Errorf("create scratch worktree: %w", err)
			}

			sc := &entity.Scratch{
				ID:        id,
				Name:      name,
				Status:    entity.ScratchStatusActive,
				FromRef:   fromRef,
				FromSHA:   fromSHA,
				Worktree:  wtPath,
				Branch:    branch,
				Author:    or(author, "agent:orchestrator"),
				CreatedAt: nowUTC(),
				Body:      entity.AppendMarkdownSection("", "Scratch notes", notes),
			}
			if err := s.WriteScratch(sc); err != nil {
				return err
			}
			if err := emitEvent(s, "scratch.create", sc.Author, id, map[string]any{
				"to":       entity.ScratchStatusActive,
				"name":     name,
				"from_ref": fromRef,
				"from_sha": fromSHA,
				"worktree": wtPath,
				"branch":   branch,
			}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("created scratch %s\n  worktree: %s\n  branch:   %s", id, wtPath, branch),
				map[string]any{
					"status":                "ok",
					"id":                    id,
					"scratch":               sc,
					"worktree":              wtPath,
					"branch":                branch,
					"promotion_instruction": scratchPromotionInstruction,
				},
			)
		},
	}
	c.Flags().StringVar(&fromRef, "from", "HEAD", "git ref to copy into the scratch worktree")
	c.Flags().StringVar(&name, "name", "", "short name for the scratch probe (required)")
	addAuthorFlag(c, &author, "")
	c.Flags().StringVar(&notes, "notes", "", "optional scratch notes persisted in the scratch body")
	return c
}

func scratchPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path <scratch-id>",
		Short: "Print the absolute path of a scratch worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			sc, err := s.ReadScratch(args[0])
			if err != nil {
				return err
			}
			return w.Emit(sc.Worktree, map[string]string{"id": sc.ID, "worktree": sc.Worktree})
		},
	}
}

func scratchShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <scratch-id>",
		Short: "Show scratch workspace metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			sc, err := s.ReadScratch(args[0])
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(map[string]any{"scratch": sc, "promotion_instruction": scratchPromotionInstruction})
			}
			w.Textf("id:         %s\n", sc.ID)
			w.Textf("name:       %s\n", sc.Name)
			w.Textf("status:     %s\n", sc.EffectiveStatus())
			w.Textf("from:       %s (%s)\n", sc.FromRef, shortSHA(sc.FromSHA))
			w.Textf("worktree:   %s\n", sc.Worktree)
			w.Textf("branch:     %s\n", sc.Branch)
			w.Textf("created_at: %s\n", sc.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
			if sc.CleanedAt != nil {
				w.Textf("cleaned_at: %s\n", sc.CleanedAt.Format("2006-01-02T15:04:05Z07:00"))
			}
			if sc.CleanupReason != "" {
				w.Textf("cleanup:    %s\n", sc.CleanupReason)
			}
			w.Textf("promote:    %s\n", scratchPromotionInstruction)
			return nil
		},
	}
}

func scratchListCmd() *cobra.Command {
	var status string
	c := &cobra.Command{
		Use:   "list",
		Short: "List scratch workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			status = strings.TrimSpace(status)
			if status == "" {
				status = entity.ScratchStatusActive
			}
			if status != "all" && status != entity.ScratchStatusActive && status != entity.ScratchStatusCleaned {
				return fmt.Errorf("--status must be active, cleaned, or all")
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			all, err := s.ListScratch()
			if err != nil {
				return err
			}
			var filtered []*entity.Scratch
			for _, sc := range all {
				if sc == nil {
					continue
				}
				if status != "all" && sc.EffectiveStatus() != status {
					continue
				}
				filtered = append(filtered, sc)
			}
			if w.IsJSON() {
				return w.JSON(filtered)
			}
			if len(filtered) == 0 {
				w.Textln("(no scratch workspaces)")
				return nil
			}
			for _, sc := range filtered {
				w.Textf("  %-8s  %-8s  %-24s  %s\n", sc.ID, sc.EffectiveStatus(), sc.Name, sc.Worktree)
			}
			return nil
		},
	}
	c.Flags().StringVar(&status, "status", entity.ScratchStatusActive, "filter by status (active|cleaned|all)")
	return c
}

func scratchCleanupCmd() *cobra.Command {
	var reason, author string
	c := &cobra.Command{
		Use:   "cleanup <scratch-id>",
		Short: "Remove a scratch worktree and mark it cleaned",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			id := args[0]
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			sc, err := s.ReadScratch(id)
			if err != nil {
				return err
			}
			if sc.EffectiveStatus() == entity.ScratchStatusCleaned {
				return fmt.Errorf("scratch %s is already cleaned", id)
			}
			reason = strings.TrimSpace(reason)
			if err := dryRun(w, fmt.Sprintf("cleanup scratch %s", id), map[string]any{
				"id":       id,
				"worktree": sc.Worktree,
				"branch":   sc.Branch,
				"reason":   reason,
			}); err != nil {
				return err
			}
			if sc.Worktree != "" {
				if _, statErr := os.Stat(sc.Worktree); statErr == nil {
					if err := worktree.Remove(globalProjectDir, sc.Worktree); err != nil {
						return fmt.Errorf("remove scratch worktree: %w", err)
					}
				} else if !os.IsNotExist(statErr) {
					return fmt.Errorf("stat scratch worktree: %w", statErr)
				}
			}
			if sc.Branch != "" {
				if err := worktree.DeleteBranch(globalProjectDir, sc.Branch); err != nil {
					return fmt.Errorf("delete scratch branch: %w", err)
				}
			}
			prevStatus := sc.EffectiveStatus()
			cleanedAt := nowUTC()
			sc.Status = entity.ScratchStatusCleaned
			sc.CleanedAt = &cleanedAt
			sc.CleanupReason = reason
			if err := s.WriteScratch(sc); err != nil {
				return err
			}
			eventData := map[string]any{
				"from":     prevStatus,
				"to":       entity.ScratchStatusCleaned,
				"worktree": sc.Worktree,
				"branch":   sc.Branch,
			}
			if reason != "" {
				eventData["reason"] = reason
			}
			if err := emitEvent(s, "scratch.cleanup", or(author, "agent:orchestrator"), id, eventData); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("cleaned scratch %s", id),
				map[string]any{"status": "ok", "id": id, "scratch": sc},
			)
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "optional reason for cleaning the scratch workspace")
	addAuthorFlag(c, &author, "")
	return c
}

func scratchSlug(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
		if b.Len() >= 48 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "probe"
	}
	return out
}

func activeScratchForRead(s *store.Store, now time.Time) ([]readmodel.ScratchWorkspaceView, []readmodel.ScratchWorkspaceView, error) {
	all, err := s.ListScratch()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := s.Config()
	if err != nil {
		return nil, nil, err
	}
	active := readmodel.ActiveScratchWorkspaces(all, now)
	var stale []readmodel.ScratchWorkspaceView
	if cfg.Budgets.StaleExperimentMinutes > 0 {
		stale = readmodel.StaleScratchWorkspaces(all, time.Duration(cfg.Budgets.StaleExperimentMinutes)*time.Minute, now)
	}
	return active, stale, nil
}
