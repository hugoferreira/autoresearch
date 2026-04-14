package cli

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func hypothesisCommands() []*cobra.Command {
	h := &cobra.Command{
		Use:     "hypothesis",
		Aliases: []string{"hyp"},
		Short:   "Manage falsifiable hypotheses",
	}
	h.AddCommand(
		hypothesisAddCmd(),
		hypothesisListCmd(),
		hypothesisShowCmd(),
		hypothesisPromoteCmd(),
		hypothesisKillCmd(),
		hypothesisReopenCmd(),
		hypothesisWorktreeCmd(),
		hypothesisDiffCmd(),
		hypothesisApplyCmd(),
	)
	return []*cobra.Command{h}
}

func hypothesisAddCmd() *cobra.Command {
	var (
		claim          string
		parent         string
		predInstrument string
		predTarget     string
		predDirection  string
		predMinEffect  float64
		killIf         []string
		inspiredBy     []string
		author         string
		tags           []string
		rationale      string
	)
	c := &cobra.Command{
		Use:   "add",
		Short: "Add a new falsifiable hypothesis",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			if strings.TrimSpace(claim) == "" {
				return errors.New("--claim is required")
			}
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			cfg, err := s.Config()
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}
			if err := firewall.RequireActiveGoal(st); err != nil {
				return err
			}
			goal, err := s.ActiveGoal()
			if err != nil {
				return err
			}

			if parent != "" {
				ph, err := s.ReadHypothesis(parent)
				if err != nil {
					return fmt.Errorf("parent hypothesis %q: %w", parent, err)
				}
				if err := firewall.CheckParentReviewed(ph); err != nil {
					return err
				}
			}

			// Validate inspired_by references exist and do not point at lessons
			// sourced from an unreviewed decisive chain.
			inspiredByLessons := make([]*entity.Lesson, 0, len(inspiredBy))
			for _, lid := range inspiredBy {
				lesson, err := s.ReadLesson(lid)
				if err != nil {
					return fmt.Errorf("--inspired-by %s: %w", lid, err)
				}
				inspiredByLessons = append(inspiredByLessons, lesson)
			}
			if err := firewall.CheckInspiredByLessonsReviewed(s, inspiredByLessons); err != nil {
				return err
			}

			h := &entity.Hypothesis{
				GoalID: st.CurrentGoalID,
				Parent: parent,
				Claim:  claim,
				Predicts: entity.Predicts{
					Instrument: predInstrument,
					Target:     predTarget,
					Direction:  predDirection,
					MinEffect:  predMinEffect,
				},
				KillIf:     killIf,
				InspiredBy: inspiredBy,
				Status:     entity.StatusOpen,
				Author:     or(author, "human"),
				CreatedAt:  nowUTC(),
				Tags:       tags,
				Body:       entity.AppendMarkdownSection("", "Rationale", rationale),
			}
			if err := firewall.ValidateHypothesis(h, cfg); err != nil {
				return err
			}
			if err := firewall.CheckHypothesisInstrumentWithinGoal(goal, h); err != nil {
				return err
			}

			if err := dryRun(w, fmt.Sprintf("add hypothesis (claim=%q)", claim), map[string]any{"hypothesis": h}); err != nil {
				return err
			}

			id, err := s.AllocID(store.KindHypothesis)
			if err != nil {
				return err
			}
			h.ID = id
			if err := s.WriteHypothesis(h); err != nil {
				return err
			}
			eventData := map[string]any{"claim": claim, "parent": parent, "goal_id": st.CurrentGoalID}
			if snippet := truncate(strings.TrimSpace(rationale), 200); snippet != "" {
				eventData["rationale"] = snippet
			}
			if len(inspiredBy) > 0 {
				eventData["inspired_by"] = inspiredBy
			}
			if err := emitEvent(s, "hypothesis.add", or(author, "human"), id, eventData); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("added %s", id),
				map[string]any{"status": "ok", "id": id, "hypothesis": h},
			)
		},
	}
	c.Flags().StringVar(&claim, "claim", "", "falsifiable claim (required)")
	c.Flags().StringVar(&parent, "parent", "", "parent hypothesis id (optional)")
	c.Flags().StringVar(&predInstrument, "predicts-instrument", "", "instrument that will measure the predicted effect (required; must be the active goal objective or a goal-constraint instrument)")
	c.Flags().StringVar(&predTarget, "predicts-target", "", "target measured by the instrument (required)")
	c.Flags().StringVar(&predDirection, "predicts-direction", "", "predicted direction: increase | decrease (required)")
	c.Flags().Float64Var(&predMinEffect, "predicts-min-effect", 0, "minimum fractional effect required to call it supported (required)")
	c.Flags().StringArrayVar(&killIf, "kill-if", nil, "kill criterion; may be repeated (at least one required)")
	c.Flags().StringSliceVar(&inspiredBy, "inspired-by", nil, "lesson IDs this hypothesis draws on (L-NNNN; reviewed lessons only; comma-separated or repeated)")
	addAuthorFlag(c, &author, "")
	c.Flags().StringSliceVar(&tags, "tag", nil, "tag; may be repeated")
	c.Flags().StringVar(&rationale, "rationale", "", "one-line rationale: why this hypothesis, what evidence or lesson led you here (persisted in the hypothesis body under `# Rationale`; first 200 chars also land on the hypothesis.add event)")
	return c
}

func hypothesisListCmd() *cobra.Command {
	var status, parent, goalFlag string
	c := &cobra.Command{
		Use:   "list",
		Short: "List hypotheses",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			scope, err := resolveGoalScope(s, goalFlag)
			if err != nil {
				return err
			}
			all, err := s.ListHypotheses()
			if err != nil {
				return err
			}
			all = newGoalScopeResolver(s, scope).filterHypotheses(all)
			var filtered []*entity.Hypothesis
			for _, h := range all {
				if status != "" && h.Status != status {
					continue
				}
				if parent != "" && h.Parent != parent {
					continue
				}
				filtered = append(filtered, h)
			}
			if w.IsJSON() {
				return w.JSON(filtered)
			}
			if len(filtered) == 0 {
				w.Textln("(no hypotheses)")
				return nil
			}
			for _, h := range filtered {
				par := h.Parent
				if par == "" {
					par = "-"
				}
				w.Textf("  %-8s  %-12s  %-12s  parent=%s  %s\n", h.ID, h.Status, h.Author, par, truncate(h.Claim, 60))
			}
			return nil
		},
	}
	c.Flags().StringVar(&status, "status", "", "filter by status")
	c.Flags().StringVar(&parent, "parent", "", "filter by parent id")
	c.Flags().StringVar(&goalFlag, "goal", "", "goal to scope the list to (defaults to active goal; use 'all' for every goal)")
	return c
}

func hypothesisShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a single hypothesis",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			h, err := s.ReadHypothesis(args[0])
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(h)
			}
			w.Textf("id:          %s\n", h.ID)
			if h.Parent != "" {
				w.Textf("parent:      %s\n", h.Parent)
			}
			w.Textf("status:      %s\n", h.Status)
			w.Textf("author:      %s\n", h.Author)
			w.Textf("claim:       %s\n", h.Claim)
			w.Textf("predicts:    %s %s on %s (min_effect=%g)\n",
				h.Predicts.Direction, h.Predicts.Instrument, h.Predicts.Target, h.Predicts.MinEffect)
			w.Textln("kill_if:")
			for _, k := range h.KillIf {
				w.Textf("  - %s\n", k)
			}
			if len(h.InspiredBy) > 0 {
				w.Textf("inspired_by: %s\n", strings.Join(h.InspiredBy, ", "))
			}
			if len(h.Tags) > 0 {
				w.Textf("tags:        %s\n", strings.Join(h.Tags, ", "))
			}
			w.Textf("created_at:  %s\n", h.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
			return nil
		},
	}
}

func hypothesisPromoteCmd() *cobra.Command {
	var reason, author string
	c := &cobra.Command{
		Use:   "promote <id>",
		Short: "Mark a hypothesis as human-priority (picked first by the generator)",
		Long: `Set priority=human on a hypothesis. The generator subagent's prompt
picks priority=human hypotheses first when choosing what to work on next.
An explicit --reason is required and recorded in the event log.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			if strings.TrimSpace(reason) == "" {
				return errors.New("--reason is required")
			}
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			h, err := s.ReadHypothesis(args[0])
			if err != nil {
				return err
			}
			if h.Status == entity.StatusKilled {
				return fmt.Errorf("%s is killed; reopen before promoting", h.ID)
			}
			h.Priority = "human"
			if err := dryRun(w, fmt.Sprintf("promote %s (%s)", h.ID, reason), map[string]any{"id": h.ID, "reason": reason}); err != nil {
				return err
			}
			if err := s.WriteHypothesis(h); err != nil {
				return err
			}
			if err := emitEvent(s, "hypothesis.promote", or(author, "human"), h.ID, map[string]string{"reason": reason}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("promoted %s: %s", h.ID, reason),
				map[string]any{"status": "ok", "id": h.ID, "reason": reason, "priority": "human"},
			)
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "why the hypothesis is being promoted (required)")
	addAuthorFlag(c, &author, "")
	return c
}

func hypothesisKillCmd() *cobra.Command {
	var reason, author string
	c := &cobra.Command{
		Use:   "kill <id>",
		Short: "Kill a hypothesis (status -> killed) with a reason",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			if strings.TrimSpace(reason) == "" {
				return errors.New("--reason is required")
			}
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			h, err := s.ReadHypothesis(args[0])
			if err != nil {
				return err
			}
			if h.Status == entity.StatusKilled {
				return fmt.Errorf("%s is already killed", h.ID)
			}
			h.Status = entity.StatusKilled
			if err := dryRun(w, fmt.Sprintf("kill %s (%s)", h.ID, reason), map[string]any{"id": h.ID, "reason": reason}); err != nil {
				return err
			}
			if err := s.WriteHypothesis(h); err != nil {
				return err
			}
			if err := emitEvent(s, "hypothesis.kill", or(author, "human"), h.ID, map[string]string{"reason": reason}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("killed %s (%s)", h.ID, reason),
				map[string]any{"status": "ok", "id": h.ID, "reason": reason},
			)
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "reason for killing (required)")
	addAuthorFlag(c, &author, "")
	return c
}

func hypothesisReopenCmd() *cobra.Command {
	var reason, author string
	c := &cobra.Command{
		Use:   "reopen <id>",
		Short: "Reopen a killed hypothesis (status killed -> open)",
		Long: `Reopen a previously killed hypothesis. Only valid for status=killed —
refuted and supported hypotheses are conclusive and should be superseded
by new hypotheses rather than reopened. An explicit --reason is required
and logged.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			if strings.TrimSpace(reason) == "" {
				return errors.New("--reason is required")
			}
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			h, err := s.ReadHypothesis(args[0])
			if err != nil {
				return err
			}
			if h.Status != entity.StatusKilled {
				return fmt.Errorf("%s has status %q; reopen is only valid for killed hypotheses (refuted/supported are conclusive)", h.ID, h.Status)
			}
			h.Status = entity.StatusOpen
			if err := dryRun(w, fmt.Sprintf("reopen %s (%s)", h.ID, reason), map[string]any{"id": h.ID, "reason": reason}); err != nil {
				return err
			}
			if err := s.WriteHypothesis(h); err != nil {
				return err
			}
			if err := emitEvent(s, "hypothesis.reopen", or(author, "human"), h.ID, map[string]string{"reason": reason}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("reopened %s (%s)", h.ID, reason),
				map[string]any{"status": "ok", "id": h.ID, "reason": reason},
			)
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "why the hypothesis is being reopened (required)")
	addAuthorFlag(c, &author, "")
	return c
}

// resolveWinningExperiment finds the best supported conclusion for a hypothesis
// and returns the candidate experiment. When conclusionID is non-empty, that
// specific conclusion is used instead of searching. Returns the conclusion,
// experiment, and any error.
func resolveWinningExperiment(s *store.Store, hypID, conclusionID string) (*entity.Conclusion, *entity.Experiment, error) {
	var concl *entity.Conclusion
	if conclusionID != "" {
		c, err := s.ReadConclusion(conclusionID)
		if err != nil {
			return nil, nil, err
		}
		if c.Hypothesis != hypID {
			return nil, nil, fmt.Errorf("conclusion %s belongs to %s, not %s", conclusionID, c.Hypothesis, hypID)
		}
		concl = c
	} else {
		all, err := s.ListConclusionsForHypothesis(hypID)
		if err != nil {
			return nil, nil, err
		}
		// Pick the best supported conclusion (largest |delta_frac|).
		for _, c := range all {
			if c.Verdict != entity.VerdictSupported {
				continue
			}
			if concl == nil || abs(c.Effect.DeltaFrac) > abs(concl.Effect.DeltaFrac) {
				concl = c
			}
		}
		if concl == nil {
			// Fall back to the most recent experiment if no supported conclusion.
			exps, err := s.ListExperimentsForHypothesis(hypID)
			if err != nil {
				return nil, nil, err
			}
			if len(exps) == 0 {
				return nil, nil, fmt.Errorf("hypothesis %s has no experiments", hypID)
			}
			exp := exps[len(exps)-1]
			return nil, exp, nil
		}
	}
	exp, err := s.ReadExperiment(concl.CandidateExp)
	if err != nil {
		return nil, nil, fmt.Errorf("read candidate experiment %s: %w", concl.CandidateExp, err)
	}
	return concl, exp, nil
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func hypothesisWorktreeCmd() *cobra.Command {
	var conclusionID string
	c := &cobra.Command{
		Use:   "worktree <hyp-id>",
		Short: "Print the worktree path for a hypothesis's winning (or latest) experiment",
		Long: `Resolve a hypothesis to the experiment that best supports it (or the
most recent experiment if none is supported) and print the worktree path.

Use --conclusion C-NNNN to pick a specific conclusion instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			_, exp, err := resolveWinningExperiment(s, args[0], conclusionID)
			if err != nil {
				return err
			}
			if exp.Worktree == "" {
				return fmt.Errorf("%s has no worktree (status=%s)", exp.ID, exp.Status)
			}
			return w.Emit(exp.Worktree, map[string]any{
				"hypothesis": args[0],
				"experiment": exp.ID,
				"worktree":   exp.Worktree,
				"branch":     exp.Branch,
			})
		},
	}
	c.Flags().StringVar(&conclusionID, "conclusion", "", "use a specific conclusion instead of the best supported one")
	return c
}

func hypothesisDiffCmd() *cobra.Command {
	var conclusionID string
	c := &cobra.Command{
		Use:   "diff <hyp-id>",
		Short: "Show the unified diff of the winning experiment against its baseline",
		Long: `Resolve a hypothesis to its best supported conclusion's candidate
experiment and show the git diff between the baseline and the experiment
branch. Falls back to the most recent experiment if none is supported.

Use --conclusion C-NNNN to pick a specific conclusion.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			_, exp, err := resolveWinningExperiment(s, args[0], conclusionID)
			if err != nil {
				return err
			}
			if exp.Branch == "" {
				return fmt.Errorf("%s has no branch (status=%s)", exp.ID, exp.Status)
			}
			base := exp.Baseline.SHA
			if base == "" {
				base = exp.Baseline.Ref
			}
			diff, err := gitDiff(globalProjectDir, base, exp.Branch)
			if err != nil {
				return err
			}
			if globalJSON {
				w := output.Default(true)
				return w.JSON(map[string]any{
					"hypothesis": args[0],
					"experiment": exp.ID,
					"baseline":   base,
					"branch":     exp.Branch,
					"diff":       diff,
				})
			}
			fmt.Print(diff)
			return nil
		},
	}
	c.Flags().StringVar(&conclusionID, "conclusion", "", "use a specific conclusion instead of the best supported one")
	return c
}

func hypothesisApplyCmd() *cobra.Command {
	var (
		conclusionID string
		merge        bool
	)
	c := &cobra.Command{
		Use:   "apply <hyp-id>",
		Short: "Cherry-pick (or merge) the winning experiment's commits onto the current branch",
		Long: `Resolve a hypothesis to its best supported conclusion's candidate
experiment and cherry-pick its commits onto the current branch. Use
--merge to merge instead of cherry-pick.

This is the "ship it" verb: once a hypothesis is supported and the gate
reviewer has accepted the conclusion, apply brings the optimization
into the main branch.

Use --conclusion C-NNNN to pick a specific conclusion.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			concl, exp, err := resolveWinningExperiment(s, args[0], conclusionID)
			if err != nil {
				return err
			}
			if concl == nil || concl.Verdict != entity.VerdictSupported {
				return fmt.Errorf("hypothesis %s has no supported conclusion — nothing to apply", args[0])
			}
			// Gate: hypothesis must have been reviewed (not still "unreviewed").
			hyp, err := s.ReadHypothesis(args[0])
			if err != nil {
				return err
			}
			if hyp.Status == entity.StatusUnreviewed {
				return fmt.Errorf("hypothesis %s is unreviewed — dispatch the gate reviewer to review %s first (or self-review with `conclusion accept %s --reviewed-by ...`)", args[0], concl.ID, concl.ID)
			}
			if exp.Branch == "" {
				return fmt.Errorf("%s has no branch (status=%s)", exp.ID, exp.Status)
			}
			verb := "cherry-pick"
			if merge {
				verb = "merge"
			}
			if err := dryRun(w, fmt.Sprintf("%s branch %s (experiment %s, conclusion %s)", verb, exp.Branch, exp.ID, concl.ID), map[string]any{
				"action":     verb,
				"hypothesis": args[0],
				"conclusion": concl.ID,
				"experiment": exp.ID,
				"branch":     exp.Branch,
			}); err != nil {
				return err
			}
			var out string
			if merge {
				out, err = gitMerge(globalProjectDir, exp.Branch)
			} else {
				out, err = gitCherryPick(globalProjectDir, exp.Baseline.SHA, exp.Branch)
			}
			if err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("applied %s → %s\n%s", concl.ID, exp.Branch, out),
				map[string]any{
					"status":     "ok",
					"hypothesis": args[0],
					"conclusion": concl.ID,
					"experiment": exp.ID,
					"branch":     exp.Branch,
					"output":     out,
				},
			)
		},
	}
	c.Flags().StringVar(&conclusionID, "conclusion", "", "use a specific conclusion instead of the best supported one")
	c.Flags().BoolVar(&merge, "merge", false, "merge instead of cherry-pick (preserves experiment branch history)")
	return c
}

func gitDiff(projectDir, base, branch string) (string, error) {
	cmd := exec.Command("git", "-C", projectDir, "diff", base+".."+branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func gitCherryPick(projectDir, baseSHA, branch string) (string, error) {
	cmd := exec.Command("git", "-C", projectDir, "cherry-pick", baseSHA+".."+branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git cherry-pick: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func gitMerge(projectDir, branch string) (string, error) {
	cmd := exec.Command("git", "-C", projectDir, "merge", branch, "--no-edit")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git merge: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
